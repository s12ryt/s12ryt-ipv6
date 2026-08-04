package node

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestManagerMovesNodeWithoutRestartingRuntime(t *testing.T) {
	factory := newFakeRuntimeFactory()
	manager, _ := NewManager(factory, nil, 5)
	config := validConfig("node-1", "first")
	config.Folder = "來源"
	if _, err := manager.Create(context.Background(), config, false); err != nil {
		t.Fatal(err)
	}

	moved, err := manager.MoveToFolder(context.Background(), "node-1", "  目標  ")
	if err != nil {
		t.Fatal(err)
	}
	if moved.Config.Folder != "目標" || !reflect.DeepEqual(factory.operations(), []string{"start:first"}) {
		t.Fatalf("moved = %#v, operations = %#v", moved, factory.operations())
	}
	unclassified, err := manager.MoveToFolder(context.Background(), "node-1", "")
	if err != nil || unclassified.Config.Folder != "" {
		t.Fatalf("MoveToFolder(unclassified) = %#v, %v", unclassified, err)
	}
}

func TestManagerRenamesFolderAtomicallyInMemory(t *testing.T) {
	manager, _ := NewManager(newFakeRuntimeFactory(), nil, 5)
	for index, id := range []string{"node-1", "node-2"} {
		config := validConfig(id, id)
		config.Folder = "來源"
		if index == 1 {
			config.Port = 52001
		}
		if _, err := manager.Create(context.Background(), config, false); err != nil {
			t.Fatal(err)
		}
	}
	target := validConfig("node-3", "node-3")
	target.Folder = "目標"
	if _, err := manager.Create(context.Background(), target, false); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.RenameFolder(context.Background(), "來源", "目標"); !errors.Is(err, ErrFolderExists) {
		t.Fatalf("RenameFolder(conflict) error = %v", err)
	}
	renamed, err := manager.RenameFolder(context.Background(), "來源", "  新名稱 ")
	if err != nil {
		t.Fatal(err)
	}
	if len(renamed) != 2 || renamed[0].Config.ID != "node-1" || renamed[1].Config.ID != "node-2" {
		t.Fatalf("renamed nodes = %#v", renamed)
	}
	for _, current := range manager.List() {
		if current.Config.ID != "node-3" && current.Config.Folder != "新名稱" {
			t.Fatalf("folder was not renamed: %#v", current)
		}
	}
	if _, err := manager.RenameFolder(context.Background(), "不存在", "other"); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("RenameFolder(missing) error = %v", err)
	}
}
