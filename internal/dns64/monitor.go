package dns64

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"time"
)

const DefaultProbeInterval = 60 * time.Second

type NAT64State string

const (
	NAT64Healthy  NAT64State = "healthy"
	NAT64Degraded NAT64State = "degraded"
)

type NAT64Status struct {
	State       NAT64State
	Prefix      netip.Prefix
	Source      string
	Conflict    bool
	Manual      bool
	LastChecked time.Time
	Error       string
}

type NAT64Probe func(context.Context, netip.Prefix) (Discovery, error)

type NAT64Monitor struct {
	mu       sync.RWMutex
	checkMu  sync.Mutex
	interval time.Duration
	manual   netip.Prefix
	probe    NAT64Probe
	now      func() time.Time
	status   NAT64Status
}

func NewNAT64Monitor(interval time.Duration, manual netip.Prefix, probe NAT64Probe, now func() time.Time) (*NAT64Monitor, error) {
	if interval <= 0 {
		return nil, errors.New("NAT64 probe interval must be positive")
	}
	if manual.IsValid() {
		if err := ValidateNAT64Prefix(manual); err != nil {
			return nil, err
		}
		manual = manual.Masked()
	}
	if probe == nil {
		return nil, errors.New("NAT64 probe is required")
	}
	if now == nil {
		now = time.Now
	}
	return &NAT64Monitor{
		interval: interval,
		manual:   manual,
		probe:    probe,
		now:      now,
		status:   NAT64Status{State: NAT64Degraded, Manual: manual.IsValid(), Error: "NAT64 has not been checked"},
	}, nil
}

func (m *NAT64Monitor) Check(ctx context.Context) NAT64Status {
	m.checkMu.Lock()
	defer m.checkMu.Unlock()

	m.mu.RLock()
	manual := m.manual
	m.mu.RUnlock()
	discovery, err := m.probe(ctx, manual)
	checked := m.now()
	status := NAT64Status{State: NAT64Degraded, Manual: manual.IsValid(), LastChecked: checked}
	if err != nil {
		status.Error = err.Error()
	} else if manual.IsValid() {
		status.State = NAT64Healthy
		status.Prefix = manual
		status.Source = "manual"
	} else if validateErr := ValidateNAT64Prefix(discovery.Prefix); validateErr != nil {
		status.Error = validateErr.Error()
	} else {
		status.State = NAT64Healthy
		status.Prefix = discovery.Prefix.Masked()
		status.Source = discovery.Source
		status.Conflict = discovery.Conflict
	}
	m.mu.Lock()
	m.status = status
	m.mu.Unlock()
	return status
}

func (m *NAT64Monitor) Status() NAT64Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *NAT64Monitor) ManualPrefix() netip.Prefix {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.manual
}

func (m *NAT64Monitor) SetManualPrefix(prefix netip.Prefix) error {
	if prefix.IsValid() {
		if err := ValidateNAT64Prefix(prefix); err != nil {
			return err
		}
		prefix = prefix.Masked()
	}
	m.checkMu.Lock()
	defer m.checkMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.manual = prefix
	m.status = NAT64Status{
		State:  NAT64Degraded,
		Manual: prefix.IsValid(),
		Error:  "NAT64 configuration has not been checked",
	}
	return nil
}

func (m *NAT64Monitor) Run(ctx context.Context) {
	m.Check(ctx)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Check(ctx)
		}
	}
}
