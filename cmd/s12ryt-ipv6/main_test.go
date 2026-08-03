package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"version"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("run(version) exit code = %d, want 0; stderr = %q", exitCode, stderr.String())
	}
	if got, want := stdout.String(), "s12ryt-ipv6 dev\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"unknown"}, &stdout, &stderr)

	if exitCode != 2 {
		t.Fatalf("run(unknown) exit code = %d, want 2", exitCode)
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr is empty, want usage error")
	}
}

func TestRunServeIsDefaultAndUsesConfiguredDataDirectory(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantDirectory string
	}{
		{name: "default command", args: nil, wantDirectory: defaultDataDirectory},
		{name: "explicit command", args: []string{"serve", "--data-dir", "/srv/s12ryt"}, wantDirectory: "/srv/s12ryt"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			var dataDirectory string
			dependencies := commandDependencies{
				serve: func(_ context.Context, directory string, output io.Writer) error {
					dataDirectory = directory
					if output != &stdout {
						t.Fatal("serve output does not use command stdout")
					}
					return nil
				},
			}

			exitCode := runWithDependencies(test.args, &stdout, &stderr, dependencies)

			if exitCode != 0 || dataDirectory != test.wantDirectory {
				t.Fatalf("exit=%d data-dir=%q stderr=%q", exitCode, dataDirectory, stderr.String())
			}
		})
	}
}

func TestRunServeSanitizesFailure(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	dependencies := commandDependencies{
		serve: func(context.Context, string, io.Writer) error {
			return errors.New("secret internal service detail")
		},
	}

	exitCode := runWithDependencies([]string{"serve"}, &stdout, &stderr, dependencies)

	if exitCode != 1 || stdout.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q", exitCode, stdout.String())
	}
	if got := stderr.String(); got != "service failed\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestRunServeRejectsInvalidFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	called := false
	dependencies := commandDependencies{
		serve: func(context.Context, string, io.Writer) error {
			called = true
			return nil
		},
	}

	exitCode := runWithDependencies([]string{"serve", "--unknown"}, &stdout, &stderr, dependencies)

	if exitCode != 2 || called || stderr.Len() == 0 {
		t.Fatalf("exit=%d called=%t stderr=%q", exitCode, called, stderr.String())
	}
}

func TestDefaultCommandDependenciesIncludeProductionService(t *testing.T) {
	dependencies := defaultCommandDependencies()
	if dependencies.serve == nil {
		t.Fatal("defaultCommandDependencies().serve = nil")
	}
	if dependencies.resetPassword == nil {
		t.Fatal("defaultCommandDependencies().resetPassword = nil")
	}
}

func TestRunAdminResetPasswordUsesConfiguredDataDirectory(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var dataDirectory string
	dependencies := commandDependencies{
		resetPassword: func(_ context.Context, directory string) (string, error) {
			dataDirectory = directory
			return "generated-admin-password-value", nil
		},
	}

	exitCode := runWithDependencies([]string{"admin", "reset-password", "--data-dir", "/srv/s12ryt"}, &stdout, &stderr, dependencies)

	if exitCode != 0 || dataDirectory != "/srv/s12ryt" {
		t.Fatalf("exit=%d data-dir=%q stderr=%q", exitCode, dataDirectory, stderr.String())
	}
	if got := stdout.String(); got != "new admin password: generated-admin-password-value\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestRunAdminResetPasswordDefaultsDataDirectoryAndSanitizesFailure(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var dataDirectory string
	dependencies := commandDependencies{
		resetPassword: func(_ context.Context, directory string) (string, error) {
			dataDirectory = directory
			return "", errors.New("reset failed")
		},
	}

	exitCode := runWithDependencies([]string{"admin", "reset-password"}, &stdout, &stderr, dependencies)

	if exitCode != 1 || dataDirectory != defaultDataDirectory || stdout.Len() != 0 {
		t.Fatalf("exit=%d data-dir=%q stdout=%q", exitCode, dataDirectory, stdout.String())
	}
	if got := stderr.String(); got != "admin password reset failed\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestRunAdminResetPasswordRejectsInvalidFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	dependencies := commandDependencies{resetPassword: func(context.Context, string) (string, error) {
		t.Fatal("resetPassword called for invalid command")
		return "", nil
	}}

	exitCode := runWithDependencies([]string{"admin", "reset-password", "--unknown"}, &stdout, &stderr, dependencies)

	if exitCode != 2 || stderr.Len() == 0 {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr.String())
	}
}
