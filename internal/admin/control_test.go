package admin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

type fakeAgentControlHandler struct {
	request           json.RawMessage
	result            json.RawMessage
	err               error
	deadlineRemaining time.Duration
}

type blockingAgentControlHandler struct {
	started chan struct{}
}

func (h *blockingAgentControlHandler) HandleAgent(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
	close(h.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (h *fakeAgentControlHandler) HandleAgent(ctx context.Context, request json.RawMessage) (json.RawMessage, error) {
	h.request = append(json.RawMessage(nil), request...)
	if deadline, ok := ctx.Deadline(); ok {
		h.deadlineRemaining = time.Until(deadline)
	}
	return h.result, h.err
}

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

func TestControlServerHandlesLargeAgentRequestAndResponse(t *testing.T) {
	if maxControlMessageBytes != 4*1024*1024 {
		t.Fatalf("maxControlMessageBytes = %d", maxControlMessageBytes)
	}
	request := json.RawMessage(`{"command":"apply","document":"` + strings.Repeat("x", 128*1024) + `"}`)
	result := json.RawMessage(`{"ok":true,"data":{"document":"` + strings.Repeat("y", 128*1024) + `"}}`)
	handler := &fakeAgentControlHandler{result: result}
	server, err := NewControlServerWithAgent(&fakePasswordResetter{}, handler, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	client, service := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.HandleConn(service) }()
	encoded, err := json.Marshal(struct {
		Action  string          `json:"action"`
		Request json.RawMessage `json:"request"`
	}{Action: "agent", Request: request})
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	if _, err := client.Write(encoded); err != nil {
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
	if !response.OK || !jsonEqual(response.Result, result) {
		t.Fatalf("response = %#v", response)
	}
	if !jsonEqual(handler.request, request) {
		t.Fatalf("handler request length = %d, want %d", len(handler.request), len(request))
	}
}

func TestControlServerRejectsInvalidAgentEnvelopeAndSanitizesHandlerErrors(t *testing.T) {
	handler := &fakeAgentControlHandler{err: errors.New("sensitive agent state")}
	server, err := NewControlServerWithAgent(&fakePasswordResetter{}, handler, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	for name, request := range map[string]string{
		"missing request": `{"action":"agent"}`,
		"unknown field":   `{"action":"agent","request":{},"secret":"leak"}`,
		"trailing value":  `{"action":"agent","request":{}} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			response, handleErr := exchangeControl(t, server, request)
			if handleErr == nil || response.OK || response.Error != "invalid control request" {
				t.Fatalf("response = %#v, error = %v", response, handleErr)
			}
		})
	}

	response, handleErr := exchangeControl(t, server, `{"action":"agent","request":{"command":"status"}}`)
	if handleErr == nil || response.OK || response.Error != "agent request failed" || strings.Contains(response.Error, "sensitive") {
		t.Fatalf("response = %#v, error = %v", response, handleErr)
	}
}

func TestControlServerUsesRequestedAgentTimeoutWithinConfiguredLimit(t *testing.T) {
	handler := &fakeAgentControlHandler{result: json.RawMessage(`{"ok":true}`)}
	server, err := NewControlServerWithAgentLimits(
		&fakePasswordResetter{},
		handler,
		50*time.Millisecond,
		500*time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}

	response, handleErr := exchangeControl(t, server, `{"action":"agent","timeout_ms":400,"request":{"command":"status"}}`)
	if handleErr != nil || !response.OK {
		t.Fatalf("response = %#v, error = %v", response, handleErr)
	}
	if handler.deadlineRemaining < 250*time.Millisecond || handler.deadlineRemaining > 500*time.Millisecond {
		t.Fatalf("handler deadline remaining = %s", handler.deadlineRemaining)
	}

	response, handleErr = exchangeControl(t, server, `{"action":"agent","timeout_ms":501,"request":{"command":"status"}}`)
	if handleErr == nil || response.OK || response.Error != "invalid control request" {
		t.Fatalf("over-limit response = %#v, error = %v", response, handleErr)
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
	if _, err := NewControlServerWithAgent(&fakePasswordResetter{}, nil, time.Second); err == nil {
		t.Fatal("NewControlServerWithAgent(nil handler) error = nil")
	}
	if _, err := NewControlServerWithAgentLimits(&fakePasswordResetter{}, &fakeAgentControlHandler{}, time.Second, 0); err == nil {
		t.Fatal("NewControlServerWithAgentLimits(max=0) error = nil")
	}
	if _, err := NewControlServerWithAgentLimits(&fakePasswordResetter{}, &fakeAgentControlHandler{}, time.Second, 500*time.Millisecond); err == nil {
		t.Fatal("NewControlServerWithAgentLimits(max < read timeout) error = nil")
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

func TestControlServerCancellationStopsActiveAgentRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	handler := &blockingAgentControlHandler{started: make(chan struct{})}
	server, err := NewControlServerWithAgentLimits(&fakePasswordResetter{}, handler, 5*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Write([]byte(`{"action":"agent","timeout_ms":5000,"request":{"command":"status"}}` + "\n")); err != nil {
		cancel()
		t.Fatal(err)
	}
	select {
	case <-handler.started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("agent handler did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Serve did not cancel the active agent request")
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

func jsonEqual(left, right json.RawMessage) bool {
	var leftValue any
	var rightValue any
	return json.Unmarshal(left, &leftValue) == nil &&
		json.Unmarshal(right, &rightValue) == nil &&
		fmt.Sprint(leftValue) == fmt.Sprint(rightValue)
}
