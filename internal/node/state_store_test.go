package node

import (
	"bytes"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
	"github.com/s12ryt/s12ryt-ipv6/internal/secret"
)

func TestFileStateStoreEncryptsCredentialsAndRoundTripsNodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nodes.yaml")
	vault, err := secret.NewVault(bytes.Repeat([]byte{0x42}, secret.MasterKeySize), bytes.NewReader(bytes.Repeat([]byte{0x24}, 4096)))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewFileStateStore(path, vault)
	if err != nil {
		t.Fatal(err)
	}
	running := Node{Config: validConfig("node-2", "running"), Status: StatusRunning}
	running.Config.Inbound = []proxy.BindSpec{{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Interface: "eth0", Freebind: false}}
	stopped := Node{Config: validConfig("node-1", "stopped"), Status: StatusStopped}
	stopped.Config.Username = ""
	stopped.Config.Password = ""

	if err := store.Save(State{Nodes: []Node{running, stopped}}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(contents, []byte(running.Config.Username)) || bytes.Contains(contents, []byte(running.Config.Password)) {
		t.Fatalf("node state contains plaintext credentials: %s", contents)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if permissions := info.Mode().Perm(); permissions != 0o600 {
			t.Fatalf("node state permissions = %o, want 600", permissions)
		}
	}

	loaded, exists, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("saved node state was not found")
	}
	want := State{Nodes: []Node{stopped, running}}
	if !reflect.DeepEqual(loaded, want) {
		t.Fatalf("loaded state = %#v, want %#v", loaded, want)
	}
}

func TestFileStateStoreTreatsMissingFileAsEmpty(t *testing.T) {
	vault, _ := secret.NewVault(bytes.Repeat([]byte{1}, secret.MasterKeySize), nil)
	store, err := NewFileStateStore(filepath.Join(t.TempDir(), "missing.yaml"), vault)
	if err != nil {
		t.Fatal(err)
	}
	state, exists, err := store.Load()
	if err != nil || exists || len(state.Nodes) != 0 {
		t.Fatalf("Load() = %#v, %v, %v", state, exists, err)
	}
}

func TestFileStateStoreRejectsMalformedOrTamperedState(t *testing.T) {
	directory := t.TempDir()
	vault, _ := secret.NewVault(bytes.Repeat([]byte{2}, secret.MasterKeySize), nil)
	store, _ := NewFileStateStore(filepath.Join(directory, "nodes.yaml"), vault)
	tests := map[string]string{
		"unknown field": `schema_version: 1
nodes: []
unknown: true
`,
		"trailing document": `schema_version: 1
nodes: []
---
schema_version: 1
nodes: []
`,
		"invalid status": `schema_version: 1
nodes:
  - id: node-1
    name: node
    protocol: socks
    status: broken
`,
		"tampered credentials": `schema_version: 1
nodes:
  - id: node-1
    name: node
    protocol: socks
    status: stopped
    username_encrypted: v1.invalid
    password_encrypted: v1.invalid
`,
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(directory, "nodes.yaml"), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.Load(); err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestFileStateStoreValidatesBeforeReplacingExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nodes.yaml")
	vault, _ := secret.NewVault(bytes.Repeat([]byte{3}, secret.MasterKeySize), bytes.NewReader(bytes.Repeat([]byte{4}, 4096)))
	store, _ := NewFileStateStore(path, vault)
	valid := State{Nodes: []Node{{Config: validConfig("node-1", "valid"), Status: StatusStopped}}}
	if err := store.Save(valid); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	invalid := valid
	invalid.Nodes = append(invalid.Nodes, invalid.Nodes[0])
	if err := store.Save(invalid); err == nil {
		t.Fatal("Save(duplicate nodes) error = nil")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, before) {
		t.Fatal("invalid save replaced existing node state")
	}
	if _, err := NewFileStateStore("", vault); err == nil {
		t.Fatal("NewFileStateStore(empty path) error = nil")
	}
	if _, err := NewFileStateStore(path, nil); err == nil {
		t.Fatal("NewFileStateStore(nil vault) error = nil")
	}
}

func TestFileStateStoreRejectsPlaintextCredentialFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nodes.yaml")
	vault, _ := secret.NewVault(bytes.Repeat([]byte{5}, secret.MasterKeySize), nil)
	store, _ := NewFileStateStore(path, vault)
	contents := `schema_version: 1
nodes:
  - id: node-1
    name: node
    protocol: socks
    username: leaked-user
    password: leaked-password
    status: stopped
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := store.Load()
	if err == nil || strings.Contains(err.Error(), "leaked-password") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestFileStateStorePersistsDeclarativeInboundWithoutResolvedBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nodes.yaml")
	vault, err := secret.NewVault(
		bytes.Repeat([]byte{6}, secret.MasterKeySize),
		bytes.NewReader(bytes.Repeat([]byte{7}, 4096)),
	)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewFileStateStore(path, vault)
	if err != nil {
		t.Fatal(err)
	}
	config := validConfig("node-1", "declarative")
	config.InboundMode = InboundDual
	config.InboundResource = "pool-in"
	config.Inbound = []proxy.BindSpec{
		{Protocol: proxy.BindTCP, Family: proxy.BindIPv4},
		{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: netip.MustParseAddr("2001:4860:200::99")},
	}
	want := config
	want.Inbound = nil
	if err := store.Save(State{Nodes: []Node{{Config: config, Status: StatusRunning}}}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	if !strings.Contains(text, "inbound_mode: dual") || !strings.Contains(text, "inbound_resource: pool-in") {
		t.Fatalf("declarative inbound was not persisted: %s", text)
	}
	if strings.Contains(text, "2001:4860:200::99") {
		t.Fatalf("resolved listener leaked into persisted state: %s", text)
	}
	loaded, exists, err := store.Load()
	if err != nil || !exists {
		t.Fatalf("Load() = %#v, %v, %v", loaded, exists, err)
	}
	if len(loaded.Nodes) != 1 || !reflect.DeepEqual(loaded.Nodes[0].Config, want) {
		t.Fatalf("loaded config = %#v, want %#v", loaded.Nodes[0].Config, want)
	}
}
