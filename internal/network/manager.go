package network

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
)

var ErrResourceConflict = errors.New("network resource conflicts with an unowned resource")

type AddressRef struct {
	Interface string     `json:"interface" yaml:"interface"`
	Address   netip.Addr `json:"address" yaml:"address"`
}

type RouteRef struct {
	Interface string       `json:"interface" yaml:"interface"`
	Prefix    netip.Prefix `json:"prefix" yaml:"prefix"`
}

type Ownership struct {
	Addresses []AddressRef `json:"addresses" yaml:"addresses"`
	Routes    []RouteRef   `json:"routes" yaml:"routes"`
}

type Kernel interface {
	AddressExists(context.Context, AddressRef) (bool, error)
	AddAddress(context.Context, AddressRef) error
	RemoveAddress(context.Context, AddressRef) error
	WaitAddressReady(context.Context, AddressRef) error
	LocalRouteExists(context.Context, RouteRef) (bool, error)
	AddLocalRoute(context.Context, RouteRef) error
	RemoveLocalRoute(context.Context, RouteRef) error
	ValidateBindable(context.Context, AddressRef, bool) error
}

type OwnershipStore interface {
	Load() (Ownership, error)
	Save(Ownership) error
}

type ResourceManager struct {
	kernel     Kernel
	store      OwnershipStore
	dadTimeout time.Duration
	mu         sync.Mutex
}

type DesiredResource struct {
	Template  ipv6resource.PrefixTemplate
	Addresses []netip.Addr
}

func NewResourceManager(kernel Kernel, store OwnershipStore, dadTimeout time.Duration) (*ResourceManager, error) {
	if kernel == nil {
		return nil, errors.New("kernel is required")
	}
	if store == nil {
		return nil, errors.New("ownership store is required")
	}
	if dadTimeout <= 0 {
		return nil, errors.New("DAD timeout must be positive")
	}
	return &ResourceManager{kernel: kernel, store: store, dadTimeout: dadTimeout}, nil
}

func (m *ResourceManager) Apply(ctx context.Context, template ipv6resource.PrefixTemplate, addresses []netip.Addr) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.apply(ctx, template, addresses)
}

func (m *ResourceManager) apply(ctx context.Context, template ipv6resource.PrefixTemplate, addresses []netip.Addr) error {
	refs, err := addressRefs(template, addresses)
	if err != nil {
		return err
	}

	switch template.Mode {
	case ipv6resource.ModeAddress:
		return m.applyAddresses(ctx, refs)
	case ipv6resource.ModeLocalRouteFreebind:
		return m.applyLocalRoute(ctx, template, refs)
	case ipv6resource.ModeExternal:
		return m.validateBindings(ctx, refs, false)
	default:
		return fmt.Errorf("unsupported configuration mode %q", template.Mode)
	}
}

func (m *ResourceManager) Release(ctx context.Context, template ipv6resource.PrefixTemplate, addresses []netip.Addr) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch template.Mode {
	case ipv6resource.ModeAddress:
		refs, err := addressRefs(template, addresses)
		if err != nil {
			return err
		}
		return m.releaseAddresses(ctx, refs)
	case ipv6resource.ModeLocalRouteFreebind:
		return m.releaseRoutes(ctx, []RouteRef{{Interface: template.Interface, Prefix: template.Prefix.Masked()}})
	case ipv6resource.ModeExternal:
		return nil
	default:
		return fmt.Errorf("unsupported configuration mode %q", template.Mode)
	}
}

