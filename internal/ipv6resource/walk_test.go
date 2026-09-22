package ipv6resource

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

const walkTestPrefix = "2001:db8:1::/120"

func newWalkTestStore(t *testing.T) *Store {
	t.Helper()
	store := NewStore()
	if err := store.AddTemplate(mustTemplate(t, "walk", walkTestPrefix)); err != nil {
		t.Fatalf("AddTemplate() error = %v", err)
	}
	return store
}

func createWalkTestPool(t *testing.T, store *Store) {
	t.Helper()
	if _, err := store.CreatePool("walk-pool", PoolSharedOutbound, "walk", 2, nil); err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
}

func assertWalkPoolActive(t *testing.T, pool *Pool, want ...string) {
	t.Helper()
	expected := make([]netip.Addr, 0, len(want))
	for _, value := range want {
		expected = append(expected, netip.MustParseAddr(value))
	}
	if !sameAddresses(pool.Active, expected) {
		t.Fatalf("pool %q active = %v, want %v", pool.Name, pool.Active, expected)
	}
}

func TestCreatePoolStartsAtLowestAvailableAddress(t *testing.T) {
	store := newWalkTestStore(t)
	pool, err := store.CreatePool("walk-pool", PoolSharedOutbound, "walk", 2, nil)
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	assertWalkPoolActive(t, pool, "2001:db8:1::", "2001:db8:1::1")
}

func TestRefreshPoolAdvancesAfterDrainCompletes(t *testing.T) {
	store := newWalkTestStore(t)
	createWalkTestPool(t, store)

	first, err := store.RefreshPool("walk-pool")
	if err != nil {
		t.Fatalf("RefreshPool() error = %v", err)
	}
	assertWalkPoolActive(t, first, "2001:db8:1::2", "2001:db8:1::3")
	if len(first.Draining) != 1 {
		t.Fatalf("draining batches = %d, want 1", len(first.Draining))
	}
	if err := store.CompleteDrain("walk-pool", first.Draining[0].ID); err != nil {
		t.Fatalf("CompleteDrain() error = %v", err)
	}

	second, err := store.RefreshPool("walk-pool")
	if err != nil {
		t.Fatalf("RefreshPool() error = %v", err)
	}
	// The addresses released by the completed drain must not be reused before the
	// walk position wraps around the whole prefix.
	assertWalkPoolActive(t, second, "2001:db8:1::4", "2001:db8:1::5")
}

func TestRefreshPoolWalkPositionSurvivesStateRoundTrip(t *testing.T) {
	store := newWalkTestStore(t)
	createWalkTestPool(t, store)

	first, err := store.RefreshPool("walk-pool")
	if err != nil {
		t.Fatalf("RefreshPool() error = %v", err)
	}
	if err := store.CompleteDrain("walk-pool", first.Draining[0].ID); err != nil {
		t.Fatalf("CompleteDrain() error = %v", err)
	}

	restored, err := NewStoreFromState(store.State())
	if err != nil {
		t.Fatalf("NewStoreFromState() error = %v", err)
	}
	second, err := restored.RefreshPool("walk-pool")
	if err != nil {
		t.Fatalf("RefreshPool() error = %v", err)
	}
	assertWalkPoolActive(t, second, "2001:db8:1::4", "2001:db8:1::5")
}

func TestFileStateStorePersistsWalkPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resources.yaml")
	stateStore, err := NewFileStateStore(path)
	if err != nil {
		t.Fatalf("NewFileStateStore() error = %v", err)
	}
	store := newWalkTestStore(t)
	createWalkTestPool(t, store)

	first, err := store.RefreshPool("walk-pool")
	if err != nil {
		t.Fatalf("RefreshPool() error = %v", err)
	}
	if err := store.CompleteDrain("walk-pool", first.Draining[0].ID); err != nil {
		t.Fatalf("CompleteDrain() error = %v", err)
	}
	if err := stateStore.Save(store.State()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, exists, err := stateStore.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !exists {
		t.Fatal("Load() exists = false, want true")
	}
	restored, err := NewStoreFromState(loaded)
	if err != nil {
		t.Fatalf("NewStoreFromState() error = %v", err)
	}
	second, err := restored.RefreshPool("walk-pool")
	if err != nil {
		t.Fatalf("RefreshPool() error = %v", err)
	}
	assertWalkPoolActive(t, second, "2001:db8:1::4", "2001:db8:1::5")
}

func TestFileStateStoreLoadsStateWithoutWalkPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resources.yaml")
	content := "schema_version: 1\n" +
		"templates:\n" +
		"  - name: walk\n" +
		"    prefix: 2001:db8:1::/120\n" +
		"    interface: eth0\n" +
		"    mode: address\n" +
		"fixed: []\n" +
		"addresses: []\n" +
		"pools: []\n" +
		"next_batch: 0\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	stateStore, err := NewFileStateStore(path)
	if err != nil {
		t.Fatalf("NewFileStateStore() error = %v", err)
	}
	loaded, exists, err := stateStore.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !exists {
		t.Fatal("Load() exists = false, want true")
	}
	store, err := NewStoreFromState(loaded)
	if err != nil {
		t.Fatalf("NewStoreFromState() error = %v", err)
	}
	pool, err := store.CreatePool("walk-pool", PoolSharedOutbound, "walk", 2, nil)
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	// State files written before walk positions existed start at the lowest
	// available address.
	assertWalkPoolActive(t, pool, "2001:db8:1::", "2001:db8:1::1")
}
