package admin

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/network"
)

type memoryResourceStateStore struct {
	state       ipv6resource.State
	exists      bool
	loadErr     error
	saveErr     error
	writableErr error
	saves       int
	operations  *[]string
}

func (s *memoryResourceStateStore) CheckWritable() error {
	return s.writableErr
}

func (s *memoryResourceStateStore) Load() (ipv6resource.State, bool, error) {
	if s.loadErr != nil {
		return ipv6resource.State{}, false, s.loadErr
	}
	return cloneResourceStateForTest(s.state), s.exists, nil
}

func (s *memoryResourceStateStore) Save(state ipv6resource.State) error {
	if s.operations != nil {
		*s.operations = append(*s.operations, "state:save")
	}
	if s.saveErr != nil {
		return s.saveErr
	}
	validated, err := ipv6resource.NewStoreFromState(state)
	if err != nil {
		return err
	}
	s.state, s.exists = validated.State(), true
	s.saves++
	return nil
}

type fakeResourceNetwork struct {
	calls      [][]network.DesiredResource
	failures   []error
	operations *[]string
}

func (n *fakeResourceNetwork) Reconcile(_ context.Context, desired []network.DesiredResource) error {
	copy := cloneDesiredResources(desired)
	n.calls = append(n.calls, copy)
	if n.operations != nil {
		*n.operations = append(*n.operations, "network:reconcile")
	}
	if len(n.failures) == 0 {
		return nil
	}
	err := n.failures[0]
	n.failures = n.failures[1:]
	return err
}

type fakeDrainTerminator struct {
	pool       string
	batch      string
	kind       ipv6resource.PoolKind
	addresses  []netip.Addr
	err        error
	operations *[]string
}

type fakeResourceRuntime struct {
	calls      []ipv6resource.State
	failures   []error
	operations *[]string
}

func (r *fakeResourceRuntime) Sync(state ipv6resource.State) error {
	r.calls = append(r.calls, cloneResourceStateForTest(state))
	if r.operations != nil {
		*r.operations = append(*r.operations, "runtime:sync")
	}
	if len(r.failures) == 0 {
		return nil
	}
	err := r.failures[0]
	r.failures = r.failures[1:]
	return err
}

func (d *fakeDrainTerminator) ForceDrain(_ context.Context, pool, batch string, kind ipv6resource.PoolKind, addresses []netip.Addr) error {
	d.pool, d.batch, d.kind = pool, batch, kind
	d.addresses = append([]netip.Addr(nil), addresses...)
	if d.operations != nil {
		*d.operations = append(*d.operations, "drain:terminate")
	}
	return d.err
}

func TestResourceCoordinatorLoadsAndReconcilesPersistedState(t *testing.T) {
	persisted := resourceStateWithPool(t, false)
	stateStore := &memoryResourceStateStore{state: persisted, exists: true}
	networkManager := &fakeResourceNetwork{}
	service, err := NewResourceCoordinator(stateStore, networkManager, &fakeDrainTerminator{}, bytes.NewReader(bytes.Repeat([]byte{0x44}, 64)))
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(networkManager.calls) != 1 || len(networkManager.calls[0]) != 1 || len(networkManager.calls[0][0].Addresses) != 2 {
		t.Fatalf("startup desired resources = %#v", networkManager.calls)
	}
	snapshot := service.Snapshot()
	if len(snapshot.Pools) != 1 || snapshot.Pools[0].Name != "shared" {
		t.Fatalf("Snapshot() = %#v", snapshot)
	}
	snapshot.Pools[0].Active[0] = netip.MustParseAddr("2001:db8:ffff::1")
	if service.Snapshot().Pools[0].Active[0] == snapshot.Pools[0].Active[0] {
		t.Fatal("mutating resource snapshot changed coordinator state")
	}
}

