package network

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
)

type fakeKernel struct {
	mu                sync.Mutex
	addresses         map[AddressRef]bool
	routes            map[RouteRef]bool
	dadErrors         map[AddressRef]error
	waitForCancel     map[AddressRef]bool
	bindErrors        map[AddressRef]error
	removeAddrError   map[AddressRef]error
	removeRouteErr    map[RouteRef]error
	addAddresses      []AddressRef
	removeAddresses   []AddressRef
	addRoutes         []RouteRef
	removeRoutes      []RouteRef
	waitStarted       chan AddressRef
	waitGate          <-chan struct{}
	operations        *[]string
	existsCalls       int
	interfaceAddrs    map[string][]netip.Addr
	interfaceAddrErr  error
	waitBatchErr      error
	interfaceAddrCalls int
	waitReadyCalls    int
	waitBatchCalls    int
	waitBatchRefs     []AddressRef
}

func (f *fakeKernel) callCounts() (exists, interfaceAddrs, waitReady, waitBatch int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.existsCalls, f.interfaceAddrCalls, f.waitReadyCalls, f.waitBatchCalls
}

func (f *fakeKernel) batchWaitRefs() []AddressRef {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]AddressRef(nil), f.waitBatchRefs...)
}

func (f *fakeKernel) AddressExists(_ context.Context, ref AddressRef) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.existsCalls++
	return f.addresses[ref], nil
}

func (f *fakeKernel) InterfaceAddresses(_ context.Context, iface string) ([]netip.Addr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interfaceAddrCalls++
	if f.interfaceAddrErr != nil {
		return nil, f.interfaceAddrErr
	}
	if f.interfaceAddrs != nil {
		return f.interfaceAddrs[iface], nil
	}
	// Derive from the addresses map so both query styles stay consistent.
	var result []netip.Addr
	for ref, present := range f.addresses {
		if present && ref.Interface == iface {
			result = append(result, ref.Address)
		}
	}
	return result, nil
}

