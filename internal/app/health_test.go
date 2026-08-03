package app

import (
	"errors"
	"sync"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/admin"
)

func TestHealthTrackerAggregatesAndRecoversSubsystems(t *testing.T) {
	tracker := NewHealthTracker()
	if got := tracker.State(); got != admin.HealthHealthy {
		t.Fatalf("initial state = %q, want healthy", got)
	}

	tracker.ReportDegraded("nat64", errors.New("probe failed"))
	tracker.ReportDegraded("resources", errors.New("DAD failed"))
	if got := tracker.State(); got != admin.HealthDegraded {
		t.Fatalf("degraded state = %q, want degraded", got)
	}
	if issues := tracker.Issues(); len(issues) != 2 || issues[0].Component != "nat64" || issues[1].Component != "resources" {
		t.Fatalf("issues = %#v, want sorted subsystem failures", issues)
	}

	tracker.ReportUnhealthy("management", errors.New("listener stopped"))
	if got := tracker.State(); got != admin.HealthUnhealthy {
		t.Fatalf("unhealthy state = %q, want unhealthy", got)
	}

	tracker.Recover("management")
	if got := tracker.State(); got != admin.HealthDegraded {
		t.Fatalf("state after critical recovery = %q, want degraded", got)
	}
	tracker.Recover("nat64")
	tracker.Recover("resources")
	if got := tracker.State(); got != admin.HealthHealthy {
		t.Fatalf("state after recovery = %q, want healthy", got)
	}
}

func TestHealthTrackerRejectsInvalidReportsAndReturnsSnapshots(t *testing.T) {
	tracker := NewHealthTracker()
	tracker.ReportDegraded("", errors.New("ignored"))
	tracker.ReportDegraded("dns", nil)
	tracker.ReportUnhealthy("firewall", errors.New("apply failed"))

	issues := tracker.Issues()
	if len(issues) != 1 {
		t.Fatalf("issues = %#v, want one valid report", issues)
	}
	issues[0].Component = "mutated"
	if got := tracker.Issues()[0].Component; got != "firewall" {
		t.Fatalf("tracker snapshot was aliased: %q", got)
	}
}

func TestHealthTrackerIsSafeForConcurrentUpdates(t *testing.T) {
	tracker := NewHealthTracker()
	var wait sync.WaitGroup
	for index := 0; index < 100; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			tracker.ReportDegraded("worker", errors.New("temporary failure"))
			_ = tracker.State()
			tracker.Recover("worker")
		}()
	}
	wait.Wait()
	if got := tracker.State(); got != admin.HealthHealthy {
		t.Fatalf("final state = %q, want healthy", got)
	}
}
