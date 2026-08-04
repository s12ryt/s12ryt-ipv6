package node

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type fakeRuntime struct {
	mu        sync.Mutex
	name      string
	log       *[]string
	stopError error
	stops     int
}

func (r *fakeRuntime) Port() uint16 { return 55000 }

func (r *fakeRuntime) Stop(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stops++
	*r.log = append(*r.log, "stop:"+r.name)
	return r.stopError
}

func (r *fakeRuntime) stopCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stops
}

type fakeRuntimeFactory struct {
	mu         sync.Mutex
	log        []string
	startError map[string]error
	runtimes   map[string][]*fakeRuntime
}

func newFakeRuntimeFactory() *fakeRuntimeFactory {
	return &fakeRuntimeFactory{startError: make(map[string]error), runtimes: make(map[string][]*fakeRuntime)}
}

func (f *fakeRuntimeFactory) Start(_ context.Context, config Config) (Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, "start:"+config.Name)
	if err := f.startError[config.Name]; err != nil {
		return nil, err
	}
	runtime := &fakeRuntime{name: config.Name, log: &f.log}
	f.runtimes[config.ID] = append(f.runtimes[config.ID], runtime)
	return runtime, nil
}

func (f *fakeRuntimeFactory) operations() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

type fakePoolCleaner struct {
	mu      sync.Mutex
	deleted []string
	err     error
}

func (c *fakePoolCleaner) DeleteDedicatedPool(_ context.Context, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleted = append(c.deleted, name)
	return c.err
}

func validConfig(id, name string) Config {
	return Config{
		ID: id, Name: name, Protocol: ProtocolSOCKS,
		Username: "alice", Password: "correct-horse-battery",
		MaxTCP: 4096, MaxUDP: 1024, DialTimeout: 10 * time.Second,
		HandshakeTimeout: 30 * time.Second, UDPIdleTimeout: 5 * time.Minute,
		ULAOverride: policy.ULAInherit, Outbound: "shared-primary",
		Inbound: []proxy.BindSpec{{Protocol: proxy.BindTCP, Family: proxy.BindIPv4}},
	}
}

func TestNormalizeFolderName(t *testing.T) {
	got, err := NormalizeFolderName("  批次 1  ")
	if err != nil || got != "批次 1" {
		t.Fatalf("NormalizeFolderName() = %q, %v", got, err)
	}
	if got, err := NormalizeFolderName(""); err != nil || got != "" {
		t.Fatalf("NormalizeFolderName(empty) = %q, %v", got, err)
	}
	if _, err := NormalizeFolderName(strings.Repeat("節", 65)); err == nil {
		t.Fatal("NormalizeFolderName(long name) error = nil")
	}
	if _, err := NormalizeFolderName("批次\n一"); err == nil {
		t.Fatal("NormalizeFolderName(control character) error = nil")
	}
}

