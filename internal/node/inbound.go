package node

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

var ErrAmbiguousInbound = errors.New("inbound resource name is ambiguous")

type inboundResource struct {
	template  ipv6resource.PrefixTemplate
	addresses []netip.Addr
}

type InboundRegistry struct {
	mu    sync.RWMutex
	fixed map[string]inboundResource
	pools map[string]inboundResource
}

func NewInboundRegistry() (*InboundRegistry, error) {
	return &InboundRegistry{
		fixed: make(map[string]inboundResource),
		pools: make(map[string]inboundResource),
	}, nil
}

func (r *InboundRegistry) Sync(state ipv6resource.State) error {
	validated, err := ipv6resource.NewStoreFromState(state)
	if err != nil {
		return fmt.Errorf("validate inbound resources: %w", err)
	}
	templates := make(map[string]ipv6resource.PrefixTemplate)
	for _, template := range validated.Templates() {
		templates[template.Name] = template
	}
	nextFixed := make(map[string]inboundResource)
	for _, fixed := range validated.FixedAddresses() {
		nextFixed[fixed.Name] = inboundResource{
			template: templates[fixed.Template], addresses: []netip.Addr{fixed.Address},
		}
	}
	nextPools := make(map[string]inboundResource)
	for _, pool := range validated.Pools() {
		if pool.Kind != ipv6resource.PoolInbound {
			continue
		}
		if _, ambiguous := nextFixed[pool.Name]; ambiguous {
			return fmt.Errorf("%w: %q", ErrAmbiguousInbound, pool.Name)
		}
		nextPools[pool.Name] = inboundResource{
			template: templates[pool.Template], addresses: append([]netip.Addr(nil), pool.Active...),
		}
	}
	r.mu.Lock()
	r.fixed = nextFixed
	r.pools = nextPools
	r.mu.Unlock()
	return nil
}

func (r *InboundRegistry) Resolve(config Config) (Config, error) {
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	resolved := cloneConfig(config)
	if config.InboundMode == "" {
		return resolved, nil
	}
	resolved.Inbound = nil
	if config.InboundMode == InboundIPv4 || config.InboundMode == InboundDual {
		resolved.Inbound = append(resolved.Inbound, proxy.BindSpec{
			Protocol: proxy.BindTCP, Family: proxy.BindIPv4,
		})
	}
	if config.InboundMode == InboundIPv4 {
		return resolved, nil
	}

	name := strings.TrimSpace(config.InboundResource)
	r.mu.RLock()
	fixed, fixedFound := r.fixed[name]
	pool, poolFound := r.pools[name]
	r.mu.RUnlock()
	if fixedFound && poolFound {
		return Config{}, fmt.Errorf("%w: %q", ErrAmbiguousInbound, name)
	}
	resource := fixed
	if poolFound {
		resource = pool
	} else if !fixedFound {
		return Config{}, fmt.Errorf("inbound resource %q does not exist", name)
	}
	freebind := resource.template.Mode == ipv6resource.ModeLocalRouteFreebind
	for _, address := range resource.addresses {
		resolved.Inbound = append(resolved.Inbound, proxy.BindSpec{
			Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: address,
			Interface: resource.template.Interface, Freebind: freebind,
		})
	}
	return resolved, nil
}
