package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
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

func TestControlClientCallsAgentThroughServer(t *testing.T) {
	request := json.RawMessage(`{"command":"apply","document":"` + strings.Repeat("x", 128*1024) + `"}`)
	result := json.RawMessage(`{"ok":false,"error":{"code":"conflict","message":"resource exists"}}`)
	server, err := NewControlServerWithAgent(
		&fakePasswordResetter{},
		&fakeAgentControlHandler{result: result},
		time.Second,
	)
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

	response, err := client.CallAgent(context.Background(), "/run/s12ryt-ipv6/control.sock", request)
	if err != nil {
		t.Fatalf("CallAgent() error = %v", err)
	}
	if !jsonEqual(response, result) {
		t.Fatalf("CallAgent() = %s", response)
	}
}

func TestControlClientSendsEffectiveAgentTimeout(t *testing.T) {
	timeoutSeen := make(chan int64, 1)
	client, err := NewControlClient(ControlClientOptions{
		Timeout: 10 * time.Minute,
		Dial: func(context.Context, string) (net.Conn, error) {
			client, service := net.Pipe()
			go func() {
				defer service.Close()
				var request struct {
					Action    string `json:"action"`
					TimeoutMS int64  `json:"timeout_ms"`
				}
				_ = json.NewDecoder(service).Decode(&request)
				timeoutSeen <- request.TimeoutMS
				_, _ = service.Write([]byte(`{"ok":true,"result":{"ok":true}}` + "\n"))
			}()
			return client, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := client.CallAgent(ctx, "/run/control.sock", json.RawMessage(`{"command":"status"}`)); err != nil {
		t.Fatal(err)
	}
	seen := <-timeoutSeen
	if seen < 1_000 || seen > 2_000 {
		t.Fatalf("timeout_ms = %d, want context-limited timeout", seen)
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

func TestControlClientPreservesDialPermissionFailure(t *testing.T) {
	client, err := NewControlClient(ControlClientOptions{
		Timeout: time.Second,
		Dial: func(context.Context, string) (net.Conn, error) {
			return nil, fmt.Errorf("dial unix: %w", os.ErrPermission)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "reset password",
			call: func() error {
				_, err := client.ResetPassword(context.Background(), "/run/control.sock", "")
				return err
			},
		},
		{
			name: "agent",
			call: func() error {
				_, err := client.CallAgent(context.Background(), "/run/control.sock", json.RawMessage(`{"command":"status"}`))
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			if !errors.Is(err, ErrControlUnavailable) {
				t.Fatalf("error = %v, want ErrControlUnavailable", err)
			}
			if !errors.Is(err, os.ErrPermission) {
				t.Fatalf("error = %v, want permission cause", err)
			}
		})
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

func TestControlClientRejectsMalformedAgentResponses(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{name: "server rejection", response: `{"ok":false,"error":"agent request failed"}` + "\n"},
		{name: "unknown field", response: `{"ok":true,"result":{},"extra":true}` + "\n"},
		{name: "missing result", response: `{"ok":true}` + "\n"},
		{name: "password field", response: `{"ok":true,"result":{},"password":"leak"}` + "\n"},
		{name: "trailing value", response: `{"ok":true,"result":{}} {}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewControlClient(ControlClientOptions{
				Timeout: time.Second,
				Dial: func(context.Context, string) (net.Conn, error) {
					client, service := net.Pipe()
					go func() {
						defer service.Close()
						buffer := make([]byte, maxControlMessageBytes)
						_, _ = service.Read(buffer)
						_, _ = service.Write([]byte(tt.response))
					}()
					return client, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.CallAgent(context.Background(), "/run/control.sock", json.RawMessage(`{"command":"status"}`)); err == nil || errors.Is(err, ErrControlUnavailable) {
				t.Fatalf("CallAgent() error = %v", err)
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
	if _, err := client.CallAgent(context.Background(), "", json.RawMessage(`{}`)); err == nil {
		t.Fatal("empty agent socket path accepted")
	}
	if _, err := client.CallAgent(context.Background(), "/run/control.sock", json.RawMessage(`not-json`)); err == nil {
		t.Fatal("invalid agent request accepted")
	}
}