func (m *ResourceManager) Reconcile(ctx context.Context, desired []DesiredResource) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	desiredAddresses := make(map[AddressRef]struct{})
	desiredRoutes := make(map[RouteRef]struct{})
	for _, resource := range desired {
		refs, err := addressRefs(resource.Template, resource.Addresses)
		if err != nil {
			return err
		}
		switch resource.Template.Mode {
		case ipv6resource.ModeAddress:
			for _, ref := range refs {
				desiredAddresses[ref] = struct{}{}
			}
		case ipv6resource.ModeLocalRouteFreebind:
			desiredRoutes[RouteRef{Interface: resource.Template.Interface, Prefix: resource.Template.Prefix.Masked()}] = struct{}{}
		case ipv6resource.ModeExternal:
		default:
			return fmt.Errorf("unsupported configuration mode %q", resource.Template.Mode)
		}
	}

	var failures []error
	if err := m.removeStale(ctx, desiredAddresses, desiredRoutes); err != nil {
		failures = append(failures, err)
	}
	for _, resource := range desired {
		if err := m.apply(ctx, resource.Template, resource.Addresses); err != nil {
			failures = append(failures, fmt.Errorf("reconcile template %q: %w", resource.Template.Name, err))
		}
	}
	return errors.Join(failures...)
}

func (m *ResourceManager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.removeStale(ctx, map[AddressRef]struct{}{}, map[RouteRef]struct{}{})
}

func (m *ResourceManager) removeStale(ctx context.Context, desiredAddresses map[AddressRef]struct{}, desiredRoutes map[RouteRef]struct{}) error {
	state, err := m.store.Load()
	if err != nil {
		return fmt.Errorf("load network ownership: %w", err)
	}
	var failures []error
	for _, ref := range slices.Clone(state.Addresses) {
		if _, keep := desiredAddresses[ref]; keep {
			continue
		}
		exists, checkErr := m.kernel.AddressExists(ctx, ref)
		if checkErr != nil {
			failures = append(failures, fmt.Errorf("check stale address %s: %w", ref.Address, checkErr))
			continue
		}
		if exists {
			if removeErr := m.kernel.RemoveAddress(ctx, ref); removeErr != nil {
				failures = append(failures, fmt.Errorf("remove stale address %s: %w", ref.Address, removeErr))
				continue
			}
		}
		state.Addresses = removeAddressRef(state.Addresses, ref)
		if saveErr := m.store.Save(state); saveErr != nil {
			failures = append(failures, fmt.Errorf("save network ownership after removing address %s: %w", ref.Address, saveErr))
		}
	}
	for _, ref := range slices.Clone(state.Routes) {
		if _, keep := desiredRoutes[ref]; keep {
			continue
		}
		exists, checkErr := m.kernel.LocalRouteExists(ctx, ref)
		if checkErr != nil {
			failures = append(failures, fmt.Errorf("check stale route %s: %w", ref.Prefix, checkErr))
			continue
		}
		if exists {
			if removeErr := m.kernel.RemoveLocalRoute(ctx, ref); removeErr != nil {
				failures = append(failures, fmt.Errorf("remove stale route %s: %w", ref.Prefix, removeErr))
				continue
			}
		}
		state.Routes = removeRouteRef(state.Routes, ref)
		if saveErr := m.store.Save(state); saveErr != nil {
			failures = append(failures, fmt.Errorf("save network ownership after removing route %s: %w", ref.Prefix, saveErr))
		}
	}
	return errors.Join(failures...)
}

func (m *ResourceManager) releaseAddresses(ctx context.Context, refs []AddressRef) error {
	state, err := m.store.Load()
	if err != nil {
		return fmt.Errorf("load network ownership: %w", err)
	}
	owned := addressSet(state.Addresses)
	for _, ref := range refs {
		if _, ok := owned[ref]; !ok {
			return fmt.Errorf("%w: address %s on %s is not owned", ErrResourceConflict, ref.Address, ref.Interface)
		}
	}

	var failures []error
	for _, ref := range refs {
		exists, checkErr := m.kernel.AddressExists(ctx, ref)
		if checkErr != nil {
			failures = append(failures, fmt.Errorf("check address %s: %w", ref.Address, checkErr))
			continue
		}
		if exists {
			if removeErr := m.kernel.RemoveAddress(ctx, ref); removeErr != nil {
				failures = append(failures, fmt.Errorf("remove address %s: %w", ref.Address, removeErr))
				continue
			}
		}
		state.Addresses = removeAddressRef(state.Addresses, ref)
		if saveErr := m.store.Save(state); saveErr != nil {
			failures = append(failures, fmt.Errorf("save network ownership after releasing address %s: %w", ref.Address, saveErr))
		}
	}
	return errors.Join(failures...)
}