func TestResourceCoordinatorRejectsProtectedStateBeforeNetworkMutation(t *testing.T) {
	protectedErr := errors.New("resource state is protected")
	stateStore := &memoryResourceStateStore{writableErr: protectedErr}
	networkManager := &fakeResourceNetwork{}
	service, err := NewResourceCoordinator(stateStore, networkManager, &fakeDrainTerminator{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	template, err := ipv6resource.NewPrefixTemplate("wan", "2001:4860:123::/64", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}

	err = service.CreateTemplate(context.Background(), template)
	if !errors.Is(err, protectedErr) {
		t.Fatalf("CreateTemplate() error = %v", err)
	}
	if len(networkManager.calls) != 0 || stateStore.saves != 0 {
		t.Fatalf("protected mutation changed network/state: calls=%d saves=%d", len(networkManager.calls), stateStore.saves)
	}
}

func TestResourceCoordinatorSynchronizesRuntimeDuringStartupAndMutation(t *testing.T) {
	operations := make([]string, 0, 3)
	stateStore := &memoryResourceStateStore{operations: &operations}
	networkManager := &fakeResourceNetwork{operations: &operations}
	runtime := &fakeResourceRuntime{operations: &operations}
	service, err := NewResourceCoordinator(stateStore, networkManager, &fakeDrainTerminator{}, bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetRuntimeSynchronizer(runtime); err != nil {
		t.Fatal(err)
	}
	if err := service.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(operations, []string{"network:reconcile", "runtime:sync"}) {
		t.Fatalf("startup operation order = %v", operations)
	}

	operations = operations[:0]
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:db8:73::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CreateTemplate(context.Background(), template); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(operations, []string{"network:reconcile", "runtime:sync", "state:save"}) {
		t.Fatalf("mutation operation order = %v", operations)
	}
	if len(runtime.calls) != 2 || len(runtime.calls[1].Templates) != 1 {
		t.Fatalf("runtime sync calls = %#v", runtime.calls)
	}
}

func TestResourceCoordinatorRollsBackRuntimeAndNetworkWhenRuntimeSyncFails(t *testing.T) {
	operations := make([]string, 0, 4)
	stateStore := &memoryResourceStateStore{operations: &operations}
	networkManager := &fakeResourceNetwork{operations: &operations}
	runtime := &fakeResourceRuntime{operations: &operations, failures: []error{errors.New("runtime rejected state"), nil}}
	service, err := NewResourceCoordinator(stateStore, networkManager, &fakeDrainTerminator{}, bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetRuntimeSynchronizer(runtime); err != nil {
		t.Fatal(err)
	}
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:db8:74::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.CreateTemplate(context.Background(), template); err == nil {
		t.Fatal("CreateTemplate() error = nil, want runtime sync failure")
	}
	if !slices.Equal(operations, []string{"network:reconcile", "runtime:sync", "runtime:sync", "network:reconcile"}) {
		t.Fatalf("rollback operation order = %v", operations)
	}
	if stateStore.saves != 0 || len(service.Snapshot().Templates) != 0 {
		t.Fatalf("failed runtime sync committed state: saves=%d snapshot=%#v", stateStore.saves, service.Snapshot())
	}
}

func TestResourceCoordinatorRollsBackRuntimeWhenStateSaveFails(t *testing.T) {
	operations := make([]string, 0, 5)
	stateStore := &memoryResourceStateStore{saveErr: errors.New("disk full"), operations: &operations}
	networkManager := &fakeResourceNetwork{operations: &operations}
	runtime := &fakeResourceRuntime{operations: &operations}
	service, err := NewResourceCoordinator(stateStore, networkManager, &fakeDrainTerminator{}, bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetRuntimeSynchronizer(runtime); err != nil {
		t.Fatal(err)
	}
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:db8:75::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.CreateTemplate(context.Background(), template); err == nil {
		t.Fatal("CreateTemplate() error = nil, want state save failure")
	}
	if !slices.Equal(operations, []string{"network:reconcile", "runtime:sync", "state:save", "runtime:sync", "network:reconcile"}) {
		t.Fatalf("rollback operation order = %v", operations)
	}
	if len(service.Snapshot().Templates) != 0 {
		t.Fatalf("failed state save committed live state: %#v", service.Snapshot())
	}
}

func TestResourceCoordinatorRejectsInvalidOrRepeatedRuntimeSynchronizer(t *testing.T) {
	service, err := NewResourceCoordinator(&memoryResourceStateStore{}, &fakeResourceNetwork{}, &fakeDrainTerminator{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetRuntimeSynchronizer(nil); err == nil {
		t.Fatal("SetRuntimeSynchronizer(nil) error = nil")
	}
	if err := service.SetRuntimeSynchronizer(&fakeResourceRuntime{}); err != nil {
		t.Fatal(err)
	}
	if err := service.SetRuntimeSynchronizer(&fakeResourceRuntime{}); err == nil {
		t.Fatal("second SetRuntimeSynchronizer() error = nil")
	}
}

func TestResourceCoordinatorCommitsNaturallyDrainedAddress(t *testing.T) {
	persisted := resourceStateWithPool(t, true)
	stateStore := &memoryResourceStateStore{state: persisted, exists: true}
	networkManager := &fakeResourceNetwork{}
	runtime := &fakeResourceRuntime{}
	service, err := NewResourceCoordinator(stateStore, networkManager, &fakeDrainTerminator{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetRuntimeSynchronizer(runtime); err != nil {
		t.Fatal(err)
	}
	address := persisted.Pools[0].Draining[0].Addresses[0]

	if err := service.CompleteDrainedAddress(context.Background(), "shared", address); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if stateStore.saves != 1 || len(networkManager.calls) != 1 || len(runtime.calls) != 1 {
		t.Fatalf("natural drain transaction calls: saves=%d network=%d runtime=%d", stateStore.saves, len(networkManager.calls), len(runtime.calls))
	}
	for _, canonical := range snapshot.Addresses {
		if canonical.Address == address {
			t.Fatalf("naturally drained address %s remained", address)
		}
	}
	if len(snapshot.Pools[0].Draining) != 1 || len(snapshot.Pools[0].Draining[0].Addresses) != 1 {
		t.Fatalf("remaining drain = %#v", snapshot.Pools[0].Draining)
	}
}

func TestResourceCoordinatorStateReturnsCommittedIsolatedSnapshot(t *testing.T) {
	state := resourceStateWithPool(t, true)
	states := &memoryResourceStateStore{state: state, exists: true}
	coordinator, err := NewResourceCoordinator(states, &fakeResourceNetwork{}, &fakeDrainTerminator{}, nil)
	if err != nil {
		t.Fatalf("NewResourceCoordinator() error = %v", err)
	}

	snapshot := coordinator.State()
	if len(snapshot.Pools) == 0 || snapshot.Pools[0].Name == "" {
		t.Fatalf("State() = %#v, want committed resource state", snapshot)
	}
	snapshot.Pools[0].Name = "mutated"
	snapshot.Pools[0].Active = nil

	again := coordinator.State()
	if again.Pools[0].Name == "mutated" || len(again.Pools[0].Active) == 0 {
		t.Fatalf("State() allowed caller mutation: %#v", again.Pools[0])
	}
}

func TestResourceCoordinatorConfiguresNetworkBeforePersistingAndCommitting(t *testing.T) {
	operations := make([]string, 0, 2)
	stateStore := &memoryResourceStateStore{operations: &operations}
	networkManager := &fakeResourceNetwork{operations: &operations}
	service, err := NewResourceCoordinator(stateStore, networkManager, &fakeDrainTerminator{}, bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:db8:70::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CreateTemplate(context.Background(), template); err != nil {
		t.Fatal(err)
	}
	operations = operations[:0]

	fixed, err := service.CreateFixedAddress(context.Background(), "auto", "edge", nil)
	if err != nil {
		t.Fatal(err)
	}
	if fixed.Address.String() != "2001:db8:70::42" {
		t.Fatalf("auto address = %s, want 2001:db8:70::42", fixed.Address)
	}
	if !slices.Equal(operations, []string{"network:reconcile", "state:save"}) {
		t.Fatalf("operation order = %v", operations)
	}
	if got := service.Snapshot().Fixed; len(got) != 1 || got[0].Name != "auto" {
		t.Fatalf("committed fixed addresses = %#v", got)
	}
}

func TestResourceCoordinatorRollsBackNetworkFailureWithoutCommittingState(t *testing.T) {
	stateStore := &memoryResourceStateStore{}
	networkManager := &fakeResourceNetwork{failures: []error{errors.New("DAD failed"), nil}}
	service, err := NewResourceCoordinator(stateStore, networkManager, &fakeDrainTerminator{}, bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:db8:71::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.CreateTemplate(context.Background(), template); err == nil {
		t.Fatal("CreateTemplate() error = nil, want network failure")
	}
	if len(networkManager.calls) != 2 || len(networkManager.calls[1]) != 0 {
		t.Fatalf("network rollback calls = %#v", networkManager.calls)
	}
	if stateStore.saves != 0 || len(service.Snapshot().Templates) != 0 {
		t.Fatalf("failed transaction committed state: saves=%d snapshot=%#v", stateStore.saves, service.Snapshot())
	}
}

func TestResourceCoordinatorRollsBackNetworkWhenStateSaveFails(t *testing.T) {
	stateStore := &memoryResourceStateStore{saveErr: errors.New("disk full")}
	networkManager := &fakeResourceNetwork{}
	service, err := NewResourceCoordinator(stateStore, networkManager, &fakeDrainTerminator{}, bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:db8:72::/120", "eth0", ipv6resource.ModeExternal)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.CreateTemplate(context.Background(), template); err == nil {
		t.Fatal("CreateTemplate() error = nil, want state save failure")
	}
	if len(networkManager.calls) != 2 || len(networkManager.calls[1]) != 0 {
		t.Fatalf("network rollback calls = %#v", networkManager.calls)
	}
	if len(service.Snapshot().Templates) != 0 {
		t.Fatalf("failed save changed live state: %#v", service.Snapshot())
	}
}

func TestResourceCoordinatorForceDrainTerminatesConnectionsBeforeRemovingAddress(t *testing.T) {
	operations := make([]string, 0, 3)
	persisted := resourceStateWithPool(t, true)
	stateStore := &memoryResourceStateStore{state: persisted, exists: true, operations: &operations}
	networkManager := &fakeResourceNetwork{operations: &operations}
	drains := &fakeDrainTerminator{operations: &operations}
	service, err := NewResourceCoordinator(stateStore, networkManager, drains, bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	pool := service.Snapshot().Pools[0]
	batch := pool.Draining[0]
	operations = operations[:0]

	if err := service.ForceDrain(context.Background(), pool.Name, batch.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(operations, []string{"drain:terminate", "network:reconcile", "state:save"}) {
		t.Fatalf("force drain operation order = %v", operations)
	}
	if drains.pool != pool.Name || drains.batch != batch.ID {
		t.Fatalf("terminated drain = %s/%s", drains.pool, drains.batch)
	}
	if drains.kind != pool.Kind || !slices.Equal(drains.addresses, batch.Addresses) {
		t.Fatalf("terminated resources = %s/%v, want %s/%v", drains.kind, drains.addresses, pool.Kind, batch.Addresses)
	}
	after := service.Snapshot()
	if len(after.Pools[0].Draining) != 0 {
		t.Fatalf("draining batch remains: %#v", after.Pools[0].Draining)
	}
	for _, address := range batch.Addresses {
		for _, canonical := range after.Addresses {
			if canonical.Address == address {
				t.Fatalf("drained address %s remains canonical", address)
			}
		}
	}
}

func TestResourceCoordinatorIgnoresCompletionForAlreadyFinishedDrain(t *testing.T) {
	persisted := resourceStateWithPool(t, true)
	service, err := NewResourceCoordinator(
		&memoryResourceStateStore{state: persisted, exists: true},
		&fakeResourceNetwork{}, &fakeDrainTerminator{}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	pool := persisted.Pools[0]
	address := pool.Draining[0].Addresses[0]
	if err := service.ForceDrain(context.Background(), pool.Name, pool.Draining[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := service.CompleteDrainedAddress(context.Background(), pool.Name, address); err != nil {
		t.Fatalf("late CompleteDrainedAddress() = %v", err)
	}
}

func TestResourceCoordinatorForceDrainFailurePreservesState(t *testing.T) {
	persisted := resourceStateWithPool(t, true)
	stateStore := &memoryResourceStateStore{state: persisted, exists: true}
	networkManager := &fakeResourceNetwork{}
	drains := &fakeDrainTerminator{err: errors.New("connections remain")}
	service, err := NewResourceCoordinator(stateStore, networkManager, drains, bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	pool := service.Snapshot().Pools[0]

	if err := service.ForceDrain(context.Background(), pool.Name, pool.Draining[0].ID); err == nil {
		t.Fatal("ForceDrain() error = nil, want termination failure")
	}
	if len(networkManager.calls) != 0 || stateStore.saves != 0 || len(service.Snapshot().Pools[0].Draining) != 1 {
		t.Fatalf("failed force drain mutated state: calls=%d saves=%d snapshot=%#v", len(networkManager.calls), stateStore.saves, service.Snapshot())
	}
}

func resourceStateWithPool(t *testing.T, refresh bool) ipv6resource.State {
	t.Helper()
	store := ipv6resource.NewStore()
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:db8:80::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddTemplate(template); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool("shared", ipv6resource.PoolSharedOutbound, "edge", 2, nil); err != nil {
		t.Fatal(err)
	}
	if refresh {
		if _, err := store.RefreshPool("shared"); err != nil {
			t.Fatal(err)
		}
	}
	return store.State()
}

func cloneResourceStateForTest(state ipv6resource.State) ipv6resource.State {
	store, err := ipv6resource.NewStoreFromState(state)
	if err != nil {
		panic(err)
	}
	return store.State()
}

func cloneDesiredResources(resources []network.DesiredResource) []network.DesiredResource {
	result := make([]network.DesiredResource, len(resources))
	for i, resource := range resources {
		result[i] = network.DesiredResource{Template: resource.Template, Addresses: append([]netip.Addr(nil), resource.Addresses...)}
	}
	return result
}
