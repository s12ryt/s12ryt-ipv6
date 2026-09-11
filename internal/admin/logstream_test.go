package admin

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/eventlog"
)

func TestLogStreamHandlerRejectsInvalidOptions(t *testing.T) {
	if _, err := NewLogStreamHandler(nil, time.Second); err == nil {
		t.Fatal("NewLogStreamHandler(nil, ·) succeeded")
	}
	if _, err := NewLogStreamHandler(func() *eventlog.LogSubscription { return nil }, 0); err == nil {
		t.Fatal("NewLogStreamHandler(·, 0) succeeded")
	}
}

func TestLogStreamHandlerStreamsLogEvents(t *testing.T) {
	logger, err := eventlog.New(filepath.Join(t.TempDir(), "events.jsonl"), 1024*1024, 5, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logger.Close() })
	handler, err := NewLogStreamHandler(logger.Subscribe, time.Hour)
	if err != nil {
		t.Fatalf("NewLogStreamHandler() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://manager.example/api/logs/stream", nil)
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

	if err := logger.Write(eventlog.Event{Kind: eventlog.KindProxy, Action: "connection.closed", Success: false, Error: "proxy connection failed: fd limit reached"}); err != nil {
		t.Fatal(err)
	}
	waitForFlush(t, recorder)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("log stream handler did not stop after context cancellation")
	}

	body := recorder.String()
	if !strings.Contains(body, "event: ready\n") {
		t.Fatalf("body missing ready frame: %q", body)
	}
	if !strings.Contains(body, "event: log\n") || !strings.Contains(body, `"action":"connection.closed"`) || !strings.Contains(body, `"error":"proxy connection failed: fd limit reached"`) {
		t.Fatalf("body missing log event payload: %q", body)
	}
	if recorder.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", recorder.Header().Get("Content-Type"))
	}
}

func TestLogStreamHandlerStopsWhenSubscriptionCloses(t *testing.T) {
	logger, err := eventlog.New(filepath.Join(t.TempDir(), "events.jsonl"), 1024*1024, 5, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewLogStreamHandler(logger.Subscribe, time.Hour)
	if err != nil {
		t.Fatalf("NewLogStreamHandler() error = %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, "http://manager.example/api/logs/stream", nil)
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

	logger.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("log stream handler did not stop when the subscription closed")
	}
}