func (m *ResourceManager) releaseRoutes(ctx context.Context, refs []RouteRef) error {
	state, err := m.store.Load()
	if err != nil {
		return fmt.Errorf("load network ownership: %w", err)
	}
	for _, ref := range refs {
		if !slices.Contains(state.Routes, ref) {
			return fmt.Errorf("%w: route %s on %s is not owned", ErrResourceConflict, ref.Prefix, ref.Interface)
		}
	}

	var failures []error
	for _, ref := range refs {
		exists, checkErr := m.kernel.LocalRouteExists(ctx, ref)
		if checkErr != nil {
			failures = append(failures, fmt.Errorf("check route %s: %w", ref.Prefix, checkErr))
			continue
		}
		if exists {
			if removeErr := m.kernel.RemoveLocalRoute(ctx, ref); removeErr != nil {
				failures = append(failures, fmt.Errorf("remove route %s: %w", ref.Prefix, removeErr))
				continue
			}
		}
		state.Routes = removeRouteRef(state.Routes, ref)
		if saveErr := m.store.Save(state); saveErr != nil {
			failures = append(failures, fmt.Errorf("save network ownership after releasing route %s: %w", ref.Prefix, saveErr))
		}
	}
	return errors.Join(failures...)
}

func (m *ResourceManager) applyAddresses(ctx context.Context, refs []AddressRef) error {
	before, err := m.store.Load()
	if err != nil {
		return fmt.Errorf("load network ownership: %w", err)
	}
	state := cloneOwnership(before)
	owned := addressSet(state.Addresses)
	toAdd := make([]AddressRef, 0, len(refs))

	for _, ref := range refs {
		exists, err := m.kernel.AddressExists(ctx, ref)
		if err != nil {
			return fmt.Errorf("check address %s on %s: %w", ref.Address, ref.Interface, err)
		}
		_, isOwned := owned[ref]
		if exists && !isOwned {
			return fmt.Errorf("%w: address %s on %s", ErrResourceConflict, ref.Address, ref.Interface)
		}
		if !exists {
			toAdd = append(toAdd, ref)
		}
	}

	added := make([]AddressRef, 0, len(toAdd))
	rollback := func(cause error) error {
		var rollbackErrors []error
		restored := cloneOwnership(before)
		for i := len(added) - 1; i >= 0; i-- {
			if removeErr := m.kernel.RemoveAddress(context.WithoutCancel(ctx), added[i]); removeErr != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("remove address %s: %w", added[i].Address, removeErr))
				if !slices.Contains(restored.Addresses, added[i]) {
					restored.Addresses = append(restored.Addresses, added[i])
				}
			}
		}
		if saveErr := m.store.Save(restored); saveErr != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore network ownership: %w", saveErr))
		}
		return errors.Join(append([]error{cause}, rollbackErrors...)...)
	}

	ownershipChanged := false
	for _, ref := range toAdd {
		if _, exists := owned[ref]; !exists {
			state.Addresses = append(state.Addresses, ref)
			owned[ref] = struct{}{}
			ownershipChanged = true
		}
	}
	if ownershipChanged {
		if err := m.store.Save(state); err != nil {
			return fmt.Errorf("save network ownership intent: %w", err)
		}
	}
	for _, ref := range toAdd {
		if err := m.kernel.AddAddress(ctx, ref); err != nil {
			return rollback(fmt.Errorf("add address %s on %s: %w", ref.Address, ref.Interface, err))
		}
		added = append(added, ref)
	}

	if err := m.waitForDAD(ctx, added); err != nil {
		return rollback(err)
	}
	if err := m.validateBindings(ctx, refs, false); err != nil {
		return rollback(err)
	}
	return nil
}

