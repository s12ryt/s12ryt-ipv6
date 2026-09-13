package app

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFileWatchdogRestartStateStoreRoundTripAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "watchdog-restart.json")
	store, err := NewFileWatchdogRestartStateStore(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, present, err := store.Load(); err != nil || present {
		t.Fatalf("Load() before Save = (_, %v, %v), want absent without error", present, err)
	}
	want := WatchdogRestartState{
		Node:        "node-1",
		Address:     "[2001:db8::10]:1080",
		Protocol:    "socks",
		RestartedAt: time.Unix(1_800_000_000, 0).UTC(),
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, present, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !present || !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = (%#v, %v), want (%#v, true)", got, present, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("state permissions = %o, want 600", got)
		}
	}

	if err := store.Clear(); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if err := store.Clear(); err != nil {
		t.Fatalf("second Clear() error = %v", err)
	}
	if _, present, err := store.Load(); err != nil || present {
		t.Fatalf("Load() after Clear = (_, %v, %v), want absent without error", present, err)
	}
}

func TestFileWatchdogRestartStateStoreRejectsInvalidInput(t *testing.T) {
	for _, path := range []string{"", " ", "\t\r\n"} {
		if _, err := NewFileWatchdogRestartStateStore(path); err == nil {
			t.Fatalf("NewFileWatchdogRestartStateStore(%q) error = nil", path)
		}
	}

	store, err := NewFileWatchdogRestartStateStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	invalid := []WatchdogRestartState{
		{},
		{Node: "node", Address: "address", Protocol: "socks"},
		{Node: " ", Address: "address", Protocol: "socks", RestartedAt: time.Now()},
	}
	for _, state := range invalid {
		if err := store.Save(state); err == nil {
			t.Fatalf("Save(%#v) error = nil", state)
		}
	}
}

func TestFileWatchdogRestartStateStoreRejectsCorruptOrTrailingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watchdog-restart.json")
	store, err := NewFileWatchdogRestartStateStore(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, contents := range []string{
		`{"schema_version":999,"node":"node","address":"address","protocol":"socks","restarted_at":"2027-01-15T08:00:00Z"}`,
		`{"schema_version":1,"node":"node","address":"address","protocol":"socks","restarted_at":"2027-01-15T08:00:00Z","unknown":true}`,
		`{"schema_version":1,"node":"node","address":"address","protocol":"socks","restarted_at":"2027-01-15T08:00:00Z"} {}`,
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "watchdog restart state") {
			t.Fatalf("Load(%q) error = %v, want watchdog restart state error", contents, err)
		}
	}
}
