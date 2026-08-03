package admin

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestControlClientResetsPasswordThroughServer(t *testing.T) {
	server, err := NewControlServer(&fakePasswordResetter{result: "generated-password-value"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewControlClient(ControlClientOptions{
		Timeout: time.Second,
		Dial: func(context.Context, string) (net.Conn, error) {
			client, service := net.Pipe()
			go func() { _ = server.HandleConn(service) }()
			return client, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	password, err := client.ResetPassword(context.Background(), "/run/s12ryt-ipv6/control.sock", "")
	if err != nil || password != "generated-password-value" {
		t.Fatalf("ResetPassword() = %q, %v", password, err)
	}
}

func TestControlClientClassifiesDialFailureAsUnavailable(t *testing.T) {
	client, err := NewControlClient(ControlClientOptions{
		Timeout: time.Second,
		Dial: func(context.Context, string) (net.Conn, error) {
			return nil, errors.New("socket missing")
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.ResetPassword(context.Background(), "/run/s12ryt-ipv6/control.sock", ""); !errors.Is(err, ErrControlUnavailable) {
		t.Fatalf("ResetPassword() error = %v, want ErrControlUnavailable", err)
	}
}

func TestControlClientRejectsMalformedOrFailedResponses(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{name: "server rejection", response: `{"ok":false,"error":"password reset failed"}` + "\n"},
		{name: "unknown field", response: `{"ok":true,"password":"valid-password-value","extra":true}` + "\n"},
		{name: "missing password", response: `{"ok":true}` + "\n"},
		{name: "trailing value", response: `{"ok":true,"password":"valid-password-value"} {}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewControlClient(ControlClientOptions{
				Timeout: time.Second,
				Dial: func(context.Context, string) (net.Conn, error) {
					client, service := net.Pipe()
					go func() {
						defer service.Close()
						buffer := make([]byte, maxControlRequestBytes)
						_, _ = service.Read(buffer)
						_, _ = service.Write([]byte(tt.response))
					}()
					return client, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.ResetPassword(context.Background(), "/run/control.sock", ""); err == nil || errors.Is(err, ErrControlUnavailable) {
				t.Fatalf("ResetPassword() error = %v", err)
			}
		})
	}
}

func TestControlClientValidatesOptionsAndRequest(t *testing.T) {
	if _, err := NewControlClient(ControlClientOptions{}); err == nil {
		t.Fatal("NewControlClient(empty) error = nil")
	}
	client, err := NewControlClient(ControlClientOptions{
		Timeout: time.Second,
		Dial: func(context.Context, string) (net.Conn, error) {
			return nil, errors.New("must not dial")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ResetPassword(context.Background(), "", ""); err == nil {
		t.Fatal("empty socket path accepted")
	}
	if _, err := client.ResetPassword(context.Background(), "/run/control.sock", strings.Repeat("x", 257)); err == nil {
		t.Fatal("invalid replacement password accepted")
	}
}
