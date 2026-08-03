package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/stats"
)

func TestLoadStatisticsCreatesEmptyRegistryWhenStateIsMissing(t *testing.T) {
	registry, err := LoadStatistics(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Snapshot().Nodes) != 0 {
		t.Fatalf("snapshot = %#v", registry.Snapshot())
	}
}

func TestLoadStatisticsRestoresCumulativeCountersOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "statistics.json")
	if err := stats.Save(path, stats.Snapshot{Nodes: map[string]stats.NodeCounters{
		"node-a": {ActiveTCP: 9, ActiveUDP: 8, TotalConnections: 7, BytesUp: 6, BytesDown: 5, Errors: 4},
	}}); err != nil {
		t.Fatal(err)
	}

	registry, err := LoadStatistics(path)
	if err != nil {
		t.Fatal(err)
	}
	counters := registry.Snapshot().Nodes["node-a"]
	if counters.ActiveTCP != 0 || counters.ActiveUDP != 0 || counters.TotalConnections != 7 || counters.BytesUp != 6 || counters.BytesDown != 5 || counters.Errors != 4 {
		t.Fatalf("restored counters = %#v", counters)
	}
}

func TestLoadStatisticsRejectsCorruptStateAndInvalidPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "statistics.json")
	if err := os.WriteFile(path, []byte(`{"nodes":{},"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStatistics(path); err == nil {
		t.Fatal("LoadStatistics(corrupt) error = nil")
	}
	if _, err := LoadStatistics(" "); err == nil {
		t.Fatal("LoadStatistics(empty path) error = nil")
	}
}
