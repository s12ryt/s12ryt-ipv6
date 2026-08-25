package admin

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"sync"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/network"
)

type ResourceStateStore interface {
	Load() (ipv6resource.State, bool, error)
	Save(ipv6resource.State) error
}

type ResourceNetworkManager interface {
	Reconcile(context.Context, []network.DesiredResource) error
}

type DrainTerminator interface {
	ForceDrain(context.Context, string, string, ipv6resource.PoolKind, []netip.Addr) error
}

type ResourceRuntimeSynchronizer interface {
	Sync(ipv6resource.State) error
}

type resourceStateWriteGuard interface {
	CheckWritable() error
}

type ResourceCoordinator struct {
	mu      sync.Mutex
	store   *ipv6resource.Store
	states  ResourceStateStore
	network ResourceNetworkManager
	drains  DrainTerminator
	entropy io.Reader
	runtime ResourceRuntimeSynchronizer
}

func NewResourceCoordinator(states ResourceStateStore, networkManager ResourceNetworkManager, drains DrainTerminator, entropy io.Reader) (*ResourceCoordinator, error) {
	if states == nil {
		return nil, errors.New("resource state store is required")
	}
	if networkManager == nil {
		return nil, errors.New("resource network manager is required")
	}
	if drains == nil {
		return nil, errors.New("drain terminator is required")
	}
	if entropy == nil {
		entropy = rand.Reader
	}
	state, exists, err := states.Load()
	if err != nil {
		return nil, fmt.Errorf("load resource state: %w", err)
	}
	store := ipv6resource.NewStore()
	if exists {
		store, err = ipv6resource.NewStoreFromState(state)
		if err != nil {
			return nil, fmt.Errorf("restore resource state: %w", err)
		}
	}
	return &ResourceCoordinator{store: store, states: states, network: networkManager, drains: drains, entropy: entropy}, nil
}

func (c *ResourceCoordinator) SetRuntimeSynchronizer(runtime ResourceRuntimeSynchronizer) error {
	if runtime == nil {
		return errors.New("resource runtime synchronizer is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runtime != nil {
		return errors.New("resource runtime synchronizer is already configured")
	}
	c.runtime = runtime
	return nil
}

func (c *ResourceCoordinator) Reconcile(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.store.State()
	if err := c.network.Reconcile(ctx, desiredResources(state)); err != nil {
		return err
	}
	if c.runtime != nil {
		if err := c.runtime.Sync(state); err != nil {
			return fmt.Errorf("synchronize resource runtime: %w", err)
		}
	}
	return nil
}

func (c *ResourceCoordinator) Snapshot() ResourceSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return resourceSnapshotFromState(c.store.State())
}

func (c *ResourceCoordinator) State() ipv6resource.State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.store.State()
}

func (c *ResourceCoordinator) CreateTemplate(ctx context.Context, template ipv6resource.PrefixTemplate) error {
	return c.transact(ctx, func(candidate *ipv6resource.Store) error {
		return candidate.AddTemplate(template)
	})
}

func (c *ResourceCoordinator) DeleteTemplate(ctx context.Context, name string) error {
	return c.transact(ctx, func(candidate *ipv6resource.Store) error {
		return candidate.DeleteTemplate(name)
	})
}

func (c *ResourceCoordinator) CreateFixedAddress(ctx context.Context, name, templateName string, requested *netip.Addr) (ipv6resource.FixedAddress, error) {
	var created ipv6resource.FixedAddress
	err := c.transact(ctx, func(candidate *ipv6resource.Store) error {
		template, exists := candidate.Template(templateName)
		if !exists {
			return fmt.Errorf("template %q does not exist", templateName)
		}
		address := netip.Addr{}
		if requested == nil {
			occupied := make(map[netip.Addr]struct{})
			for _, canonical := range candidate.Addresses() {
				occupied[canonical.Address] = struct{}{}
			}
			var err error
			address, err = ipv6resource.RandomAddress(template.Prefix, occupied, c.entropy)
			if err != nil {
				return err
			}
		} else {
			address = requested.Unmap()
		}
		var err error
		created, err = candidate.CreateFixedAddress(name, templateName, address, ownershipForTemplateMode(template.Mode))
		return err
	})
	return created, err
}

func (c *ResourceCoordinator) DeleteFixedAddress(ctx context.Context, name string) error {
	return c.transact(ctx, func(candidate *ipv6resource.Store) error {
		return candidate.DeleteFixedAddress(name)
	})
}

func (c *ResourceCoordinator) CreatePool(ctx context.Context, name string, kind ipv6resource.PoolKind, template string, capacity int, pinned []string) (*ipv6resource.Pool, error) {
	var created *ipv6resource.Pool
	err := c.transact(ctx, func(candidate *ipv6resource.Store) error {
		var err error
		created, err = candidate.CreatePool(name, kind, template, capacity, pinned)
		return err
	})
	return created, err
}

