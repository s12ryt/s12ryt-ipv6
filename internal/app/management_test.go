package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

type managementTestListener struct {
	mu       sync.Mutex
	address  string
	closed   chan struct{}
	closeOne sync.Once
}

func newManagementTestListener(address string) *managementTestListener {
	return &managementTestListener{address: address, closed: make(chan struct{})}
}

func (l *managementTestListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *managementTestListener) Close() error {
	l.closeOne.Do(func() { close(l.closed) })
	return nil
}

func (l *managementTestListener) Addr() net.Addr { return testAddr(l.address) }

func TestManagementHTTPBindsBothFamiliesAndRollsBackPartialFailure(t *testing.T) {
	var calls []struct{ network, address string }
	first := newManagementTestListener("0.0.0.0:34466")
	wantErr := errors.New("IPv6 port occupied")
	management, err := NewManagementHTTP(ManagementHTTPOptions{
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		Listen: func(_ context.Context, network, address string) (net.Listener, error) {
			calls = append(calls, struct{ network, address string }{network, address})
			if network == "tcp4" {
				return first, nil
			}
			return nil, wantErr
		},
		ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := management.Listen(context.Background(), 34466); !errors.Is(err, wantErr) {
		t.Fatalf("Listen() error = %v, want %v", err, wantErr)
	}
	if len(calls) != 2 || calls[0].network != "tcp4" || calls[0].address != "0.0.0.0:34466" || calls[1].network != "tcp6" || calls[1].address != "[::]:34466" {
		t.Fatalf("listen calls = %#v", calls)
	}
	select {
	case <-first.closed:
	default:
		t.Fatal("IPv4 listener remained open after IPv6 bind failure")
	}
}

func TestManagementHTTPServesAllListenersUntilContextCancellation(t *testing.T) {
	management, err := NewManagementHTTP(ManagementHTTPOptions{
		Handler:         http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		Listen:          func(context.Context, string, string) (net.Listener, error) { return nil, errors.New("unused") },
		ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	listeners := []net.Listener{newManagementTestListener("0.0.0.0:34466"), newManagementTestListener("[::]:34466")}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- management.Serve(ctx, listeners) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve() did not stop after context cancellation")
	}
	for _, listener := range listeners {
		select {
		case <-listener.(*managementTestListener).closed:
		default:
			t.Fatal("management listener remained open")
		}
	}
}

func TestNewManagementHTTPRejectsInvalidOptions(t *testing.T) {
	valid := ManagementHTTPOptions{
		Handler:         http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		Listen:          func(context.Context, string, string) (net.Listener, error) { return nil, nil },
		ShutdownTimeout: time.Second,
	}
	for name, mutate := range map[string]func(*ManagementHTTPOptions){
		"handler": func(options *ManagementHTTPOptions) { options.Handler = nil },
		"listen":  func(options *ManagementHTTPOptions) { options.Listen = nil },
		"timeout": func(options *ManagementHTTPOptions) { options.ShutdownTimeout = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			options := valid
			mutate(&options)
			if _, err := NewManagementHTTP(options); err == nil {
				t.Fatal("NewManagementHTTP() error = nil")
			}
		})
	}
}
