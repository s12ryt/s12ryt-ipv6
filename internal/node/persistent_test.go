package node

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type memoryNodeStateStore struct {
	state   State
	exists  bool
	loadErr error
	saveErr error
	saves   []State
}

func (s *memoryNodeStateStore) Load() (State, bool, error) {
	return cloneState(s.state), s.exists, s.loadErr
}

func (s *memoryNodeStateStore) Save(state State) error {
	s.saves = append(s.saves, cloneState(state))
	if s.saveErr != nil {
		return s.saveErr
	}
	s.state = cloneState(state)
	s.exists = true
	return nil
}

func TestPersistentManagerRestoresAndPersistsMutations(t *testing.T) {
	store := &memoryNodeStateStore{exists: true, state: State{Nodes: []Node{{
		Config: validConfig("node-1", "restored"), Status: StatusStopped,
	}}}}
	manager, _ := NewManager(newFakeRuntimeFactory(), nil, 3)
	service, err := NewPersistentManager(manager, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(context.Background(), "node-1"); err != nil {
		t.Fatal(err)
	}
	if len(store.saves) != 1 || store.state.Nodes[0].Status != StatusRunning || store.state.Nodes[0].Config.Port != 55000 {
		t.Fatalf("saved states = %#v", store.saves)
	}
	if listed := service.List(); len(listed) != 1 || listed[0].Status != StatusRunning {
		t.Fatalf("List() = %#v", listed)
	}
}

func TestPersistentManagerRollsBackCreateWhenSaveFails(t *testing.T) {
	factory := newFakeRuntimeFactory()
	manager, _ := NewManager(factory, nil, 3)
	store := &memoryNodeStateStore{saveErr: errors.New("disk full")}
	service, _ := NewPersistentManager(manager, store)

	if _, err := service.Create(context.Background(), validConfig("node-1", "new"), false); err == nil {
		t.Fatal("Create() error = nil")
	}
	if len(service.List()) != 0 {
		t.Fatalf("nodes after rollback = %#v", service.List())
	}
	runtimes := factory.runtimes["node-1"]
	if len(runtimes) != 1 || runtimes[0].stopCount() != 1 {
		t.Fatalf("created runtime was not stopped: %#v", factory.operations())
	}
}

func TestPersistentManagerRollsBackStartWhenSaveFails(t *testing.T) {
	factory := newFakeRuntimeFactory()
	manager, _ := NewManager(factory, nil, 3)
	if _, err := manager.Create(context.Background(), validConfig("node-1", "start"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Stop(context.Background(), "node-1"); err != nil {
		t.Fatal(err)
	}
	store := &memoryNodeStateStore{saveErr: errors.New("disk full")}
	service, _ := NewPersistentManager(manager, store)

	if _, err := service.Start(context.Background(), "node-1"); err == nil {
		t.Fatal("Start() error = nil")
	}
	got, _ := service.Get("node-1")
	if got.Status != StatusStopped {
		t.Fatalf("node status after rollback = %q", got.Status)
	}
	runtimes := factory.runtimes["node-1"]
	if len(runtimes) != 2 || runtimes[1].stopCount() != 1 {
		t.Fatalf("started runtime was not stopped: %#v", factory.operations())
	}
}

func TestPersistentManagerRollsBackStopWhenSaveFails(t *testing.T) {
	factory := newFakeRuntimeFactory()
	manager, _ := NewManager(factory, nil, 3)
	if _, err := manager.Create(context.Background(), validConfig("node-1", "stop"), false); err != nil {
		t.Fatal(err)
	}
	store := &memoryNodeStateStore{saveErr: errors.New("disk full")}
	service, _ := NewPersistentManager(manager, store)

	if _, err := service.Stop(context.Background(), "node-1"); err == nil {
		t.Fatal("Stop() error = nil")
	}
	got, _ := service.Get("node-1")
	if got.Status != StatusRunning {
		t.Fatalf("node status after rollback = %q", got.Status)
	}
	if len(factory.runtimes["node-1"]) != 2 {
		t.Fatalf("node was not restarted: %#v", factory.operations())
	}
}

func TestPersistentManagerRollsBackUpdateWhenSaveFails(t *testing.T) {
	factory := newFakeRuntimeFactory()
	manager, _ := NewManager(factory, nil, 3)
	original := validConfig("node-1", "original")
	if _, err := manager.Create(context.Background(), original, false); err != nil {
		t.Fatal(err)
	}
	store := &memoryNodeStateStore{saveErr: errors.New("disk full")}
	service, _ := NewPersistentManager(manager, store)
	replacement := original
	replacement.Name = "replacement"

	if _, err := service.Update(context.Background(), "node-1", replacement, false); err == nil {
		t.Fatal("Update() error = nil")
	}
	got, _ := service.Get("node-1")
	if got.Status != StatusRunning || got.Config.Name != original.Name {
		t.Fatalf("node after rollback = %#v", got)
	}
	if runtimes := factory.runtimes["node-1"]; len(runtimes) != 3 || runtimes[1].stopCount() != 1 {
		t.Fatalf("replacement runtime was not stopped: %#v", factory.operations())
	}
}

func TestPersistentManagerRollsBackDeleteBeforeCleaningDedicatedPool(t *testing.T) {
	factory := newFakeRuntimeFactory()
	cleaner := &fakePoolCleaner{}
	manager, _ := NewManager(factory, cleaner, 3)
	config := validConfig("node-1", "delete")
	config.DedicatedPool = "pool-1"
	if _, err := manager.Create(context.Background(), config, false); err != nil {
		t.Fatal(err)
	}
	store := &memoryNodeStateStore{saveErr: errors.New("disk full")}
	service, _ := NewPersistentManager(manager, store)

	if err := service.Delete(context.Background(), "node-1"); err == nil {
		t.Fatal("Delete() error = nil")
	}
	got, found := service.Get("node-1")
	if !found || got.Status != StatusRunning {
		t.Fatalf("node after rollback = %#v, found = %v", got, found)
	}
	cleaner.mu.Lock()
	deleted := append([]string(nil), cleaner.deleted...)
	cleaner.mu.Unlock()
	if len(deleted) != 0 {
		t.Fatalf("dedicated pool was deleted before persistence committed: %#v", deleted)
	}
}

func TestPersistentManagerRejectsSecondRestoreWithoutMutation(t *testing.T) {
	store := &memoryNodeStateStore{exists: true, state: State{Nodes: []Node{{
		Config: validConfig("node-1", "restored"), Status: StatusStopped,
	}}}}
	manager, _ := NewManager(newFakeRuntimeFactory(), nil, 3)
	service, _ := NewPersistentManager(manager, store)
	if err := service.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := service.List()
	if err := service.Restore(context.Background()); err == nil {
		t.Fatal("second Restore() error = nil")
	}
	if !reflect.DeepEqual(service.List(), before) {
		t.Fatal("second restore mutated nodes")
	}
}

func TestPersistentManagerShutdownPreservesDesiredRunningState(t *testing.T) {
	factory := newFakeRuntimeFactory()
	manager, _ := NewManager(factory, nil, 3)
	if _, err := manager.Create(context.Background(), validConfig("node-1", "running"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(context.Background(), validConfig("node-2", "stopped"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Stop(context.Background(), "node-2"); err != nil {
		t.Fatal(err)
	}
	store := &memoryNodeStateStore{}
	service, _ := NewPersistentManager(manager, store)

	if err := service.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.saves) != 1 {
		t.Fatalf("saved states = %#v", store.saves)
	}
	if got := store.state.Nodes[0].Status; got != StatusRunning {
		t.Fatalf("saved running node status = %q", got)
	}
	if got := store.state.Nodes[1].Status; got != StatusStopped {
		t.Fatalf("saved stopped node status = %q", got)
	}
	if got, _ := service.Get("node-1"); got.Status != StatusStopped {
		t.Fatalf("live node status after shutdown = %q", got.Status)
	}
	if runtimes := factory.runtimes["node-1"]; len(runtimes) != 1 || runtimes[0].stopCount() != 1 {
		t.Fatalf("running node was not stopped: %#v", factory.operations())
	}
}

func TestPersistentManagerValidatesDependencies(t *testing.T) {
	manager, _ := NewManager(newFakeRuntimeFactory(), nil, 1)
	if _, err := NewPersistentManager(nil, &memoryNodeStateStore{}); err == nil {
		t.Fatal("NewPersistentManager(nil manager) error = nil")
	}
	if _, err := NewPersistentManager(manager, nil); err == nil {
		t.Fatal("NewPersistentManager(nil store) error = nil")
	}
}
