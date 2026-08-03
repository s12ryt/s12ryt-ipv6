package app

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/config"
)

func TestConfigStoreCreatesDefaultsAndPersistsRuntimeUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	store, err := NewConfigStore(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, created, err := store.LoadOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if !created || loaded.Management.Port != 34466 {
		t.Fatalf("LoadOrCreate() = %#v, created=%v", loaded, created)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file was not created: %v", err)
	}

	prefix := netip.MustParsePrefix("64:ff9b::/96")
	if err := store.SaveNAT64(prefix); err != nil {
		t.Fatal(err)
	}
	resolvers := []config.Resolver{{
		Name: "Custom", Address: "2001:4860:4860::6464", Port: 853, ServerName: "dns.google", Enabled: true,
	}}
	if err := store.SaveResolvers(resolvers); err != nil {
		t.Fatal(err)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.NAT64Prefix != prefix.String() || len(reloaded.Resolvers) != 1 || reloaded.Resolvers[0].Name != "Custom" {
		t.Fatalf("persisted config = %#v", reloaded)
	}

	snapshot := store.Snapshot()
	snapshot.Resolvers[0].Name = "mutated"
	if store.Snapshot().Resolvers[0].Name != "Custom" {
		t.Fatal("Snapshot() aliases live resolver configuration")
	}
}

func TestConfigStoreLoadsExistingConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	want := config.Default()
	want.Management.Port = 45555
	if err := config.Save(path, want); err != nil {
		t.Fatal(err)
	}
	store, err := NewConfigStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, created, err := store.LoadOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	if created || got.Management.Port != 45555 {
		t.Fatalf("LoadOrCreate() = %#v, created=%v", got, created)
	}
}

func TestConfigStoreRejectsInvalidUpdatesWithoutChangingDiskOrMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	store, err := NewConfigStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.LoadOrCreate(); err != nil {
		t.Fatal(err)
	}
	before := store.Snapshot()
	invalid := []config.Resolver{{Name: "IPv4", Address: "1.1.1.1", Port: 853, ServerName: "example", Enabled: true}}
	if err := store.SaveResolvers(invalid); err == nil {
		t.Fatal("SaveResolvers(invalid) error = nil")
	}
	if got := store.Snapshot(); got.Resolvers[0] != before.Resolvers[0] || got.NAT64Prefix != before.NAT64Prefix {
		t.Fatalf("live config changed after rejected update: %#v", got)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Resolvers[0] != before.Resolvers[0] {
		t.Fatal("disk config changed after rejected update")
	}
}

func TestNewConfigStoreRejectsEmptyPath(t *testing.T) {
	if _, err := NewConfigStore(" "); err == nil {
		t.Fatal("NewConfigStore(empty) error = nil")
	}
}
