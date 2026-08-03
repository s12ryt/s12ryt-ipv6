package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Type     string    `json:"type"`
	Resource string    `json:"resource"`
	ID       string    `json:"id,omitempty"`
	Action   string    `json:"action"`
	State    string    `json:"state"`
	Time     time.Time `json:"time"`
}

type EventSubscription struct {
	Events <-chan Event
	close  func()
	once   sync.Once
}

func (s *EventSubscription) Close() {
	if s == nil {
		return
	}
	s.once.Do(s.close)
}

type EventHub struct {
	mu          sync.Mutex
	buffer      int
	nextID      uint64
	subscribers map[uint64]chan Event
}

func NewEventHub(buffer int) (*EventHub, error) {
	if buffer <= 0 {
		return nil, errors.New("event subscriber buffer must be positive")
	}
	return &EventHub{buffer: buffer, subscribers: make(map[uint64]chan Event)}, nil
}

func (h *EventHub) Subscribe() *EventSubscription {
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	channel := make(chan Event, h.buffer)
	h.subscribers[id] = channel
	h.mu.Unlock()
	return &EventSubscription{
		Events: channel,
		close: func() {
			h.mu.Lock()
			if current, exists := h.subscribers[id]; exists {
				delete(h.subscribers, id)
				close(current)
			}
			h.mu.Unlock()
		},
	}
}

func (h *EventHub) Publish(event Event) error {
	if err := validateEvent(event); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, subscriber := range h.subscribers {
		select {
		case subscriber <- event:
		default:
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- event:
			default:
			}
		}
	}
	return nil
}

func validateEvent(event Event) error {
	fields := map[string]string{
		"type": event.Type, "resource": event.Resource, "id": event.ID, "action": event.Action, "state": event.State,
	}
	for name, value := range fields {
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("event %s contains a newline", name)
		}
		if len(value) > 256 {
			return fmt.Errorf("event %s exceeds 256 bytes", name)
		}
	}
	if event.Type == "" || event.Resource == "" || event.Action == "" || event.State == "" {
		return errors.New("event type, resource, action, and state are required")
	}
	if event.Time.IsZero() {
		return errors.New("event time is required")
	}
	return nil
}

func NewSSEHandler(hub *EventHub, heartbeat time.Duration) (http.Handler, error) {
	if hub == nil {
		return nil, errors.New("event hub is required")
	}
	if heartbeat <= 0 {
		return nil, errors.New("SSE heartbeat interval must be positive")
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		flusher, ok := response.(http.Flusher)
		if !ok {
			writeAPIError(response, http.StatusInternalServerError, "streaming unavailable")
			return
		}
		response.Header().Set("Content-Type", "text/event-stream")
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("X-Accel-Buffering", "no")
		subscription := hub.Subscribe()
		defer subscription.Close()
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("event: ready\ndata: {}\n\n"))
		flusher.Flush()

		ticker := time.NewTicker(heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-request.Context().Done():
				return
			case event, open := <-subscription.Events:
				if !open {
					return
				}
				encoded, err := json.Marshal(event)
				if err != nil {
					continue
				}
				if _, err := fmt.Fprintf(response, "event: %s\ndata: %s\n\n", event.Type, encoded); err != nil {
					return
				}
				flusher.Flush()
			case <-ticker.C:
				if _, err := response.Write([]byte(": heartbeat\n\n")); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}), nil
}
