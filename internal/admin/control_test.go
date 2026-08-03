package admin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

type fakePasswordResetter struct {
	replacement string
	result      string
	err         error
}

func (r *fakePasswordResetter) Reset(replacement string) (string, error) {
	r.replacement = replacement
	return r.result, r.err
}

func TestControlServerResetsPasswordAndReturnsGeneratedValue(t *testing.T) {
	resetter := &fakePasswordResetter{result: "generated-password"}
	server, err := NewControlServer(resetter, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client, service := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.HandleConn(service) }()

	if _, err := client.Write([]byte("{\"action\":\"reset_password\",\"new_password\":\"\"}\n")); err != nil {
		t.Fatal(err)
	}
	var response ControlResponse
	if err := json.NewDecoder(bufio.NewReader(client)).Decode(&response); err != nil {
		t.Fatal(err)
	}
	client.Close()
	if err := <-done; err != nil {
		t.Fatalf("HandleConn() error = %v", err)
	}
	if !response.OK || response.Password != "generated-password" || resetter.replacement != "" {
		t.Fatalf("response = %#v, replacement = %q", response, resetter.replacement)
	}
}

func TestControlServerRejectsUnknownFieldsAndSanitizesResetErrors(t *testing.T) {
	for name, request := range map[string]string{
		"unknown action": "{\"action\":\"status\"}",
		"unknown field":  "{\"action\":\"reset_password\",\"secret\":\"leak\"}",
		"trailing value": "{\"action\":\"reset_password\"} {}",
	} {
		t.Run(name, func(t *testing.T) {
			server, err := NewControlServer(&fakePasswordResetter{}, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			response, handleErr := exchangeControl(t, server, request)
			if handleErr == nil || response.OK || response.Error == "" {
				t.Fatalf("response = %#v, error = %v", response, handleErr)
			}
		})
	}

	server, err := NewControlServer(&fakePasswordResetter{err: errors.New("sensitive hash path")}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response, handleErr := exchangeControl(t, server, "{\"action\":\"reset_password\",\"new_password\":\"valid-password-value\"}")
	if handleErr == nil || response.OK || strings.Contains(response.Error, "sensitive") || response.Error != "password reset failed" {
		t.Fatalf("response = %#v, error = %v", response, handleErr)
	}
}

func TestControlServerValidatesOptions(t *testing.T) {
	if _, err := NewControlServer(nil, time.Second); err == nil {
		t.Fatal("NewControlServer(nil) error = nil")
	}
	if _, err := NewControlServer(&fakePasswordResetter{}, 0); err == nil {
		t.Fatal("NewControlServer(timeout=0) error = nil")
	}
}

func TestControlServerServeStopsWithContext(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewControlServer(&fakePasswordResetter{}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after context cancellation")
	}
}

func exchangeControl(t *testing.T, server *ControlServer, request string) (ControlResponse, error) {
	t.Helper()
	client, service := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.HandleConn(service) }()
	if _, err := client.Write([]byte(request + "\n")); err != nil {
		t.Fatal(err)
	}
	var response ControlResponse
	if err := json.NewDecoder(client).Decode(&response); err != nil {
		t.Fatal(err)
	}
	client.Close()
	return response, <-done
}