func (c *ResourceCoordinator) DeletePool(ctx context.Context, name string) error {
	return c.transact(ctx, func(candidate *ipv6resource.Store) error {
		return candidate.DeletePool(name)
	})
}

func (c *ResourceCoordinator) RefreshPool(ctx context.Context, name string) (*ipv6resource.Pool, error) {
	var refreshed *ipv6resource.Pool
	err := c.transact(ctx, func(candidate *ipv6resource.Store) error {
		var err error
		refreshed, err = candidate.RefreshPool(name)
		return err
	})
	return refreshed, err
}

func (c *ResourceCoordinator) ForceDrain(ctx context.Context, pool, batch string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureWritable(); err != nil {
		return err
	}
	before := c.store.State()
	candidate, err := ipv6resource.NewStoreFromState(before)
	if err != nil {
		return fmt.Errorf("clone resource state: %w", err)
	}
	if err := candidate.CompleteDrain(pool, batch); err != nil {
		return err
	}
	kind, addresses, err := drainingBatch(before, pool, batch)
	if err != nil {
		return err
	}
	if err := c.drains.ForceDrain(ctx, pool, batch, kind, addresses); err != nil {
		return fmt.Errorf("terminate draining connections: %w", err)
	}
	return c.commitCandidate(ctx, before, candidate)
}

// CompleteAllDrains finishes every draining batch in a single transaction.
// It is intended for the startup sequence, before nodes restore: no proxy
// connection survives a restart, so residual batches can never drain
// naturally and would otherwise linger until an administrator force-drains.
func (c *ResourceCoordinator) CompleteAllDrains(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	before := c.store.State()
	type residualBatch struct{ pool, batch string }
	var residuals []residualBatch
	for _, pool := range before.Pools {
		for _, batch := range pool.Draining {
			residuals = append(residuals, residualBatch{pool: pool.Name, batch: batch.ID})
		}
	}
	if len(residuals) == 0 {
		return nil
	}
	if err := c.ensureWritable(); err != nil {
		return err
	}
	candidate, err := ipv6resource.NewStoreFromState(before)
	if err != nil {
		return fmt.Errorf("clone resource state: %w", err)
	}
	for _, residual := range residuals {
		if err := candidate.CompleteDrain(residual.pool, residual.batch); err != nil {
			return err
		}
	}
	return c.commitCandidate(ctx, before, candidate)
}

func (c *ResourceCoordinator) CompleteDrainedAddress(ctx context.Context, pool string, address netip.Addr) error {
	return c.CompleteDrainedAddresses(ctx, pool, []netip.Addr{address})
}

