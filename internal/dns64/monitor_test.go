package dns64

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"
)

func TestNAT64MonitorTransitionsFromHealthyToDegradedAndClearsUsablePrefix(t *testing.T) {
	now := time.Date(2026, 8, 3, 13, 0, 0, 0, time.UTC)
	probeError := false
	probe := func(context.Context, netip.Prefix) (Discovery, error) {
		if probeError {
			return Discovery{}, errors.New("gateway unavailable")
		}
		return Discovery{Prefix: netip.MustParsePrefix("64:ff9b::/96"), Source: "primary"}, nil
	}
	monitor, err := NewNAT64Monitor(DefaultProbeInterval, netip.Prefix{}, probe, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	monitor.Check(context.Background())
	healthy := monitor.Status()
	if healthy.State != NAT64Healthy || healthy.Prefix != netip.MustParsePrefix("64:ff9b::/96") || healthy.Source != "primary" {
		t.Fatalf("healthy status = %#v", healthy)
	}

	probeError = true
	now = now.Add(DefaultProbeInterval)
	monitor.Check(context.Background())
	degraded := monitor.Status()
	if degraded.State != NAT64Degraded || degraded.Prefix.IsValid() || degraded.Error == "" || degraded.LastChecked != now {
		t.Fatalf("degraded status = %#v", degraded)
	}
}

func TestNAT64MonitorPassesManualPrefixToProbeAndMarksSource(t *testing.T) {
	manual := netip.MustParsePrefix("2001:db8:64::/96")
	var got netip.Prefix
	probe := func(_ context.Context, prefix netip.Prefix) (Discovery, error) {
		got = prefix
		return Discovery{Prefix: netip.MustParsePrefix("64:ff9b::/96"), Source: "ignored"}, nil
	}
	monitor, err := NewNAT64Monitor(DefaultProbeInterval, manual, probe, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	monitor.Check(context.Background())
	status := monitor.Status()
	if got != manual || status.Prefix != manual || status.Source != "manual" || !status.Manual {
		t.Fatalf("probe prefix=%s status=%#v", got, status)
	}
}

func TestNAT64MonitorRunChecksImmediatelyAndPeriodically(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	called := make(chan struct{}, 3)
	probe := func(context.Context, netip.Prefix) (Discovery, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		called <- struct{}{}
		return Discovery{Prefix: netip.MustParsePrefix("64:ff9b::/96"), Source: "primary"}, nil
	}
	monitor, err := NewNAT64Monitor(10*time.Millisecond, netip.Prefix{}, probe, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		monitor.Run(ctx)
		close(done)
	}()
	for range 2 {
		select {
		case <-called:
		case <-time.After(time.Second):
			t.Fatal("monitor did not probe on schedule")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor did not stop after context cancellation")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls < 2 {
		t.Fatalf("probe calls = %d, want at least 2", calls)
	}
}

func TestNewNAT64MonitorRejectsInvalidConfiguration(t *testing.T) {
	probe := func(context.Context, netip.Prefix) (Discovery, error) { return Discovery{}, nil }
	if _, err := NewNAT64Monitor(0, netip.Prefix{}, probe, time.Now); err == nil {
		t.Fatal("zero interval accepted")
	}
	if _, err := NewNAT64Monitor(time.Minute, netip.MustParsePrefix("64:ff9b::/64"), probe, time.Now); err == nil {
		t.Fatal("non-/96 manual prefix accepted")
	}
}

func TestNAT64MonitorManualPrefixChangeInvalidatesPreviousHealthUntilChecked(t *testing.T) {
	probe := func(_ context.Context, prefix netip.Prefix) (Discovery, error) {
		return Discovery{Prefix: prefix, Source: "probe"}, nil
	}
	monitor, err := NewNAT64Monitor(DefaultProbeInterval, netip.Prefix{}, probe, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	monitor.Check(context.Background())
	manual := netip.MustParsePrefix("2001:4860:64::/96")
	if err := monitor.SetManualPrefix(manual); err != nil {
		t.Fatal(err)
	}
	if got := monitor.ManualPrefix(); got != manual {
		t.Fatalf("ManualPrefix() = %s, want %s", got, manual)
	}
	pending := monitor.Status()
	if pending.State != NAT64Degraded || pending.Prefix.IsValid() || !pending.Manual {
		t.Fatalf("status after manual update = %#v", pending)
	}
	checked := monitor.Check(context.Background())
	if checked.State != NAT64Healthy || checked.Prefix != manual {
		t.Fatalf("checked status = %#v", checked)
	}
	if err := monitor.SetManualPrefix(netip.MustParsePrefix("64:ff9b::/64")); err == nil {
		t.Fatal("invalid manual prefix update accepted")
	}
	if got := monitor.ManualPrefix(); got != manual {
		t.Fatalf("invalid update changed ManualPrefix() to %s", got)
	}
}
