package stats

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestRegistryTracksActiveAndCumulativeCounters(t *testing.T) {
	registry := NewRegistry()
	registry.TCPOpened("edge")
	registry.UDPAssociationOpened("edge")
	registry.TCPClosed("edge", 100, 200, false)
	registry.UDPAssociationClosed("edge", 20, 30, true)

	got := registry.Snapshot().Nodes["edge"]
	if got.ActiveTCP != 0 || got.ActiveUDP != 0 {
		t.Errorf("active counters = TCP %d UDP %d", got.ActiveTCP, got.ActiveUDP)
	}
	if got.TotalConnections != 2 || got.BytesUp != 120 || got.BytesDown != 230 || got.Errors != 1 {
		t.Fatalf("cumulative counters = %#v", got)
	}
}

func TestRegistryResetPreservesActiveConnections(t *testing.T) {
	registry := NewRegistry()
	registry.TCPOpened("edge")
	registry.TCPClosed("edge", 100, 200, true)
	registry.TCPOpened("edge")

	registry.ResetNode("edge")
	got := registry.Snapshot().Nodes["edge"]
	if got.ActiveTCP != 1 {
		t.Fatalf("active TCP = %d, want 1", got.ActiveTCP)
	}
	if got.TotalConnections != 0 || got.BytesUp != 0 || got.BytesDown != 0 || got.Errors != 0 {
		t.Fatalf("cumulative counters not reset: %#v", got)
	}
}

func TestRegistryResetAllPreservesEveryNodesActiveConnections(t *testing.T) {
	registry := NewRegistry()
	registry.TCPOpened("edge-a")
	registry.TCPOpened("edge-b")
	registry.UDPAssociationOpened("edge-b")

	registry.ResetAll()

	a := registry.Snapshot().Nodes["edge-a"]
	b := registry.Snapshot().Nodes["edge-b"]
	if a.ActiveTCP != 1 || b.ActiveTCP != 1 || b.ActiveUDP != 1 {
		t.Fatalf("active counters changed: edge-a=%#v edge-b=%#v", a, b)
	}
	if a.TotalConnections != 0 || b.TotalConnections != 0 {
		t.Fatalf("cumulative counters not reset: edge-a=%#v edge-b=%#v", a, b)
	}
}

func TestRegistryIsSafeUnderConcurrentUpdates(t *testing.T) {
	registry := NewRegistry()
	var wait sync.WaitGroup
	for range 100 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			registry.TCPOpened("edge")
			registry.TCPClosed("edge", 1, 2, false)
		}()
	}
	wait.Wait()

	got := registry.Snapshot().Nodes["edge"]
	if got.ActiveTCP != 0 || got.TotalConnections != 100 || got.BytesUp != 100 || got.BytesDown != 200 {
		t.Fatalf("counters = %#v", got)
	}
}

func TestRegistryRemoveNodeDeletesCounters(t *testing.T) {
	registry := NewRegistry()
	registry.TCPOpened("edge")
	registry.TCPOpened("other")

	registry.RemoveNode("edge")

	snapshot := registry.Snapshot()
	if _, exists := snapshot.Nodes["edge"]; exists {
		t.Fatalf("removed node still present in snapshot: %v", snapshot.Nodes)
	}
	if _, exists := snapshot.Nodes["other"]; !exists {
		t.Fatalf("unrelated node missing from snapshot: %v", snapshot.Nodes)
	}
}

func TestRegistryRemoveNodeIgnoresUnknownAndEmptyNodes(t *testing.T) {
	registry := NewRegistry()
	registry.TCPOpened("edge")

	registry.RemoveNode("")
	registry.RemoveNode("missing")

	snapshot := registry.Snapshot()
	if len(snapshot.Nodes) != 1 {
		t.Fatalf("snapshot nodes = %v", snapshot.Nodes)
	}
	if _, exists := snapshot.Nodes["edge"]; !exists {
		t.Fatalf("edge node missing after no-op removals: %v", snapshot.Nodes)
	}
}

func TestSnapshotSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	registry := NewRegistry()
	registry.TCPOpened("edge")
	registry.TCPClosed("edge", 12, 34, true)

	if err := Save(path, registry.Snapshot()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := loaded.Nodes["edge"]
	if got.TotalConnections != 1 || got.BytesUp != 12 || got.BytesDown != 34 || got.Errors != 1 {
		t.Fatalf("loaded counters = %#v", got)
	}
	if got.ActiveTCP != 0 || got.ActiveUDP != 0 {
		t.Fatalf("persisted active counters must restart at zero: %#v", got)
	}
}

func TestRegistryRestoresPersistedCountersWithoutActiveConnections(t *testing.T) {
	registry, err := NewRegistryFromSnapshot(Snapshot{Nodes: map[string]NodeCounters{
		"edge": {
			ActiveTCP: 9, ActiveUDP: 8, TotalConnections: 7,
			BytesUp: 123, BytesDown: 456, Errors: 2,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := registry.Snapshot().Nodes["edge"]
	if got.ActiveTCP != 0 || got.ActiveUDP != 0 || got.TotalConnections != 7 || got.BytesUp != 123 || got.BytesDown != 456 || got.Errors != 2 {
		t.Fatalf("restored counters = %#v", got)
	}
	if _, err := NewRegistryFromSnapshot(Snapshot{Nodes: map[string]NodeCounters{"": {}}}); err == nil {
		t.Fatal("NewRegistryFromSnapshot(empty node) error = nil")
	}
}
