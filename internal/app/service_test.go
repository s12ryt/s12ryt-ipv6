package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type lifecycleRecorder struct {
	mu    sync.Mutex
	steps []string
}

func (r *lifecycleRecorder) add(step string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, step)
}

func (r *lifecycleRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.steps...)
}

type inertListener struct{ closed bool }

func (l *inertListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (l *inertListener) Addr() net.Addr            { return testAddr("[::]:34466") }
func (l *inertListener) Close() error              { l.closed = true; return nil }

type testAddr string

func (a testAddr) Network() string { return "tcp" }
func (a testAddr) String() string  { return string(a) }

func validServiceOptions(recorder *lifecycleRecorder, output io.Writer) ServiceOptions {
	return ServiceOptions{
		ListenManagement: func(context.Context) ([]net.Listener, error) {
			recorder.add("listen-management")
			return []net.Listener{&inertListener{}}, nil
		},
		InitializePassword: func() (string, bool, error) {
			recorder.add("initialize-password")
			return "generated-admin-password", true, nil
		},
		InitializeRuntime: func(context.Context) error {
			recorder.add("initialize-runtime")
			return nil
		},
		ReconcileResources: func(context.Context) error {
			recorder.add("reconcile-resources")
			return nil
		},
		RestoreNodes: func(context.Context) error {
			recorder.add("restore-nodes")
			return nil
		},
		ServeHTTP: func(ctx context.Context, _ []net.Listener) error {
			recorder.add("serve-http")
			<-ctx.Done()
			return nil
		},
		ServeControl: func(ctx context.Context) error {
			recorder.add("serve-control")
			<-ctx.Done()
			return nil
		},
		RunNAT64: func(ctx context.Context) error {
			recorder.add("run-nat64")
			<-ctx.Done()
			return nil
		},
		ShutdownNodes:    func(context.Context) error { recorder.add("shutdown-nodes"); return nil },
		ShutdownNetwork:  func(context.Context) error { recorder.add("shutdown-network"); return nil },
		ShutdownFirewall: func(context.Context) error { recorder.add("shutdown-firewall"); return nil },
		SaveStatistics:   func() error { recorder.add("save-statistics"); return nil },
		CloseLog:         func() error { recorder.add("close-log"); return nil },
		PasswordOutput:   output,
		StatsInterval:    time.Hour,
	}
}

func TestServiceBindsManagementBeforeInitializingOrRestoring(t *testing.T) {
	recorder := &lifecycleRecorder{}
	wantErr := errors.New("port occupied")
	options := validServiceOptions(recorder, io.Discard)
	options.ListenManagement = func(context.Context) ([]net.Listener, error) {
		recorder.add("listen-management")
		return nil, wantErr
	}
	service, err := NewService(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Run(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if got := recorder.snapshot(); len(got) != 1 || got[0] != "listen-management" {
		t.Fatalf("lifecycle = %v", got)
	}
}

func TestServiceStartsInSafeOrderAndPrintsGeneratedPasswordOnce(t *testing.T) {
	recorder := &lifecycleRecorder{}
	var output bytes.Buffer
	service, err := NewService(validServiceOptions(recorder, &output))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()

	deadline := time.Now().Add(time.Second)
	for {
		steps := recorder.snapshot()
		if containsStep(steps, "serve-http") && containsStep(steps, "serve-control") && containsStep(steps, "run-nat64") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("service did not start: %v", steps)
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	steps := recorder.snapshot()
	indices := map[string]int{}
	for index, step := range steps {
		if _, exists := indices[step]; !exists {
			indices[step] = index
		}
	}
	for before, after := range map[string]string{
		"listen-management":   "initialize-password",
		"initialize-password": "initialize-runtime",
		"initialize-runtime":  "reconcile-resources",
		"reconcile-resources": "restore-nodes",
		"restore-nodes":       "serve-http",
		"shutdown-nodes":      "shutdown-network",
		"shutdown-network":    "shutdown-firewall",
		"shutdown-firewall":   "save-statistics",
		"save-statistics":     "close-log",
	} {
		if indices[before] >= indices[after] {
			t.Fatalf("%q did not happen before %q: %v", before, after, steps)
		}
	}
	if got := output.String(); got != "initial admin password: generated-admin-password\n" {
		t.Fatalf("password output = %q", got)
	}
}

func TestServiceCleansFirewallWhenRuntimeInitializationFails(t *testing.T) {
	recorder := &lifecycleRecorder{}
	wantErr := errors.New("nftables unavailable")
	options := validServiceOptions(recorder, io.Discard)
	options.InitializeRuntime = func(context.Context) error {
		recorder.add("initialize-runtime")
		return wantErr
	}
	service, err := NewService(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Run(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	steps := recorder.snapshot()
	want := []string{"listen-management", "initialize-password", "initialize-runtime", "shutdown-firewall"}
	if len(steps) != len(want) {
		t.Fatalf("lifecycle = %v, want %v", steps, want)
	}
	for index := range want {
		if steps[index] != want[index] {
			t.Fatalf("lifecycle = %v, want %v", steps, want)
		}
	}
}

func TestServiceContinuesWithDegradedStartupComponents(t *testing.T) {
	recorder := &lifecycleRecorder{}
	options := validServiceOptions(recorder, io.Discard)
	resourceErr := errors.New("resource reconcile failed")
	nodeErr := errors.New("one node failed")
	options.ReconcileResources = func(context.Context) error { return resourceErr }
	options.RestoreNodes = func(context.Context) error { return nodeErr }
	var reported []error
	options.ReportDegraded = func(err error) { reported = append(reported, err) }
	service, err := NewService(options)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(reported) != 2 || !errors.Is(reported[0], resourceErr) || !errors.Is(reported[1], nodeErr) {
		t.Fatalf("reported degraded errors = %v", reported)
	}
}

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	options := validServiceOptions(&lifecycleRecorder{}, io.Discard)
	options.ServeHTTP = nil
	if _, err := NewService(options); err == nil || !strings.Contains(err.Error(), "HTTP") {
		t.Fatalf("NewService() error = %v", err)
	}
}

func containsStep(steps []string, wanted string) bool {
	for _, step := range steps {
		if step == wanted {
			return true
		}
	}
	return false
}
