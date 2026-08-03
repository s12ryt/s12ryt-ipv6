package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"testing"
)

type fakeProductionService struct {
	run func(context.Context) error
}

func (s fakeProductionService) Run(ctx context.Context) error {
	return s.run(ctx)
}

type recordingCloser struct {
	steps *[]string
	err   error
}

func (c recordingCloser) Close() error {
	*c.steps = append(*c.steps, "unlock")
	return c.err
}

func TestRunProductionLocksBeforeBuildingAndReleasesAfterRun(t *testing.T) {
	var steps []string
	var output bytes.Buffer
	dataDirectory := t.TempDir()
	dependencies := ProductionDependencies{
		AcquireLock: func(path string) (io.Closer, error) {
			steps = append(steps, "lock:"+path)
			return recordingCloser{steps: &steps}, nil
		},
		Build: func(options ProductionOptions) (ProductionService, error) {
			steps = append(steps, "build:"+options.DataDirectory)
			if options.Stdout != &output {
				t.Fatal("builder did not receive production stdout")
			}
			return fakeProductionService{run: func(context.Context) error {
				steps = append(steps, "run")
				return nil
			}}, nil
		},
	}

	err := RunProduction(context.Background(), ProductionOptions{
		DataDirectory: dataDirectory,
		Stdout:        &output,
	}, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"lock:" + filepath.Join(dataDirectory, "service.lock"),
		"build:" + filepath.Clean(dataDirectory),
		"run",
		"unlock",
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps = %v, want %v", steps, want)
	}
}

func TestRunProductionReleasesLockOnBuildAndRunFailures(t *testing.T) {
	buildErr := errors.New("build failed")
	runErr := errors.New("run failed")
	closeErr := errors.New("unlock failed")
	tests := []struct {
		name     string
		buildErr error
		runErr   error
	}{
		{name: "build failure", buildErr: buildErr},
		{name: "run failure", runErr: runErr},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var steps []string
			dependencies := ProductionDependencies{
				AcquireLock: func(string) (io.Closer, error) {
					steps = append(steps, "lock")
					return recordingCloser{steps: &steps, err: closeErr}, nil
				},
				Build: func(ProductionOptions) (ProductionService, error) {
					steps = append(steps, "build")
					if test.buildErr != nil {
						return nil, test.buildErr
					}
					return fakeProductionService{run: func(context.Context) error {
						steps = append(steps, "run")
						return test.runErr
					}}, nil
				},
			}

			err := RunProduction(context.Background(), ProductionOptions{
				DataDirectory: t.TempDir(),
				Stdout:        io.Discard,
			}, dependencies)
			if !errors.Is(err, closeErr) {
				t.Fatalf("RunProduction() error = %v, want close error", err)
			}
			wantErr := test.buildErr
			if wantErr == nil {
				wantErr = test.runErr
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("RunProduction() error = %v, want %v", err, wantErr)
			}
			if steps[len(steps)-1] != "unlock" {
				t.Fatalf("steps = %v, lock was not released last", steps)
			}
		})
	}
}

func TestRunProductionRejectsInvalidDependenciesWithoutSideEffects(t *testing.T) {
	valid := ProductionDependencies{
		AcquireLock: func(string) (io.Closer, error) {
			return recordingCloser{steps: new([]string)}, nil
		},
		Build: func(ProductionOptions) (ProductionService, error) {
			return fakeProductionService{run: func(context.Context) error { return nil }}, nil
		},
	}
	tests := []struct {
		name         string
		ctx          context.Context
		options      ProductionOptions
		dependencies ProductionDependencies
	}{
		{name: "nil context", options: ProductionOptions{DataDirectory: t.TempDir(), Stdout: io.Discard}, dependencies: valid},
		{name: "empty data directory", ctx: context.Background(), options: ProductionOptions{DataDirectory: " ", Stdout: io.Discard}, dependencies: valid},
		{name: "nil stdout", ctx: context.Background(), options: ProductionOptions{DataDirectory: t.TempDir()}, dependencies: valid},
		{name: "nil lock", ctx: context.Background(), options: ProductionOptions{DataDirectory: t.TempDir(), Stdout: io.Discard}, dependencies: ProductionDependencies{Build: valid.Build}},
		{name: "nil builder", ctx: context.Background(), options: ProductionOptions{DataDirectory: t.TempDir(), Stdout: io.Discard}, dependencies: ProductionDependencies{AcquireLock: valid.AcquireLock}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := RunProduction(test.ctx, test.options, test.dependencies); err == nil {
				t.Fatal("RunProduction(invalid) error = nil")
			}
		})
	}
}

func TestRunProductionStopsBeforeBuildWhenLockFails(t *testing.T) {
	wantErr := errors.New("already running")
	built := false
	err := RunProduction(context.Background(), ProductionOptions{
		DataDirectory: t.TempDir(),
		Stdout:        io.Discard,
	}, ProductionDependencies{
		AcquireLock: func(string) (io.Closer, error) { return nil, wantErr },
		Build: func(ProductionOptions) (ProductionService, error) {
			built = true
			return nil, nil
		},
	})
	if !errors.Is(err, wantErr) || built {
		t.Fatalf("RunProduction() error = %v, built = %t", err, built)
	}
}
