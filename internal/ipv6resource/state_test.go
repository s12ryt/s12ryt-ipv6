package ipv6resource

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreStateRoundTripPreservesReferencesAndDrainSequence(t *testing.T) {
	store := NewStore()
	if err := store.AddTemplate(mustTemplate(t, "edge", "2001:db8:60::/120")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateFixedAddress("fixed", "edge", netip.MustParseAddr("2001:db8:60::f0"), OwnershipAddress); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool("shared", PoolSharedOutbound, "edge", 3, []string{"fixed"}); err != nil {
		t.Fatal(err)
	}
	firstRefresh, err := store.RefreshPool("shared")
	if err != nil {
		t.Fatal(err)
	}
	if got := firstRefresh.Draining[0].ID; got != "drain-1" {
		t.Fatalf("first drain ID = %q, want drain-1", got)
	}

	restored, err := NewStoreFromState(store.State())
	if err != nil {
		t.Fatalf("NewStoreFromState() error = %v", err)
	}
	secondRefresh, err := restored.RefreshPool("shared")
	if err != nil {
		t.Fatal(err)
	}
	if got := secondRefresh.Draining[len(secondRefresh.Draining)-1].ID; got != "drain-2" {
		t.Fatalf("second drain ID = %q, want drain-2", got)
	}
	if got, want := len(restored.Addresses()), len(store.Addresses())+2; got != want {
		t.Fatalf("restored address count after refresh = %d, want %d", got, want)
	}

	state := store.State()
	state.Pools[0].Active[0] = netip.MustParseAddr("2001:db8:ffff::1")
	if got := store.Pool("shared").Active[0]; got == state.Pools[0].Active[0] {
		t.Fatal("mutating State() snapshot changed live store")
	}
}

func TestNewStoreFromStateRejectsInconsistentCanonicalReferences(t *testing.T) {
	store := NewStore()
	if err := store.AddTemplate(mustTemplate(t, "edge", "2001:db8:61::/120")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateFixedAddress("fixed", "edge", netip.MustParseAddr("2001:db8:61::1"), OwnershipAddress); err != nil {
		t.Fatal(err)
	}
	state := store.State()
	state.Addresses[0].References = 99

	if _, err := NewStoreFromState(state); err == nil || !strings.Contains(err.Error(), "references") {
		t.Fatalf("NewStoreFromState() error = %v, want reference validation", err)
	}
}

func TestNewStoreFromStateRejectsDuplicateFixedAddressIdentity(t *testing.T) {
	store := NewStore()
	if err := store.AddTemplate(mustTemplate(t, "edge", "2001:db8:63::/120")); err != nil {
		t.Fatal(err)
	}
	fixed, err := store.CreateFixedAddress("first", "edge", netip.MustParseAddr("2001:db8:63::1"), OwnershipAddress)
	if err != nil {
		t.Fatal(err)
	}
	state := store.State()
	state.Fixed = append(state.Fixed, FixedAddress{Name: "second", Template: fixed.Template, Address: fixed.Address, Ownership: fixed.Ownership})
	state.Addresses[0].References++

	if _, err := NewStoreFromState(state); err == nil || !strings.Contains(err.Error(), "more than one fixed") {
		t.Fatalf("NewStoreFromState() error = %v, want duplicate fixed identity rejection", err)
	}
}

func TestStoreRejectsFixedOwnershipThatDiffersFromTemplateMode(t *testing.T) {
	store := NewStore()
	if err := store.AddTemplate(mustTemplate(t, "edge", "2001:db8:64::/120")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateFixedAddress("fixed", "edge", netip.MustParseAddr("2001:db8:64::1"), OwnershipExternal); err == nil {
		t.Fatal("CreateFixedAddress() error = nil, want ownership mismatch rejection")
	}
}

func TestNewStoreFromStateRejectsAutomaticAddressOwnershipMismatch(t *testing.T) {
	store := NewStore()
	if err := store.AddTemplate(mustTemplate(t, "edge", "2001:db8:65::/120")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool("pool", PoolInbound, "edge", 1, nil); err != nil {
		t.Fatal(err)
	}
	state := store.State()
	state.Addresses[0].Ownership = OwnershipExternal

	if _, err := NewStoreFromState(state); err == nil || !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("NewStoreFromState() error = %v, want automatic ownership rejection", err)
	}
}

func TestFileStateStoreRoundTripAndStrictParsing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resources.yaml")
	fileStore, err := NewFileStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if state, exists, err := fileStore.Load(); err != nil || exists || len(state.Templates) != 0 {
		t.Fatalf("Load(missing) = %#v, %v, %v", state, exists, err)
	}

	store := NewStore()
	if err := store.AddTemplate(mustTemplate(t, "edge", "2001:db8:62::/120")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool("inbound", PoolInbound, "edge", 2, nil); err != nil {
		t.Fatal(err)
	}
	if err := fileStore.Save(store.State()); err != nil {
		t.Fatal(err)
	}
	loaded, exists, err := fileStore.Load()
	if err != nil || !exists {
		t.Fatalf("Load() = exists %v, error %v", exists, err)
	}
	if _, err := NewStoreFromState(loaded); err != nil {
		t.Fatalf("loaded state is invalid: %v", err)
	}
	if got := loaded.Pools[0].Active; len(got) != 2 || got[0].String() != "2001:db8:62::" {
		t.Fatalf("loaded active addresses = %v", got)
	}
	if _, err := store.RefreshPool("inbound"); err != nil {
		t.Fatal(err)
	}
	if err := fileStore.Save(store.State()); err != nil {
		t.Fatalf("Save(second state) error = %v", err)
	}
	updated, exists, err := fileStore.Load()
	if err != nil || !exists || len(updated.Pools[0].Draining) != 1 {
		t.Fatalf("Load(second state) = %#v, %v, %v", updated, exists, err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 && os.PathSeparator != '\\' {
		t.Fatalf("state mode = %o, want 600", info.Mode().Perm())
	}

	invalid := []byte("schema_version: 1\ntemplates: []\nfixed: []\naddresses: []\npools: []\nnext_batch: 0\nunknown: true\n")
	if err := os.WriteFile(path, invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fileStore.Load(); err == nil {
		t.Fatal("Load(unknown field) error = nil")
	}
	trailing := []byte("schema_version: 1\ntemplates: []\nfixed: []\naddresses: []\npools: []\nnext_batch: 0\n---\nschema_version: 1\n")
	if err := os.WriteFile(path, trailing, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fileStore.Load(); err == nil {
		t.Fatal("Load(trailing document) error = nil")
	}
}

func TestFileStateStoreRejectsInvalidStateBeforeReplacingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resources.yaml")
	fileStore, err := NewFileStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	valid := State{}
	if err := fileStore.Save(valid); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	invalid := State{Templates: []PrefixTemplate{{Name: "bad", Prefix: netip.MustParsePrefix("fd00::/64"), Interface: "eth0", Mode: ModeAddress}}}
	if err := fileStore.Save(invalid); err == nil {
		t.Fatal("Save(invalid state) error = nil")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("invalid save replaced the previous state")
	}
}