func (f *fakeKernel) WaitAddressesReady(ctx context.Context, refs []AddressRef) error {
	f.mu.Lock()
	f.waitBatchCalls++
	f.waitBatchRefs = append(f.waitBatchRefs, refs...)
	f.mu.Unlock()
	for _, ref := range refs {
		if f.waitStarted != nil {
			f.waitStarted <- ref
		}
	}
	f.mu.Lock()
	for _, ref := range refs {
		if err := f.dadErrors[ref]; err != nil {
			failures := []error{fmt.Errorf("wait for address %s DAD: %w", ref.Address, err)}
			for _, other := range refs {
				if other == ref {
					continue
				}
				failures = append(failures, fmt.Errorf("wait for address %s DAD: %w", other.Address, context.Canceled))
			}
			joined := errors.Join(failures...)
			f.mu.Unlock()
			return joined
		}
	}
	waitForCancel := false
	for _, ref := range refs {
		if f.waitForCancel[ref] {
			waitForCancel = true
			break
		}
	}
	gate := f.waitGate
	f.mu.Unlock()
	if waitForCancel {
		<-ctx.Done()
		return ctx.Err()
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (f *fakeKernel) AddAddress(_ context.Context, ref AddressRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.operations != nil {
		*f.operations = append(*f.operations, "kernel:add-address")
	}
	f.addresses[ref] = true
	f.addAddresses = append(f.addAddresses, ref)
	return nil
}

func (f *fakeKernel) RemoveAddress(_ context.Context, ref AddressRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.removeAddrError[ref]; err != nil {
		return err
	}
	delete(f.addresses, ref)
	f.removeAddresses = append(f.removeAddresses, ref)
	return nil
}

func (f *fakeKernel) WaitAddressReady(ctx context.Context, ref AddressRef) error {
	if f.waitStarted != nil {
		f.waitStarted <- ref
	}
	f.mu.Lock()
	waitForCancel := f.waitForCancel[ref]
	f.mu.Unlock()
	if waitForCancel {
		<-ctx.Done()
		return ctx.Err()
	}
	if f.waitGate != nil {
		select {
		case <-f.waitGate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.waitReadyCalls++
	return f.dadErrors[ref]
}

func (f *fakeKernel) LocalRouteExists(_ context.Context, ref RouteRef) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.routes[ref], nil
}

func (f *fakeKernel) AddLocalRoute(_ context.Context, ref RouteRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.operations != nil {
		*f.operations = append(*f.operations, "kernel:add-route")
	}
	f.routes[ref] = true
	f.addRoutes = append(f.addRoutes, ref)
	return nil
}

func (f *fakeKernel) RemoveLocalRoute(_ context.Context, ref RouteRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.removeRouteErr[ref]; err != nil {
		return err
	}
	delete(f.routes, ref)
	f.removeRoutes = append(f.removeRoutes, ref)
	return nil
}

func (f *fakeKernel) ValidateBindable(_ context.Context, ref AddressRef, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bindErrors[ref]
}

type memoryOwnershipStore struct {
	mu         sync.Mutex
	state      Ownership
	saves      int
	saveErr    error
	operations *[]string
}

func (s *memoryOwnershipStore) Load() (Ownership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneOwnership(s.state), nil
}

func (s *memoryOwnershipStore) Save(state Ownership) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.state = cloneOwnership(state)
	s.saves++
	if s.operations != nil {
		*s.operations = append(*s.operations, "store:save")
	}
	return nil
}

func TestAddressModeDADFailureRollsBackOnlyAddressesAddedByOperation(t *testing.T) {
	template := mustTemplate(t, ipv6resource.ModeAddress)
	a := addressRef(template, "2001:4860:1::1")
	b := addressRef(template, "2001:4860:1::2")
	c := addressRef(template, "2001:4860:1::3")
	kernel := &fakeKernel{
		addresses: map[AddressRef]bool{a: true}, routes: make(map[RouteRef]bool),
		dadErrors: map[AddressRef]error{c: errors.New("dadfailed")}, bindErrors: make(map[AddressRef]error),
	}
	store := &memoryOwnershipStore{state: Ownership{Addresses: []AddressRef{a}}}
	manager, err := NewResourceManager(kernel, store, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	err = manager.Apply(context.Background(), template, []netip.Addr{a.Address, b.Address, c.Address})
	if err == nil {
		t.Fatal("Apply() error = nil, want DAD failure")
	}
	if len(kernel.removeAddresses) != 2 || kernel.removeAddresses[0] != c || kernel.removeAddresses[1] != b {
		t.Fatalf("removed addresses = %#v, want newly added addresses in reverse order", kernel.removeAddresses)
	}
	if !kernel.addresses[a] || kernel.addresses[b] || kernel.addresses[c] {
		t.Fatalf("kernel addresses after rollback = %#v", kernel.addresses)
	}
	if len(store.state.Addresses) != 1 || store.state.Addresses[0] != a {
		t.Fatalf("ownership after rollback = %#v", store.state)
	}
}

func TestApplyPersistsOwnershipIntentBeforeKernelMutation(t *testing.T) {
	for _, mode := range []ipv6resource.ConfigMode{ipv6resource.ModeAddress, ipv6resource.ModeLocalRouteFreebind} {
		t.Run(string(mode), func(t *testing.T) {
			operations := make([]string, 0, 2)
			template := mustTemplate(t, mode)
			ref := addressRef(template, "2001:4860:1::1")
			kernel := &fakeKernel{
				addresses: make(map[AddressRef]bool), routes: make(map[RouteRef]bool),
				dadErrors: make(map[AddressRef]error), bindErrors: make(map[AddressRef]error),
				operations: &operations,
			}
			manager, err := NewResourceManager(kernel, &memoryOwnershipStore{operations: &operations}, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.Apply(context.Background(), template, []netip.Addr{ref.Address}); err != nil {
				t.Fatal(err)
			}
			if len(operations) < 2 || operations[0] != "store:save" {
				t.Fatalf("operation order = %v, ownership intent must be first", operations)
			}
		})
	}
}

func TestApplyDoesNotMutateKernelWhenOwnershipIntentCannotBeSaved(t *testing.T) {
	for _, mode := range []ipv6resource.ConfigMode{ipv6resource.ModeAddress, ipv6resource.ModeLocalRouteFreebind} {
		t.Run(string(mode), func(t *testing.T) {
			template := mustTemplate(t, mode)
			ref := addressRef(template, "2001:4860:1::1")
			kernel := &fakeKernel{
				addresses: make(map[AddressRef]bool), routes: make(map[RouteRef]bool),
				dadErrors: make(map[AddressRef]error), bindErrors: make(map[AddressRef]error),
			}
			store := &memoryOwnershipStore{saveErr: errors.New("disk full")}
			manager, err := NewResourceManager(kernel, store, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.Apply(context.Background(), template, []netip.Addr{ref.Address}); err == nil {
				t.Fatal("Apply() error = nil, want ownership save failure")
			}
			if len(kernel.addAddresses)+len(kernel.addRoutes) != 0 {
				t.Fatalf("kernel mutated before ownership intent was durable: addresses=%v routes=%v", kernel.addAddresses, kernel.addRoutes)
			}
		})
	}
}

func TestAddressRollbackRetainsOwnershipWhenKernelCleanupFails(t *testing.T) {
	template := mustTemplate(t, ipv6resource.ModeAddress)
	ref := addressRef(template, "2001:4860:1::1")
	kernel := &fakeKernel{
		addresses: make(map[AddressRef]bool), routes: make(map[RouteRef]bool),
		dadErrors:       make(map[AddressRef]error),
		bindErrors:      map[AddressRef]error{ref: errors.New("bind failed")},
		removeAddrError: map[AddressRef]error{ref: errors.New("delete failed")},
	}
	store := &memoryOwnershipStore{}
	manager, err := NewResourceManager(kernel, store, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Apply(context.Background(), template, []netip.Addr{ref.Address}); err == nil {
		t.Fatal("Apply() error = nil, want bind and rollback failure")
	}
	if !kernel.addresses[ref] {
		t.Fatal("failed cleanup unexpectedly removed kernel address")
	}
	if len(store.state.Addresses) != 1 || store.state.Addresses[0] != ref {
		t.Fatalf("ownership after failed cleanup = %#v, want address retained", store.state)
	}
}

func TestLocalRouteRollbackRetainsOwnershipWhenKernelCleanupFails(t *testing.T) {
	template := mustTemplate(t, ipv6resource.ModeLocalRouteFreebind)
	ref := addressRef(template, "2001:4860:1::1")
	route := RouteRef{Interface: template.Interface, Prefix: template.Prefix}
	kernel := &fakeKernel{
		addresses: make(map[AddressRef]bool), routes: make(map[RouteRef]bool),
		dadErrors:      make(map[AddressRef]error),
		bindErrors:     map[AddressRef]error{ref: errors.New("bind failed")},
		removeRouteErr: map[RouteRef]error{route: errors.New("delete failed")},
	}
	store := &memoryOwnershipStore{}
	manager, err := NewResourceManager(kernel, store, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Apply(context.Background(), template, []netip.Addr{ref.Address}); err == nil {
		t.Fatal("Apply() error = nil, want bind and rollback failure")
	}
	if !kernel.routes[route] {
		t.Fatal("failed cleanup unexpectedly removed kernel route")
	}
	if len(store.state.Routes) != 1 || store.state.Routes[0] != route {
		t.Fatalf("ownership after failed cleanup = %#v, want route retained", store.state)
	}
}

func TestAddressModeRejectsPreexistingUnownedAddressWithoutMutation(t *testing.T) {
	template := mustTemplate(t, ipv6resource.ModeAddress)
	ref := addressRef(template, "2001:4860:1::1")
	kernel := &fakeKernel{
		addresses: map[AddressRef]bool{ref: true}, routes: make(map[RouteRef]bool),
		dadErrors: make(map[AddressRef]error), bindErrors: make(map[AddressRef]error),
	}
	manager, err := NewResourceManager(kernel, &memoryOwnershipStore{}, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Apply(context.Background(), template, []netip.Addr{ref.Address}); !errors.Is(err, ErrResourceConflict) {
		t.Fatalf("Apply() error = %v, want ErrResourceConflict", err)
	}
	if len(kernel.addAddresses) != 0 || len(kernel.removeAddresses) != 0 {
		t.Fatalf("kernel was mutated: add=%v remove=%v", kernel.addAddresses, kernel.removeAddresses)
	}
}

func TestAddressModeWaitsForDADInParallel(t *testing.T) {
	template := mustTemplate(t, ipv6resource.ModeAddress)
	a := addressRef(template, "2001:4860:1::1")
	b := addressRef(template, "2001:4860:1::2")
	gate := make(chan struct{})
	started := make(chan AddressRef, 2)
	kernel := &fakeKernel{
		addresses: make(map[AddressRef]bool), routes: make(map[RouteRef]bool),
		dadErrors: make(map[AddressRef]error), bindErrors: make(map[AddressRef]error),
		waitStarted: started, waitGate: gate,
	}
	manager, err := NewResourceManager(kernel, &memoryOwnershipStore{}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- manager.Apply(context.Background(), template, []netip.Addr{a.Address, b.Address}) }()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("DAD checks did not start in parallel")
		}
	}
	close(gate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAddressModeCancelsOtherDADWaitsAfterFirstFailure(t *testing.T) {
	template := mustTemplate(t, ipv6resource.ModeAddress)
	a := addressRef(template, "2001:4860:1::1")
	b := addressRef(template, "2001:4860:1::2")
	kernel := &fakeKernel{
		addresses: make(map[AddressRef]bool), routes: make(map[RouteRef]bool),
		dadErrors:     map[AddressRef]error{a: errors.New("dadfailed")},
		waitForCancel: map[AddressRef]bool{b: true}, bindErrors: make(map[AddressRef]error),
	}
	manager, err := NewResourceManager(kernel, &memoryOwnershipStore{}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- manager.Apply(context.Background(), template, []netip.Addr{a.Address, b.Address}) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Apply() error = nil, want DAD failure")
		}
	case <-time.After(time.Second):
		t.Fatal("Apply() did not cancel remaining DAD wait after failure")
	}
}

func TestLocalRouteModeRollsBackOwnedRouteWhenBindValidationFails(t *testing.T) {
	template := mustTemplate(t, ipv6resource.ModeLocalRouteFreebind)
	ref := addressRef(template, "2001:4860:1::1")
	route := RouteRef{Interface: template.Interface, Prefix: template.Prefix}
	kernel := &fakeKernel{
		addresses: make(map[AddressRef]bool), routes: make(map[RouteRef]bool),
		dadErrors: make(map[AddressRef]error), bindErrors: map[AddressRef]error{ref: errors.New("bind failed")},
	}
	store := &memoryOwnershipStore{}
	manager, err := NewResourceManager(kernel, store, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Apply(context.Background(), template, []netip.Addr{ref.Address}); err == nil {
		t.Fatal("Apply() error = nil, want bind validation failure")
	}
	if len(kernel.addRoutes) != 1 || len(kernel.removeRoutes) != 1 || kernel.removeRoutes[0] != route {
		t.Fatalf("route operations add=%v remove=%v", kernel.addRoutes, kernel.removeRoutes)
	}
	if len(store.state.Routes) != 0 {
		t.Fatalf("route ownership remains after rollback: %#v", store.state)
	}
}

func TestExternalModeOnlyValidatesAndNeverMutatesKernel(t *testing.T) {
	template := mustTemplate(t, ipv6resource.ModeExternal)
	ref := addressRef(template, "2001:4860:1::1")
	kernel := &fakeKernel{
		addresses: make(map[AddressRef]bool), routes: make(map[RouteRef]bool),
		dadErrors: make(map[AddressRef]error), bindErrors: map[AddressRef]error{ref: errors.New("not configured")},
	}
	store := &memoryOwnershipStore{}
	manager, err := NewResourceManager(kernel, store, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Apply(context.Background(), template, []netip.Addr{ref.Address}); err == nil {
		t.Fatal("Apply() error = nil, want validation failure")
	}
	if len(kernel.addAddresses)+len(kernel.removeAddresses)+len(kernel.addRoutes)+len(kernel.removeRoutes) != 0 || store.saves != 0 {
		t.Fatalf("external mode mutated state: kernel=%#v saves=%d", kernel, store.saves)
	}
}

func TestReconcileRemovesStaleOwnedResourcesAndRepairsDesiredAddress(t *testing.T) {
	addressTemplate := mustTemplate(t, ipv6resource.ModeAddress)
	routeTemplate := mustTemplate(t, ipv6resource.ModeLocalRouteFreebind)
	desired := addressRef(addressTemplate, "2001:4860:1::1")
	stale := addressRef(addressTemplate, "2001:4860:1::2")
	staleRoute := RouteRef{Interface: routeTemplate.Interface, Prefix: routeTemplate.Prefix}
	kernel := &fakeKernel{
		addresses: map[AddressRef]bool{stale: true}, routes: map[RouteRef]bool{staleRoute: true},
		dadErrors: make(map[AddressRef]error), bindErrors: make(map[AddressRef]error),
	}
	store := &memoryOwnershipStore{state: Ownership{Addresses: []AddressRef{stale}, Routes: []RouteRef{staleRoute}}}
	manager, err := NewResourceManager(kernel, store, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	err = manager.Reconcile(context.Background(), []DesiredResource{{Template: addressTemplate, Addresses: []netip.Addr{desired.Address}}})
	if err != nil {
		t.Fatal(err)
	}
	if kernel.addresses[stale] || !kernel.addresses[desired] || kernel.routes[staleRoute] {
		t.Fatalf("kernel state after reconcile: addresses=%v routes=%v", kernel.addresses, kernel.routes)
	}
	if len(store.state.Addresses) != 1 || store.state.Addresses[0] != desired || len(store.state.Routes) != 0 {
		t.Fatalf("ownership after reconcile = %#v", store.state)
	}
}

func TestReleaseAndShutdownNeverRemoveUnownedResources(t *testing.T) {
	template := mustTemplate(t, ipv6resource.ModeAddress)
	ownedA := addressRef(template, "2001:4860:1::1")
	ownedB := addressRef(template, "2001:4860:1::2")
	unowned := addressRef(template, "2001:4860:1::3")
	kernel := &fakeKernel{
		addresses: map[AddressRef]bool{ownedA: true, ownedB: true, unowned: true}, routes: make(map[RouteRef]bool),
		dadErrors: make(map[AddressRef]error), bindErrors: make(map[AddressRef]error),
	}
	store := &memoryOwnershipStore{state: Ownership{Addresses: []AddressRef{ownedA, ownedB}}}
	manager, err := NewResourceManager(kernel, store, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Release(context.Background(), template, []netip.Addr{ownedA.Address}); err != nil {
		t.Fatal(err)
	}
	if kernel.addresses[ownedA] || !kernel.addresses[ownedB] || !kernel.addresses[unowned] {
		t.Fatalf("addresses after release = %v", kernel.addresses)
	}
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if kernel.addresses[ownedA] || kernel.addresses[ownedB] || !kernel.addresses[unowned] {
		t.Fatalf("addresses after shutdown = %v", kernel.addresses)
	}
	if len(store.state.Addresses) != 0 || len(store.state.Routes) != 0 {
		t.Fatalf("ownership after shutdown = %#v", store.state)
	}
}

func TestReconcileBatchesOwnershipSavesWhenRemovingStaleAddresses(t *testing.T) {
	template := mustTemplate(t, ipv6resource.ModeAddress)
	refs := []AddressRef{
		addressRef(template, "2001:4860:1::1"),
		addressRef(template, "2001:4860:1::2"),
		addressRef(template, "2001:4860:1::3"),
		addressRef(template, "2001:4860:1::4"),
		addressRef(template, "2001:4860:1::5"),
	}
	kernel := &fakeKernel{
		addresses: make(map[AddressRef]bool), routes: make(map[RouteRef]bool),
		dadErrors: make(map[AddressRef]error), bindErrors: make(map[AddressRef]error),
	}
	owned := make([]AddressRef, 0, len(refs))
	for _, ref := range refs {
		kernel.addresses[ref] = true
		owned = append(owned, ref)
	}
	store := &memoryOwnershipStore{state: Ownership{Addresses: owned}}
	manager, err := NewResourceManager(kernel, store, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Reconcile(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(kernel.removeAddresses) != len(refs) {
		t.Fatalf("removed addresses = %v, want all stale addresses", kernel.removeAddresses)
	}
	if len(store.state.Addresses) != 0 {
		t.Fatalf("ownership after reconcile = %#v, want empty", store.state)
	}
	if store.saves > 2 {
		t.Fatalf("ownership saves = %d, want batched persistence (<= 2) instead of one save per address", store.saves)
	}
}

func TestReleaseBatchesOwnershipSaves(t *testing.T) {
	template := mustTemplate(t, ipv6resource.ModeAddress)
	refs := []AddressRef{
		addressRef(template, "2001:4860:1::1"),
		addressRef(template, "2001:4860:1::2"),
		addressRef(template, "2001:4860:1::3"),
	}
	kernel := &fakeKernel{
		addresses: make(map[AddressRef]bool), routes: make(map[RouteRef]bool),
		dadErrors: make(map[AddressRef]error), bindErrors: make(map[AddressRef]error),
	}
	owned := make([]AddressRef, 0, len(refs))
	addresses := make([]netip.Addr, 0, len(refs))
	for _, ref := range refs {
		kernel.addresses[ref] = true
		owned = append(owned, ref)
		addresses = append(addresses, ref.Address)
	}
	store := &memoryOwnershipStore{state: Ownership{Addresses: owned}}
	manager, err := NewResourceManager(kernel, store, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Release(context.Background(), template, addresses); err != nil {
		t.Fatal(err)
	}
	if len(store.state.Addresses) != 0 {
		t.Fatalf("ownership after release = %#v, want empty", store.state)
	}
	if store.saves > 1 {
		t.Fatalf("ownership saves = %d, want a single batched save", store.saves)
	}
}

func TestReconcilePersistsSuccessfulRemovalsWhenARemovalFails(t *testing.T) {
	template := mustTemplate(t, ipv6resource.ModeAddress)
	first := addressRef(template, "2001:4860:1::1")
	failing := addressRef(template, "2001:4860:1::2")
	last := addressRef(template, "2001:4860:1::3")
	kernel := &fakeKernel{
		addresses: map[AddressRef]bool{first: true, failing: true, last: true},
		routes:    make(map[RouteRef]bool),
		dadErrors: make(map[AddressRef]error), bindErrors: make(map[AddressRef]error),
		removeAddrError: map[AddressRef]error{failing: errors.New("netlink busy")},
	}
	store := &memoryOwnershipStore{state: Ownership{Addresses: []AddressRef{first, failing, last}}}
	manager, err := NewResourceManager(kernel, store, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Reconcile(context.Background(), nil); err == nil {
		t.Fatal("Reconcile() error = nil, want removal failure")
	}
	if len(store.state.Addresses) != 1 || store.state.Addresses[0] != failing {
		t.Fatalf("ownership after partial failure = %#v, want only the failed address retained", store.state)
	}
	if store.saves < 1 {
		t.Fatal("ownership saves = 0, want successful removals persisted")
	}
}

func TestApplyAddressesUsesBatchedKernelQueries(t *testing.T) {
	template := mustTemplate(t, ipv6resource.ModeAddress)
	a := addressRef(template, "2001:4860:1::1")
	b := addressRef(template, "2001:4860:1::2")
	c := addressRef(template, "2001:4860:1::3")
	kernel := &fakeKernel{
		addresses:      make(map[AddressRef]bool),
		routes:         make(map[RouteRef]bool),
		bindErrors:     make(map[AddressRef]error),
		interfaceAddrs: map[string][]netip.Addr{template.Interface: {}},
	}
	store := &memoryOwnershipStore{}
	manager, err := NewResourceManager(kernel, store, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Apply(context.Background(), template, []netip.Addr{a.Address, b.Address, c.Address}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	exists, interfaceAddrs, waitReady, waitBatch := kernel.callCounts()
	if exists != 0 {
		t.Errorf("AddressExists calls = %d, want 0 (batched via InterfaceAddresses)", exists)
	}
	if interfaceAddrs != 1 {
		t.Errorf("InterfaceAddresses calls = %d, want 1 per interface", interfaceAddrs)
	}
	if waitReady != 0 {
		t.Errorf("WaitAddressReady calls = %d, want 0 (batched via WaitAddressesReady)", waitReady)
	}
	if waitBatch != 1 {
		t.Errorf("WaitAddressesReady calls = %d, want 1", waitBatch)
	}
	if refs := kernel.batchWaitRefs(); len(refs) != 3 {
		t.Errorf("WaitAddressesReady refs = %v, want 3 added addresses", refs)
	}
}

func TestReconcileStaleRemovalsUseBatchedKernelQueries(t *testing.T) {
	template := mustTemplate(t, ipv6resource.ModeAddress)
	stale1 := addressRef(template, "2001:4860:1::1")
	stale2 := addressRef(template, "2001:4860:1::2")
	keep := addressRef(template, "2001:4860:1::3")
	kernel := &fakeKernel{
		addresses:      map[AddressRef]bool{stale1: true, stale2: true},
		routes:         make(map[RouteRef]bool),
		bindErrors:     make(map[AddressRef]error),
		interfaceAddrs: map[string][]netip.Addr{template.Interface: {stale1.Address, stale2.Address}},
	}
	store := &memoryOwnershipStore{state: Ownership{Addresses: []AddressRef{stale1, stale2, keep}}}
	manager, err := NewResourceManager(kernel, store, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	desired := []DesiredResource{{Template: template, Addresses: []netip.Addr{keep.Address}}}
	if err := manager.Reconcile(context.Background(), desired); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	exists, interfaceAddrs, _, _ := kernel.callCounts()
	if exists != 0 {
		t.Errorf("AddressExists calls = %d, want 0 (batched via InterfaceAddresses)", exists)
	}
	// Reconcile batches twice on this path: once for stale removals and once
	// for the apply pre-check of the desired address (one dump per interface
	// per phase instead of one dump per address).
	if interfaceAddrs != 2 {
		t.Errorf("InterfaceAddresses calls = %d, want 2 (one per Reconcile phase)", interfaceAddrs)
	}
}

func mustTemplate(t *testing.T, mode ipv6resource.ConfigMode) ipv6resource.PrefixTemplate {
	t.Helper()
	template, err := ipv6resource.NewPrefixTemplate("wan", "2001:4860:1::/64", "eth0", mode)
	if err != nil {
		t.Fatal(err)
	}
	return template
}

func addressRef(template ipv6resource.PrefixTemplate, address string) AddressRef {
	return AddressRef{Interface: template.Interface, Address: netip.MustParseAddr(address)}
}
