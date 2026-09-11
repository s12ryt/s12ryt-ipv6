package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/eventlog"
)

// logStreamHandler streams event log records over server-sent events. Every
// request creates its own subscription so slow clients never delay others.
type logStreamHandler struct {
	subscribe func() *eventlog.LogSubscription
	heartbeat time.Duration
}

// NewLogStreamHandler returns a handler that serves GET /api/logs/stream.
// Each request subscribes to the event log and receives "log" frames plus
// periodic heartbeats until the client disconnects or the log closes.
func NewLogStreamHandler(subscribe func() *eventlog.LogSubscription, heartbeat time.Duration) (http.Handler, error) {
	if subscribe == nil {
		return nil, fmt.Errorf("log stream subscription source is required")
	}
	if heartbeat <= 0 {
		return nil, fmt.Errorf("log stream heartbeat must be positive")
	}
	return &logStreamHandler{subscribe: subscribe, heartbeat: heartbeat}, nil
}

func (h *logStreamHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	flusher, ok := response.(http.Flusher)
	if !ok {
		http.Error(response, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Accel-Buffering", "no")
	response.WriteHeader(http.StatusOK)
	fmt.Fprint(response, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	subscription := h.subscribe()
	if subscription == nil || subscription.Events == nil {
		return
	}
	defer subscription.Close()

	ctx := request.Context()
	ticker := time.NewTicker(h.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-subscription.Events:
			if !ok {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(response, "event: log\ndata: %s\n\n", payload)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(response, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
