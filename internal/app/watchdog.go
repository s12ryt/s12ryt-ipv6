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

// WatchdogRestartState identifies a target for which the watchdog has already
// requested one service restart. Credentials are intentionally excluded.
type WatchdogRestartState struct {
	Node        string
	Address     string
	Protocol    string
	RestartedAt time.Time
}

// WatchdogRestartStateStore persists the one-restart guard across service
// process replacement.
type WatchdogRestartStateStore interface {
	Load() (WatchdogRestartState, bool, error)
	Save(WatchdogRestartState) error
	Clear() error
}

// WatchdogOptions configures the self-healing proxy watchdog. All fields
// except Restart are required; a nil Restart switches the watchdog into
// log-only mode.
type WatchdogOptions struct {
	Interval     time.Duration
	Timeout      time.Duration
	Failures     int
	Cooldown     time.Duration
	Targets      func() []ProbeTarget
	Probe        func(context.Context, ProbeTarget) error
	Restart      func() error
	RestartState WatchdogRestartStateStore
	Now          func() time.Time
	Random       func(n int) int
	OnEvent      func(eventlog.Event)
}

type watchdog struct {
	options     WatchdogOptions
	mu          sync.Mutex
	failures    map[probeTargetKey]int
	lastRestart time.Time
}

type probeTargetKey struct {
	node     string
	address  string
	protocol string
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
	if options.RestartState == nil {
		return nil, errors.New("watchdog restart state store is required")
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
	return &watchdog{options: options, failures: make(map[probeTargetKey]int)}, nil
}

// Run probes one running proxy listener every Interval until the context is
// cancelled. Targets with the least failure evidence are sampled first, and a
// service restart is considered only after every running target reaches the
// failure threshold.
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
	w.dropRemovedTargets(targets)
	restartState, restartGuarded, stateErr := w.options.RestartState.Load()
	if stateErr != nil {
		w.options.OnEvent(eventlog.Event{
			Kind:    eventlog.KindSystem,
			Action:  "watchdog.state",
			Success: false,
			Error:   "load restart guard: " + stateErr.Error(),
		})
		return
	}
	if len(targets) == 0 {
		// Without running nodes there is no active data-plane failure episode.
		if !restartGuarded {
			return
		}
		if err := w.options.RestartState.Clear(); err != nil {
			w.options.OnEvent(eventlog.Event{
				Kind:    eventlog.KindSystem,
				Action:  "watchdog.state",
				Success: false,
				Node:    restartState.Node,
				Error:   "clear restart guard without targets: " + err.Error(),
			})
		}
		return
	}
	target := w.selectTarget(targets)
	probeCtx, cancel := context.WithTimeout(ctx, w.options.Timeout)
	err := w.options.Probe(probeCtx, target)
	cancel()

	w.mu.Lock()
	defer w.mu.Unlock()
	key := probeTargetKey{node: target.Node, address: target.Address, protocol: target.Protocol}
	if err == nil {
		clear(w.failures)
		if restartGuarded {
			if clearErr := w.options.RestartState.Clear(); clearErr != nil {
				w.options.OnEvent(eventlog.Event{
					Kind:    eventlog.KindSystem,
					Action:  "watchdog.state",
					Success: false,
					Node:    target.Node,
					Error:   "clear recovered restart guard: " + clearErr.Error(),
				})
				return
			}
			w.options.OnEvent(eventlog.Event{
				Kind:    eventlog.KindSystem,
				Action:  "watchdog.recovered",
				Success: true,
				Node:    target.Node,
			})
		}
		return
	}
	if w.failures[key] < w.options.Failures {
		w.failures[key]++
	}
	w.options.OnEvent(eventlog.Event{
		Kind:    eventlog.KindSystem,
		Action:  "watchdog.probe",
		Success: false,
		Node:    target.Node,
		Error:   "proxy probe failed: " + describeDialError(err),
	})
	if restartGuarded {
		return
	}
	if !w.allTargetsFailedAtThresholdLocked(targets) {
		return
	}
	now := w.options.Now()
	if !w.lastRestart.IsZero() && now.Sub(w.lastRestart) < w.options.Cooldown {
		return
	}
	clear(w.failures)
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
	state := WatchdogRestartState{
		Node:        target.Node,
		Address:     target.Address,
		Protocol:    target.Protocol,
		RestartedAt: now,
	}
	if stateErr := w.options.RestartState.Save(state); stateErr != nil {
		w.options.OnEvent(eventlog.Event{
			Kind:    eventlog.KindSystem,
			Action:  "watchdog.restart",
			Success: false,
			Node:    target.Node,
			Error:   "persist restart guard: " + stateErr.Error(),
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
		if clearErr := w.options.RestartState.Clear(); clearErr != nil {
			event.Error += "; clear restart guard: " + clearErr.Error()
		}
	}
	w.options.OnEvent(event)
}

func (w *watchdog) selectTarget(targets []ProbeTarget) ProbeTarget {
	w.mu.Lock()
	minFailures := w.failures[probeTargetKey{
		node: targets[0].Node, address: targets[0].Address, protocol: targets[0].Protocol,
	}]
	candidates := make([]ProbeTarget, 0, len(targets))
	for _, target := range targets {
		key := probeTargetKey{node: target.Node, address: target.Address, protocol: target.Protocol}
		failures := w.failures[key]
		switch {
		case failures < minFailures:
			minFailures = failures
			candidates = append(candidates[:0], target)
		case failures == minFailures:
			candidates = append(candidates, target)
		}
	}
	w.mu.Unlock()
	return candidates[w.options.Random(len(candidates))]
}

func (w *watchdog) allTargetsFailedAtThresholdLocked(targets []ProbeTarget) bool {
	for _, target := range targets {
		key := probeTargetKey{node: target.Node, address: target.Address, protocol: target.Protocol}
		if w.failures[key] < w.options.Failures {
			return false
		}
	}
	return true
}

func (w *watchdog) dropRemovedTargets(targets []ProbeTarget) {
	active := make(map[probeTargetKey]struct{}, len(targets))
	for _, target := range targets {
		active[probeTargetKey{node: target.Node, address: target.Address, protocol: target.Protocol}] = struct{}{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for key := range w.failures {
		if _, ok := active[key]; !ok {
			delete(w.failures, key)
		}
	}
}

func (w *watchdog) failureCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	total := 0
	for _, failures := range w.failures {
		total += failures
	}
	return total
}
