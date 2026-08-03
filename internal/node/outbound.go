package node

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

var ErrAmbiguousOutbound = errors.New("outbound resource name is ambiguous")

type OutboundConnectorFactory func(ipv6resource.PrefixTemplate) (proxy.Connector, error)

type OutboundRegistryOptions struct {
	Resolver    proxy.DestinationResolver
	Policy      func() policy.DestinationPolicy
	NAT64Prefix func() netip.Prefix
	Connector   OutboundConnectorFactory
	OnDrained   func(string, netip.Addr)
}

type outboundResource struct {
	template ipv6resource.PrefixTemplate
	sources  *proxy.SourcePool
	draining map[netip.Addr]struct{}
}

type OutboundRegistry struct {
	syncMu      sync.Mutex
	mu          sync.RWMutex
	resolver    proxy.DestinationResolver
	policy      func() policy.DestinationPolicy
	nat64Prefix func() netip.Prefix
	connector   OutboundConnectorFactory
	onDrained   func(string, netip.Addr)
	fixed       map[string]outboundResource
	pools       map[string]outboundResource
}

func NewOutboundRegistry(options OutboundRegistryOptions) (*OutboundRegistry, error) {
	if options.Resolver == nil {
		return nil, errors.New("destination resolver is required")
	}
	if options.Policy == nil {
		return nil, errors.New("destination policy provider is required")
	}
	if options.NAT64Prefix == nil {
		return nil, errors.New("NAT64 prefix provider is required")
	}
	if options.Connector == nil {
		return nil, errors.New("outbound connector factory is required")
	}
	return &OutboundRegistry{
		resolver: options.Resolver, policy: options.Policy, nat64Prefix: options.NAT64Prefix,
		connector: options.Connector, onDrained: options.OnDrained,
		fixed: make(map[string]outboundResource), pools: make(map[string]outboundResource),
	}, nil
}

func (r *OutboundRegistry) Sync(state ipv6resource.State) error {
	validated, err := ipv6resource.NewStoreFromState(state)
	if err != nil {
		return fmt.Errorf("validate outbound resources: %w", err)
	}
	templates := make(map[string]ipv6resource.PrefixTemplate)
	for _, template := range validated.Templates() {
		templates[template.Name] = template
	}

	fixedAddresses := validated.FixedAddresses()
	pools := validated.Pools()
	poolNames := make(map[string]struct{}, len(pools))
	for _, pool := range pools {
		poolNames[pool.Name] = struct{}{}
	}
	for _, fixed := range fixedAddresses {
		if _, ambiguous := poolNames[fixed.Name]; ambiguous {
			return fmt.Errorf("%w: %q", ErrAmbiguousOutbound, fixed.Name)
		}
	}

	r.syncMu.Lock()
	defer r.syncMu.Unlock()
	r.mu.RLock()
	existingPools := make(map[string]outboundResource, len(r.pools))
	for name, resource := range r.pools {
		existingPools[name] = resource
	}
	r.mu.RUnlock()

	nextFixed := make(map[string]outboundResource, len(fixedAddresses))
	for _, fixed := range fixedAddresses {
		template := templates[fixed.Template]
		sources, poolErr := proxy.NewSourcePool([]netip.Addr{fixed.Address}, nil)
		if poolErr != nil {
			return fmt.Errorf("build fixed outbound %q: %w", fixed.Name, poolErr)
		}
		nextFixed[fixed.Name] = outboundResource{template: template, sources: sources}
	}

	type poolUpdate struct {
		pool      *proxy.SourcePool
		addresses []netip.Addr
	}
	nextPools := make(map[string]outboundResource)
	updates := make([]poolUpdate, 0, len(pools))
	for _, pool := range pools {
		if pool.Kind == ipv6resource.PoolInbound {
			continue
		}
		template := templates[pool.Template]
		draining := make(map[netip.Addr]struct{})
		for _, batch := range pool.Draining {
			for _, address := range batch.Addresses {
				draining[address.Unmap()] = struct{}{}
			}
		}
		if existing, ok := existingPools[pool.Name]; ok && existing.template == template {
			updates = append(updates, poolUpdate{pool: existing.sources, addresses: pool.Active})
			existing.draining = draining
			nextPools[pool.Name] = existing
			continue
		}
		name := pool.Name
		sources, poolErr := proxy.NewSourcePool(pool.Active, func(address netip.Addr) {
			if r.onDrained != nil {
				r.onDrained(name, address)
			}
		})
		if poolErr != nil {
			return fmt.Errorf("build outbound pool %q: %w", pool.Name, poolErr)
		}
		nextPools[pool.Name] = outboundResource{template: template, sources: sources, draining: draining}
	}
	for _, update := range updates {
		if err := update.pool.Replace(update.addresses); err != nil {
			return fmt.Errorf("update outbound pool: %w", err)
		}
	}

	r.mu.Lock()
	r.fixed = nextFixed
	r.pools = nextPools
	r.mu.Unlock()
	return nil
}

func (r *OutboundRegistry) ForceDrain(name string, addresses []netip.Addr) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("outbound pool name is required")
	}
	if len(addresses) == 0 {
		return errors.New("at least one draining outbound address is required")
	}
	r.syncMu.Lock()
	defer r.syncMu.Unlock()
	r.mu.RLock()
	resource, exists := r.pools[name]
	r.mu.RUnlock()
	if !exists {
		return fmt.Errorf("outbound pool %q does not exist", name)
	}
	normalized := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !address.IsValid() || !address.Is6() || address.Is4In6() {
			return errors.New("draining outbound address must be native IPv6")
		}
		if _, duplicate := seen[address]; duplicate {
			continue
		}
		if _, draining := resource.draining[address]; !draining {
			return fmt.Errorf("address %s is not draining from outbound pool %q", address, name)
		}
		seen[address] = struct{}{}
		normalized = append(normalized, address)
	}
	var failures []error
	for _, address := range normalized {
		if err := resource.sources.ForceDrain(address); err != nil && !errors.Is(err, proxy.ErrSourceNotDraining) {
			failures = append(failures, fmt.Errorf("force drain outbound address %s: %w", address, err))
		}
	}
	return errors.Join(failures...)
}

func (r *OutboundRegistry) BuildDialer(config Config) (proxy.ProxyDialer, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(config.Outbound)
	r.mu.RLock()
	fixed, fixedFound := r.fixed[name]
	pool, poolFound := r.pools[name]
	r.mu.RUnlock()
	if fixedFound && poolFound {
		return nil, fmt.Errorf("%w: %q", ErrAmbiguousOutbound, name)
	}
	resource := fixed
	if poolFound {
		resource = pool
	} else if !fixedFound {
		return nil, fmt.Errorf("outbound resource %q does not exist", name)
	}
	connector, err := r.connector(resource.template)
	if err != nil {
		return nil, fmt.Errorf("build connector for outbound %q: %w", name, err)
	}
	if connector == nil {
		return nil, fmt.Errorf("build connector for outbound %q: connector is nil", name)
	}
	return proxy.NewDialer(proxy.DialerOptions{
		Resolver: r.resolver, Sources: resource.sources, Connector: connector,
		Policy: r.policy, NAT64Prefix: r.nat64Prefix,
		ULAOverride: config.ULAOverride, Timeout: config.DialTimeout,
	})
}

var _ NodeDialerFactory = (*OutboundRegistry)(nil)