func (m *ResourceManager) applyLocalRoute(ctx context.Context, template ipv6resource.PrefixTemplate, refs []AddressRef) error {
	before, err := m.store.Load()
	if err != nil {
		return fmt.Errorf("load network ownership: %w", err)
	}
	state := cloneOwnership(before)
	route := RouteRef{Interface: template.Interface, Prefix: template.Prefix.Masked()}
	exists, err := m.kernel.LocalRouteExists(ctx, route)
	if err != nil {
		return fmt.Errorf("check local route %s on %s: %w", route.Prefix, route.Interface, err)
	}
	owned := slices.Contains(state.Routes, route)
	if exists && !owned {
		return fmt.Errorf("%w: local route %s on %s", ErrResourceConflict, route.Prefix, route.Interface)
	}

	added := false
	rollback := func(cause error) error {
		var rollbackErrors []error
		restored := cloneOwnership(before)
		if added {
			if removeErr := m.kernel.RemoveLocalRoute(context.WithoutCancel(ctx), route); removeErr != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("remove local route %s: %w", route.Prefix, removeErr))
				if !slices.Contains(restored.Routes, route) {
					restored.Routes = append(restored.Routes, route)
				}
			}
		}
		if saveErr := m.store.Save(restored); saveErr != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restore network ownership: %w", saveErr))
		}
		return errors.Join(append([]error{cause}, rollbackErrors...)...)
	}

	if !exists {
		if !owned {
			state.Routes = append(state.Routes, route)
			if err := m.store.Save(state); err != nil {
				return fmt.Errorf("save network ownership intent: %w", err)
			}
		}
		if err := m.kernel.AddLocalRoute(ctx, route); err != nil {
			return rollback(fmt.Errorf("add local route %s on %s: %w", route.Prefix, route.Interface, err))
		}
		added = true
	}

	if err := m.validateBindings(ctx, refs, true); err != nil {
		return rollback(err)
	}
	return nil
}

func (m *ResourceManager) waitForDAD(ctx context.Context, refs []AddressRef) error {
	if len(refs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, m.dadTimeout)
	defer cancel()

	errs := make(chan error, len(refs))
	var wg sync.WaitGroup
	for _, ref := range refs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.kernel.WaitAddressReady(ctx, ref); err != nil {
				errs <- fmt.Errorf("wait for address %s DAD: %w", ref.Address, err)
				cancel()
			}
		}()
	}
	wg.Wait()
	close(errs)

	var failures []error
	for err := range errs {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (m *ResourceManager) validateBindings(ctx context.Context, refs []AddressRef, freebind bool) error {
	for _, ref := range refs {
		if err := m.kernel.ValidateBindable(ctx, ref, freebind); err != nil {
			return fmt.Errorf("validate address %s on %s: %w", ref.Address, ref.Interface, err)
		}
	}
	return nil
}

func addressRefs(template ipv6resource.PrefixTemplate, addresses []netip.Addr) ([]AddressRef, error) {
	if template.Interface == "" || !template.Prefix.IsValid() || !template.Prefix.Addr().Is6() {
		return nil, errors.New("invalid prefix template")
	}
	if len(addresses) == 0 {
		return nil, errors.New("at least one address is required")
	}
	refs := make([]AddressRef, 0, len(addresses))
	seen := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !address.Is6() || !template.Prefix.Contains(address) {
			return nil, fmt.Errorf("address %s is outside prefix %s", address, template.Prefix)
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		refs = append(refs, AddressRef{Interface: template.Interface, Address: address})
	}
	return refs, nil
}

func addressSet(refs []AddressRef) map[AddressRef]struct{} {
	set := make(map[AddressRef]struct{}, len(refs))
	for _, ref := range refs {
		set[ref] = struct{}{}
	}
	return set
}

func cloneOwnership(state Ownership) Ownership {
	return Ownership{
		Addresses: slices.Clone(state.Addresses),
		Routes:    slices.Clone(state.Routes),
	}
}

func removeAddressRef(refs []AddressRef, target AddressRef) []AddressRef {
	return slices.DeleteFunc(refs, func(ref AddressRef) bool { return ref == target })
}

func removeRouteRef(refs []RouteRef, target RouteRef) []RouteRef {
	return slices.DeleteFunc(refs, func(ref RouteRef) bool { return ref == target })
}