func TestManagerCreatesAndImmediatelyStartsNode(t *testing.T) {
	factory := newFakeRuntimeFactory()
	manager, err := NewManager(factory, &fakePoolCleaner{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := validConfig("node-1", "primary")
	got, err := manager.Create(context.Background(), want, false)
	if err != nil {
		t.Fatal(err)
	}
	want.Port = 55000
	if got.Status != StatusRunning || !reflect.DeepEqual(got.Config, want) {
		t.Fatalf("node = %#v", got)
	}
	if operations := factory.operations(); !reflect.DeepEqual(operations, []string{"start:primary"}) {
		t.Fatalf("operations = %#v", operations)
	}
}

func TestManagerCreateBatchPreflightsBeforeStartingAnyRuntime(t *testing.T) {
	tests := map[string][]Config{
		"invalid config": {
			validConfig("node-1", "first"),
			func() Config { value := validConfig("node-2", "second"); value.Outbound = ""; return value }(),
		},
		"duplicate ID": {
			validConfig("node-1", "first"),
			validConfig("node-1", "second"),
		},
		"duplicate name": {
			validConfig("node-1", "same"),
			validConfig("node-2", "same"),
		},
		"duplicate manual port": {
			func() Config { value := validConfig("node-1", "first"); value.Port = 52000; return value }(),
			func() Config { value := validConfig("node-2", "second"); value.Port = 52000; return value }(),
		},
	}
	for name, configs := range tests {
		t.Run(name, func(t *testing.T) {
			factory := newFakeRuntimeFactory()
			manager, _ := NewManager(factory, nil, 10)
			for index := range configs {
				configs[index].Folder = "批次 1"
			}
			if _, err := manager.CreateBatch(context.Background(), configs, false); err == nil {
				t.Fatal("CreateBatch() error = nil")
			}
			if operations := factory.operations(); len(operations) != 0 {
				t.Fatalf("runtime operations before complete preflight = %#v", operations)
			}
			if len(manager.List()) != 0 {
				t.Fatalf("nodes after rejected batch = %#v", manager.List())
			}
		})
	}
}

func TestManagerCreateBatchRollsBackStartedNodesInReverseOrder(t *testing.T) {
	factory := newFakeRuntimeFactory()
	factory.startError["third"] = errors.New("bind failed")
	manager, _ := NewManager(factory, nil, 10)
	configs := []Config{
		validConfig("node-1", "first"),
		validConfig("node-2", "second"),
		validConfig("node-3", "third"),
	}
	for index := range configs {
		configs[index].Folder = "批次 1"
	}

	if _, err := manager.CreateBatch(context.Background(), configs, false); err == nil {
		t.Fatal("CreateBatch() error = nil")
	}
	wantOperations := []string{"start:first", "start:second", "start:third", "stop:second", "stop:first"}
	if operations := factory.operations(); !reflect.DeepEqual(operations, wantOperations) {
		t.Fatalf("operations = %#v, want %#v", operations, wantOperations)
	}
	if len(manager.List()) != 0 {
		t.Fatalf("nodes after rollback = %#v", manager.List())
	}
}

func TestManagerCreateBatchCreatesEveryNodeWithOneUniqueFolder(t *testing.T) {
	manager, _ := NewManager(newFakeRuntimeFactory(), nil, 10)
	configs := []Config{validConfig("node-1", "first"), validConfig("node-2", "second")}
	for index := range configs {
		configs[index].Folder = "  批次 1  "
	}
	created, err := manager.CreateBatch(context.Background(), configs, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 2 || created[0].Config.Folder != "批次 1" || created[1].Config.Folder != "批次 1" {
		t.Fatalf("created nodes = %#v", created)
	}
	conflicting := validConfig("node-3", "third")
	conflicting.Folder = "批次 1"
	if _, err := manager.CreateBatch(context.Background(), []Config{conflicting}, false); !errors.Is(err, ErrFolderExists) {
		t.Fatalf("CreateBatch(existing folder) error = %v", err)
	}
}

func TestManagerRequiresExplicitUnauthenticatedRiskConfirmation(t *testing.T) {
	factory := newFakeRuntimeFactory()
	manager, err := NewManager(factory, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	config := validConfig("node-1", "public")
	config.Username = ""
	config.Password = ""
	if _, err := manager.Create(context.Background(), config, false); !errors.Is(err, ErrUnauthenticatedRiskConfirmation) {
		t.Fatalf("Create() error = %v", err)
	}
	if _, found := manager.Get("node-1"); found {
		t.Fatal("unconfirmed node was persisted")
	}
	if len(factory.operations()) != 0 {
		t.Fatalf("operations = %#v", factory.operations())
	}
	if _, err := manager.Create(context.Background(), config, true); err != nil {
		t.Fatal(err)
	}
}

func TestManagerFailedCreateLeavesNoNode(t *testing.T) {
	factory := newFakeRuntimeFactory()
	factory.startError["broken"] = errors.New("bind failed")
	manager, _ := NewManager(factory, nil, 2)
	if _, err := manager.Create(context.Background(), validConfig("node-1", "broken"), false); err == nil {
		t.Fatal("Create() error = nil")
	}
	if _, found := manager.Get("node-1"); found {
		t.Fatal("failed node was persisted")
	}
}

func TestManagerRunningUpdateIsTransactional(t *testing.T) {
	factory := newFakeRuntimeFactory()
	manager, _ := NewManager(factory, nil, 2)
	original := validConfig("node-1", "original")
	if _, err := manager.Create(context.Background(), original, false); err != nil {
		t.Fatal(err)
	}
	oldRuntime := factory.runtimes["node-1"][0]
	factory.startError["broken"] = errors.New("firewall failed")
	broken := original
	broken.Name = "broken"
	if _, err := manager.Update(context.Background(), "node-1", broken, false); err == nil {
		t.Fatal("Update() error = nil")
	}
	got, _ := manager.Get("node-1")
	if got.Config.Name != "original" || got.Status != StatusRunning || oldRuntime.stopCount() != 0 {
		t.Fatalf("node after failed update = %#v, old stops = %d", got, oldRuntime.stopCount())
	}

	replacement := original
	replacement.Name = "replacement"
	got, err := manager.Update(context.Background(), "node-1", replacement, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.Name != "replacement" || got.Status != StatusRunning || oldRuntime.stopCount() != 1 {
		t.Fatalf("node after update = %#v, old stops = %d", got, oldRuntime.stopCount())
	}
	wantOperations := []string{"start:original", "start:broken", "start:replacement", "stop:original"}
	if operations := factory.operations(); !reflect.DeepEqual(operations, wantOperations) {
		t.Fatalf("operations = %#v, want %#v", operations, wantOperations)
	}
}

func TestManagerKeepsReplacementRunningWhenPreviousCleanupFails(t *testing.T) {
	factory := newFakeRuntimeFactory()
	manager, _ := NewManager(factory, nil, 2)
	original := validConfig("node-1", "original")
	if _, err := manager.Create(context.Background(), original, false); err != nil {
		t.Fatal(err)
	}
	oldRuntime := factory.runtimes["node-1"][0]
	oldRuntime.stopError = errors.New("listener cleanup failed")
	replacement := original
	replacement.Name = "replacement"
	got, err := manager.Update(context.Background(), original.ID, replacement, false)
	if !errors.Is(err, ErrPreviousRuntimeCleanup) {
		t.Fatalf("Update() error = %v, want ErrPreviousRuntimeCleanup", err)
	}
	if got.Config.Name != "replacement" || got.Status != StatusRunning {
		t.Fatalf("Update() node = %#v", got)
	}
	current, found := manager.Get(original.ID)
	if !found || current.Config.Name != "replacement" || current.Status != StatusRunning {
		t.Fatalf("committed node = %#v, found %v", current, found)
	}
	newRuntime := factory.runtimes["node-1"][1]
	if newRuntime.stopCount() != 0 {
		t.Fatal("replacement runtime was stopped after previous cleanup failure")
	}
}

func TestManagerRestartPersistsAutomaticallyAllocatedPort(t *testing.T) {
	factory := newFakeRuntimeFactory()
	manager, _ := NewManager(factory, nil, 2)
	config := validConfig("node-1", "primary")
	if _, err := manager.Create(context.Background(), config, false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Stop(context.Background(), config.ID); err != nil {
		t.Fatal(err)
	}
	config.Port = 0
	if _, err := manager.Update(context.Background(), config.ID, config, false); err != nil {
		t.Fatal(err)
	}
	started, err := manager.Start(context.Background(), config.ID)
	if err != nil {
		t.Fatal(err)
	}
	if started.Config.Port != 55000 {
		t.Fatalf("restarted node port = %d, want 55000", started.Config.Port)
	}
}

func TestManagerStopStartAndDeleteCleanDedicatedPool(t *testing.T) {
	factory := newFakeRuntimeFactory()
	cleaner := &fakePoolCleaner{}
	manager, _ := NewManager(factory, cleaner, 2)
	config := validConfig("node-1", "primary")
	config.DedicatedPool = "node-1-outbound"
	if _, err := manager.Create(context.Background(), config, false); err != nil {
		t.Fatal(err)
	}
	if got, err := manager.Stop(context.Background(), "node-1"); err != nil || got.Status != StatusStopped {
		t.Fatalf("Stop() = %#v, %v", got, err)
	}
	if got, err := manager.Start(context.Background(), "node-1"); err != nil || got.Status != StatusRunning {
		t.Fatalf("Start() = %#v, %v", got, err)
	}
	if err := manager.Delete(context.Background(), "node-1"); err != nil {
		t.Fatal(err)
	}
	if _, found := manager.Get("node-1"); found {
		t.Fatal("deleted node still exists")
	}
	if !reflect.DeepEqual(cleaner.deleted, []string{"node-1-outbound"}) {
		t.Fatalf("deleted pools = %#v", cleaner.deleted)
	}
	wantOperations := []string{"start:primary", "stop:primary", "start:primary", "stop:primary"}
	if operations := factory.operations(); !reflect.DeepEqual(operations, wantOperations) {
		t.Fatalf("operations = %#v, want %#v", operations, wantOperations)
	}
}

func TestManagerValidatesIdentityCapacityAndCredentials(t *testing.T) {
	factory := newFakeRuntimeFactory()
	manager, err := NewManager(factory, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(context.Background(), validConfig("node-1", "first"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(context.Background(), validConfig("node-1", "duplicate"), false); !errors.Is(err, ErrNodeExists) {
		t.Fatalf("duplicate error = %v", err)
	}
	if _, err := manager.Create(context.Background(), validConfig("node-2", "second"), false); !errors.Is(err, ErrNodeLimit) {
		t.Fatalf("capacity error = %v", err)
	}
	invalid := validConfig("node-1", "invalid")
	invalid.Password = ""
	if _, err := manager.Update(context.Background(), "node-1", invalid, false); err == nil {
		t.Fatal("partial credentials update error = nil")
	}
	invalid = validConfig("different-id", "invalid")
	if _, err := manager.Update(context.Background(), "node-1", invalid, false); err == nil {
		t.Fatal("identity-changing update error = nil")
	}
	if _, err := NewManager(nil, nil, 1); err == nil {
		t.Fatal("NewManager(nil factory) error = nil")
	}
	if _, err := NewManager(factory, nil, 0); err == nil {
		t.Fatal("NewManager(zero max nodes) error = nil")
	}
}

func TestManagerRestoresNodesAndContinuesAfterIndividualStartFailure(t *testing.T) {
	factory := newFakeRuntimeFactory()
	factory.startError["broken"] = errors.New("bind failed")
	manager, _ := NewManager(factory, nil, 4)
	stopped := Node{Config: validConfig("node-1", "stopped"), Status: StatusStopped}
	running := Node{Config: validConfig("node-2", "running"), Status: StatusRunning}
	broken := Node{Config: validConfig("node-3", "broken"), Status: StatusRunning}

	err := manager.Restore(context.Background(), State{Nodes: []Node{broken, stopped, running}})
	if err == nil || !strings.Contains(err.Error(), "node-3") {
		t.Fatalf("Restore() error = %v", err)
	}
	got := manager.List()
	if len(got) != 3 || got[0].Status != StatusStopped || got[1].Status != StatusRunning || got[2].Status != StatusStopped {
		t.Fatalf("restored nodes = %#v", got)
	}
	if operations := factory.operations(); !reflect.DeepEqual(operations, []string{"start:running", "start:broken"}) {
		t.Fatalf("operations = %#v", operations)
	}
}

func TestManagerRestoreRejectsInvalidSnapshotWithoutMutation(t *testing.T) {
	factory := newFakeRuntimeFactory()
	manager, _ := NewManager(factory, nil, 2)
	duplicate := Node{Config: validConfig("node-1", "duplicate"), Status: StatusStopped}
	err := manager.Restore(context.Background(), State{Nodes: []Node{duplicate, duplicate}})
	if err == nil {
		t.Fatal("Restore(duplicate) error = nil")
	}
	if len(manager.List()) != 0 || len(factory.operations()) != 0 {
		t.Fatalf("invalid restore mutated manager: %#v %#v", manager.List(), factory.operations())
	}
}

func TestManagerShutdownAttemptsEveryRunningNode(t *testing.T) {
	factory := newFakeRuntimeFactory()
	manager, _ := NewManager(factory, nil, 3)
	for _, id := range []string{"node-1", "node-2"} {
		if _, err := manager.Create(context.Background(), validConfig(id, id), false); err != nil {
			t.Fatal(err)
		}
	}
	factory.runtimes["node-1"][0].stopError = errors.New("close failed")
	if err := manager.Shutdown(context.Background()); err == nil {
		t.Fatal("Shutdown() error = nil")
	}
	if factory.runtimes["node-1"][0].stopCount() != 1 || factory.runtimes["node-2"][0].stopCount() != 1 {
		t.Fatal("Shutdown() did not attempt every running node")
	}
	if node2, _ := manager.Get("node-2"); node2.Status != StatusStopped {
		t.Fatalf("node-2 status = %q", node2.Status)
	}
}
