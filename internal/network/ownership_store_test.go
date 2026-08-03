package network

import (
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFileOwnershipStoreRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime", "network-ownership.yaml")
	store, err := NewFileOwnershipStore(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Ownership{
		Addresses: []AddressRef{{Interface: "eth0", Address: netip.MustParseAddr("2001:4860:1::1")}},
		Routes:    []RouteRef{{Interface: "eth0", Prefix: netip.MustParsePrefix("2001:4860:2::/64")}},
	}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Addresses) != 1 || got.Addresses[0] != want.Addresses[0] || len(got.Routes) != 1 || got.Routes[0] != want.Routes[0] {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("ownership permissions = %o, want 600", got)
		}
	}
}

func TestFileOwnershipStoreMissingFileIsEmpty(t *testing.T) {
	store, err := NewFileOwnershipStore(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Addresses) != 0 || len(got.Routes) != 0 {
		t.Fatalf("Load() = %#v, want empty ownership", got)
	}
}

func TestFileOwnershipStoreRejectsUnknownFieldsAndTrailingDocuments(t *testing.T) {
	for name, content := range map[string]string{
		"unknown":  "schema_version: 1\naddresses: []\nroutes: []\nextra: true\n",
		"trailing": "schema_version: 1\naddresses: []\nroutes: []\n---\nschema_version: 1\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ownership.yaml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := NewFileOwnershipStore(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "ownership") {
				t.Fatalf("Load() error = %v, want ownership parse error", err)
			}
		})
	}
}