func (c *ResourceCoordinator) CompleteDrainedAddresses(ctx context.Context, pool string, addresses []netip.Addr) error {
	if strings.TrimSpace(pool) == "" {
		return errors.New("valid draining pool is required")
	}
	normalized := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]struct{}, len(addresses))
	for _, raw := range addresses {
		address := raw.Unmap()
		if !address.IsValid() || !address.Is6() || address.Is4In6() {
			return errors.New("valid native IPv6 addresses are required")
		}
		if _, duplicate := seen[address]; duplicate {
			continue
		}
		seen[address] = struct{}{}
		normalized = append(normalized, address)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	before := c.store.State()
	pending := make([]netip.Addr, 0, len(normalized))
	for _, address := range normalized {
		if isDrainingAddress(before, pool, address) {
			pending = append(pending, address)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	if err := c.ensureWritable(); err != nil {
		return err
	}
	candidate, err := ipv6resource.NewStoreFromState(before)
	if err != nil {
		return fmt.Errorf("clone resource state: %w", err)
	}
	for _, address := range pending {
		if _, err := candidate.CompleteDrainedAddress(pool, address); err != nil {
			return err
		}
	}
	return c.commitCandidate(ctx, before, candidate)
}

func drainingBatch(state ipv6resource.State, poolName, batchID string) (ipv6resource.PoolKind, []netip.Addr, error) {
	for _, pool := range state.Pools {
		if pool.Name != poolName {
			continue
		}
		for _, batch := range pool.Draining {
			if batch.ID == batchID {
				return pool.Kind, append([]netip.Addr(nil), batch.Addresses...), nil
			}
		}
		return "", nil, fmt.Errorf("draining batch %q does not exist", batchID)
	}
	return "", nil, fmt.Errorf("pool %q does not exist", poolName)
}

func isDrainingAddress(state ipv6resource.State, poolName string, address netip.Addr) bool {
	for _, pool := range state.Pools {
		if pool.Name != poolName {
			continue
		}
		for _, batch := range pool.Draining {
			for _, current := range batch.Addresses {
				if current == address {
					return true
				}
			}
		}
	}
	return false
}

func (c *ResourceCoordinator) transact(ctx context.Context, mutate func(*ipv6resource.Store) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureWritable(); err != nil {
		return err
	}
	before := c.store.State()
	candidate, err := ipv6resource.NewStoreFromState(before)
	if err != nil {
		return fmt.Errorf("clone resource state: %w", err)
	}
	if err := mutate(candidate); err != nil {
		return err
	}
	return c.commitCandidate(ctx, before, candidate)
}

func (c *ResourceCoordinator) ensureWritable() error {
	guard, guarded := c.states.(resourceStateWriteGuard)
	if !guarded {
		return nil
	}
	if err := guard.CheckWritable(); err != nil {
		return fmt.Errorf("resource state is not writable: %w", err)
	}
	return nil
}

func (c *ResourceCoordinator) commitCandidate(ctx context.Context, before ipv6resource.State, candidate *ipv6resource.Store) error {
	after := candidate.State()
	if err := c.network.Reconcile(ctx, desiredResources(after)); err != nil {
		rollbackErr := c.network.Reconcile(context.WithoutCancel(ctx), desiredResources(before))
		return errors.Join(fmt.Errorf("apply resource network state: %w", err), wrapResourceRollback(rollbackErr))
	}
	if c.runtime != nil {
		if err := c.runtime.Sync(after); err != nil {
			rollbackContext := context.WithoutCancel(ctx)
			runtimeRollbackErr := c.runtime.Sync(before)
			networkRollbackErr := c.network.Reconcile(rollbackContext, desiredResources(before))
			return errors.Join(
				fmt.Errorf("synchronize resource runtime: %w", err),
				wrapRuntimeRollback(runtimeRollbackErr),
				wrapResourceRollback(networkRollbackErr),
			)
		}
	}
	if err := c.states.Save(after); err != nil {
		rollbackContext := context.WithoutCancel(ctx)
		var runtimeRollbackErr error
		if c.runtime != nil {
			runtimeRollbackErr = c.runtime.Sync(before)
		}
		networkRollbackErr := c.network.Reconcile(rollbackContext, desiredResources(before))
		return errors.Join(
			fmt.Errorf("persist resource state: %w", err),
			wrapRuntimeRollback(runtimeRollbackErr),
			wrapResourceRollback(networkRollbackErr),
		)
	}
	c.store = candidate
	return nil
}

func desiredResources(state ipv6resource.State) []network.DesiredResource {
	byTemplate := make(map[string][]netip.Addr, len(state.Templates))
	for _, address := range state.Addresses {
		byTemplate[address.Template] = append(byTemplate[address.Template], address.Address)
	}
	result := make([]network.DesiredResource, 0, len(byTemplate))
	for _, template := range state.Templates {
		addresses := byTemplate[template.Name]
		if len(addresses) == 0 {
			continue
		}
		result = append(result, network.DesiredResource{Template: template, Addresses: append([]netip.Addr(nil), addresses...)})
	}
	return result
}

func resourceSnapshotFromState(state ipv6resource.State) ResourceSnapshot {
	snapshot := ResourceSnapshot{
		Templates: append([]ipv6resource.PrefixTemplate(nil), state.Templates...),
		Fixed:     append([]ipv6resource.FixedAddress(nil), state.Fixed...),
		Addresses: append([]ipv6resource.CanonicalAddress(nil), state.Addresses...),
		Pools:     make([]*ipv6resource.Pool, 0, len(state.Pools)),
	}
	for i := range state.Pools {
		pool := state.Pools[i]
		pool.Pinned = append([]netip.Addr(nil), pool.Pinned...)
		pool.Active = append([]netip.Addr(nil), pool.Active...)
		pool.Draining = append([]ipv6resource.DrainBatch(nil), pool.Draining...)
		for batchIndex := range pool.Draining {
			pool.Draining[batchIndex].Addresses = append([]netip.Addr(nil), pool.Draining[batchIndex].Addresses...)
		}
		snapshot.Pools = append(snapshot.Pools, &pool)
	}
	return snapshot
}

func ownershipForTemplateMode(mode ipv6resource.ConfigMode) ipv6resource.Ownership {
	switch mode {
	case ipv6resource.ModeLocalRouteFreebind:
		return ipv6resource.OwnershipRoute
	case ipv6resource.ModeExternal:
		return ipv6resource.OwnershipExternal
	default:
		return ipv6resource.OwnershipAddress
	}
}

func wrapResourceRollback(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("restore previous resource network state: %w", err)
}

func wrapRuntimeRollback(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("restore previous resource runtime state: %w", err)
}

var _ ResourceService = (*ResourceCoordinator)(nil)
