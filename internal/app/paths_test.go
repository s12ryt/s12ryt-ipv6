package app

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestNewDataPathsBuildsEveryPersistentPath(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "nested", "..", "state")

	paths, err := NewDataPaths(directory)
	if err != nil {
		t.Fatal(err)
	}

	root := filepath.Clean(directory)
	want := DataPaths{
		Directory:        root,
		Configuration:    filepath.Join(root, "config.yaml"),
		Resources:        filepath.Join(root, "resources.yaml"),
		Nodes:            filepath.Join(root, "nodes.yaml"),
		NetworkOwnership: filepath.Join(root, "network-ownership.yaml"),
		Statistics:       filepath.Join(root, "statistics.json"),
		MasterKey:        filepath.Join(root, "master.key"),
		AdminPassword:    filepath.Join(root, "admin-password.yaml"),
		EventLog:         filepath.Join(root, "events.jsonl"),
		ControlSocket:    filepath.Join(root, "control.sock"),
		ServiceLock:      filepath.Join(root, "service.lock"),
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("NewDataPaths() = %#v, want %#v", paths, want)
	}
}

func TestNewDataPathsRejectsEmptyDirectory(t *testing.T) {
	for _, directory := range []string{"", " ", "\t\r\n"} {
		if _, err := NewDataPaths(directory); err == nil {
			t.Fatalf("NewDataPaths(%q) error = nil", directory)
		}
	}
}
