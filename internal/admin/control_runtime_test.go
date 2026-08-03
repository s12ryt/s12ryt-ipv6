package admin

import (
	"context"
	"errors"
	"io"
	"net"
	"reflect"
	"testing"
)

type fakeControlService struct {
	operations *[]string
	err        error
}

func (s *fakeControlService) Serve(context.Context, net.Listener) error {
	*s.operations = append(*s.operations, "serve")
	return s.err
}

type recordedCloser struct {
	name       string
	operations *[]string
}

func (c *recordedCloser) Close() error {
	*c.operations = append(*c.operations, "close:"+c.name)
	return nil
}

type recordedListener struct {
	net.Listener
	closer *recordedCloser
}

func (l *recordedListener) Close() error {
	return l.closer.Close()
}

func TestControlRuntimeLocksBeforeListeningAndCleansUpInReverseOrder(t *testing.T) {
	operations := []string{}
	service := &fakeControlService{operations: &operations}
	runtime, err := NewControlRuntime(ControlRuntimeOptions{
		Service: service,
		AcquireLock: func(path string) (io.Closer, error) {
			operations = append(operations, "lock:"+path)
			return &recordedCloser{name: "lock", operations: &operations}, nil
		},
		Listen: func(path string) (net.Listener, error) {
			operations = append(operations, "listen:"+path)
			return &recordedListener{closer: &recordedCloser{name: "listener", operations: &operations}}, nil
		},
		LockPath: "/run/service.lock", SocketPath: "/run/control.sock",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"lock:/run/service.lock", "listen:/run/control.sock", "serve", "close:listener", "close:lock"}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("operations = %#v, want %#v", operations, want)
	}
}

func TestControlRuntimeDoesNotListenWhenLockIsHeld(t *testing.T) {
	operations := []string{}
	runtime, err := NewControlRuntime(ControlRuntimeOptions{
		Service: &fakeControlService{operations: &operations},
		AcquireLock: func(string) (io.Closer, error) {
			operations = append(operations, "lock")
			return nil, ErrServiceRunning
		},
		Listen: func(string) (net.Listener, error) {
			operations = append(operations, "listen")
			return nil, errors.New("must not listen")
		},
		LockPath: "/run/service.lock", SocketPath: "/run/control.sock",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Serve(context.Background()); !errors.Is(err, ErrServiceRunning) {
		t.Fatalf("Serve() error = %v, want ErrServiceRunning", err)
	}
	if !reflect.DeepEqual(operations, []string{"lock"}) {
		t.Fatalf("operations = %#v", operations)
	}
}

func TestControlRuntimeReleasesLockWhenListenFails(t *testing.T) {
	operations := []string{}
	runtime, err := NewControlRuntime(ControlRuntimeOptions{
		Service: &fakeControlService{operations: &operations},
		AcquireLock: func(string) (io.Closer, error) {
			return &recordedCloser{name: "lock", operations: &operations}, nil
		},
		Listen:   func(string) (net.Listener, error) { return nil, errors.New("bind failed") },
		LockPath: "/run/service.lock", SocketPath: "/run/control.sock",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Serve(context.Background()); err == nil {
		t.Fatal("Serve() error = nil")
	}
	if !reflect.DeepEqual(operations, []string{"close:lock"}) {
		t.Fatalf("operations = %#v", operations)
	}
}

func TestControlRuntimeValidatesOptions(t *testing.T) {
	if _, err := NewControlRuntime(ControlRuntimeOptions{}); err == nil {
		t.Fatal("NewControlRuntime(empty) error = nil")
	}
}
