package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/eventlog"
)

// ProbeTarget describes one running proxy listener that the watchdog can probe.
type ProbeTarget struct {
	Node     string
	Address  string
	Protocol string
	Username string
	Password string
}

// WatchdogOptions configures the self-healing proxy watchdog. All fields
// except Restart are required; a nil Restart switches the watchdog into
// log-only mode.
type WatchdogOptions struct {
	Interval time.Duration
	Timeout  time.Duration
	Failures int
	Cooldown time.Duration
	Targets  func() []ProbeTarget
	Probe    func(context.Context, ProbeTarget) error
	Restart  func() error
	Now      func() time.Time
	Random   func(n int) int
	OnEvent  func(eventlog.Event)
}

type watchdog struct {
	options     WatchdogOptions
	mu          sync.Mutex
	failures    int
	lastRestart time.Time
}

// NewWatchdog validates the options and returns a watchdog ready to Run.
func NewWatchdog(options WatchdogOptions) (*watchdog, error) {
	if options.Interval <= 0 {
		return nil, errors.New("watchdog interval must be positive")
	}
	if options.Timeout <= 0 {
		return nil, errors.New("watchdog timeout must be positive")
	}
	if options.Failures <= 0 {
		return nil, errors.New("watchdog failure threshold must be positive")
	}
	if options.Targets == nil {
		return nil, errors.New("watchdog targets function is required")
	}
	if options.Probe == nil {
		return nil, errors.New("watchdog probe function is required")
	}
	if options.Now == nil {
		return nil, errors.New("watchdog clock function is required")
	}
	if options.Random == nil {
		return nil, errors.New("watchdog random function is required")
	}
	if options.OnEvent == nil {
		return nil, errors.New("watchdog event writer is required")
	}
	return &watchdog{options: options}, nil
}

// Run probes one randomly selected running proxy listener every Interval
// until the context is cancelled.
func (w *watchdog) Run(ctx context.Context) {
	ticker := time.NewTicker(w.options.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.probeOnce(ctx)
		}
	}
}

func (w *watchdog) probeOnce(ctx context.Context) {
	targets := w.options.Targets()
	if len(targets) == 0 {
		// Without running nodes there is nothing to guard; do not count
		// this as a failure.
		return
	}
	target := targets[w.options.Random(len(targets))]
	probeCtx, cancel := context.WithTimeout(ctx, w.options.Timeout)
	err := w.options.Probe(probeCtx, target)
	cancel()

	w.mu.Lock()
	defer w.mu.Unlock()
	if err == nil {
		w.failures = 0
		return
	}
	w.failures++
	w.options.OnEvent(eventlog.Event{
		Kind:    eventlog.KindSystem,
		Action:  "watchdog.probe",
		Success: false,
		Node:    target.Node,
		Error:   "proxy probe failed: " + describeDialError(err),
	})
	if w.failures < w.options.Failures {
		return
	}
	now := w.options.Now()
	if !w.lastRestart.IsZero() && now.Sub(w.lastRestart) < w.options.Cooldown {
		return
	}
	w.failures = 0
	w.lastRestart = now
	if w.options.Restart == nil {
		w.options.OnEvent(eventlog.Event{
			Kind:    eventlog.KindSystem,
			Action:  "watchdog.restart",
			Success: false,
			Node:    target.Node,
			Error:   "service restart unavailable",
		})
		return
	}
	restartErr := w.options.Restart()
	event := eventlog.Event{
		Kind:    eventlog.KindSystem,
		Action:  "watchdog.restart",
		Success: restartErr == nil,
		Node:    target.Node,
	}
	if restartErr != nil {
		event.Error = "service restart failed: " + restartErr.Error()
	}
	w.options.OnEvent(event)
}

func (w *watchdog) failureCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failures
}
