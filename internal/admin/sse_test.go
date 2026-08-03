package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEventHubPublishesSanitizedEventsToSubscribers(t *testing.T) {
	hub, err := NewEventHub(2)
	if err != nil {
		t.Fatalf("NewEventHub() error = %v", err)
	}
	subscription := hub.Subscribe()
	defer subscription.Close()
	event := Event{
		Type:     "node.changed",
		Resource: "node",
		ID:       "node-1",
		Action:   "updated",
		State:    "running",
		Time:     time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC),
	}
	if err := hub.Publish(event); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	select {
	case got := <-subscription.Events:
		if got != event {
			t.Fatalf("event = %#v, want %#v", got, event)
		}
	case <-time.After(time.Second):
		t.Fatal("subscription did not receive event")
	}

	invalid := event
	invalid.Type = "node.changed\ndata: leaked"
	if err := hub.Publish(invalid); err == nil {
		t.Fatal("Publish(event with newline) succeeded")
	}
}

func TestEventHubDoesNotBlockOnSlowSubscribers(t *testing.T) {
	hub, err := NewEventHub(1)
	if err != nil {
		t.Fatal(err)
	}
	subscription := hub.Subscribe()
	defer subscription.Close()
	for _, id := range []string{"one", "two", "three"} {
		if err := hub.Publish(Event{Type: "node.changed", Resource: "node", ID: id, Action: "updated", State: "running", Time: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case got := <-subscription.Events:
		if got.ID != "three" {
			t.Fatalf("slow subscriber event ID = %q, want latest three", got.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("slow subscriber channel was empty")
	}
}

func TestEventSubscriptionCloseIsIdempotent(t *testing.T) {
	hub, err := NewEventHub(1)
	if err != nil {
		t.Fatal(err)
	}
	subscription := hub.Subscribe()
	subscription.Close()
	subscription.Close()
	if err := hub.Publish(Event{Type: "system.changed", Resource: "system", Action: "updated", State: "healthy", Time: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, open := <-subscription.Events; open {
		t.Fatal("subscription channel remained open")
	}
}

type streamRecorder struct {
	mu      sync.Mutex
	header  http.Header
	status  int
	body    strings.Builder
	flushed chan struct{}
}

func newStreamRecorder() *streamRecorder {
	return &streamRecorder{header: make(http.Header), flushed: make(chan struct{}, 8)}
}

func (r *streamRecorder) Header() http.Header { return r.header }

func (r *streamRecorder) WriteHeader(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == 0 {
		r.status = status
	}
}

func (r *streamRecorder) Write(value []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(value)
}

func (r *streamRecorder) Flush() {
	select {
	case r.flushed <- struct{}{}:
	default:
	}
}

func (r *streamRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body.String()
}

func waitForFlush(t *testing.T, recorder *streamRecorder) {
	t.Helper()
	select {
	case <-recorder.flushed:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not flush")
	}
}

func TestSSEHandlerStreamsReadyAndPublishedEvent(t *testing.T) {
	hub, err := NewEventHub(4)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewSSEHandler(hub, time.Hour)
	if err != nil {
		t.Fatalf("NewSSEHandler() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://manager.example/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := newStreamRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(recorder, request)
		close(done)
	}()
	waitForFlush(t, recorder)

	event := Event{Type: "node.changed", Resource: "node", ID: "node-1", Action: "updated", State: "running", Time: time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)}
	if err := hub.Publish(event); err != nil {
		t.Fatal(err)
	}
	waitForFlush(t, recorder)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SSE handler did not stop after context cancellation")
	}

	body := recorder.String()
	if !strings.Contains(body, "event: ready\n") || !strings.Contains(body, "event: node.changed\n") {
		t.Fatalf("SSE body missing events: %q", body)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "data: "+string(encoded)+"\n\n") {
		t.Fatalf("SSE body missing JSON payload: %q", body)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestEventHubAndSSEHandlerRejectInvalidOptions(t *testing.T) {
	if _, err := NewEventHub(0); err == nil {
		t.Fatal("NewEventHub(0) succeeded")
	}
	hub, _ := NewEventHub(1)
	if _, err := NewSSEHandler(nil, time.Second); err == nil {
		t.Fatal("NewSSEHandler(nil) succeeded")
	}
	if _, err := NewSSEHandler(hub, 0); err == nil {
		t.Fatal("NewSSEHandler(zero heartbeat) succeeded")
	}
}
