package app

import (
	"errors"
	"fmt"
	"net/netip"
	"sync"

	"github.com/s12ryt/s12ryt-ipv6/internal/config"
	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
)

type PolicyProviderOptions struct {
	ScanHostAddresses func() ([]netip.Addr, error)
	Configuration     func() config.Config
	NAT64Prefix       func() netip.Prefix
}

type PolicyProvider struct {
	mu                sync.RWMutex
	scanHostAddresses func() ([]netip.Addr, error)
	configuration     func() config.Config
	nat64Prefix       func() netip.Prefix
	localAddresses    map[netip.Addr]struct{}
	managedAddresses  map[netip.Addr]struct{}
}

func NewPolicyProvider(options PolicyProviderOptions) (*PolicyProvider, error) {
	if options.ScanHostAddresses == nil {
		return nil, errors.New("host address scanner is required")
	}
	if options.Configuration == nil {
		return nil, errors.New("configuration provider is required")
	}
	if options.NAT64Prefix == nil {
		return nil, errors.New("NAT64 prefix provider is required")
	}
	return &PolicyProvider{
		scanHostAddresses: options.ScanHostAddresses,
		configuration:     options.Configuration,
		nat64Prefix:       options.NAT64Prefix,
		localAddresses:    make(map[netip.Addr]struct{}),
		managedAddresses:  make(map[netip.Addr]struct{}),
	}, nil
}

func (p *PolicyProvider) RefreshHostAddresses() error {
	addresses, err := p.scanHostAddresses()
	if err != nil {
		return fmt.Errorf("scan host addresses: %w", err)
	}
	next := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		if !address.IsValid() || address.Zone() != "" {
			return errors.New("host address snapshot contains an invalid address")
		}
		next[address.Unmap()] = struct{}{}
	}
	p.mu.Lock()
	p.localAddresses = next
	p.mu.Unlock()
	return nil
}

func (p *PolicyProvider) Sync(state ipv6resource.State) error {
	validated, err := ipv6resource.NewStoreFromState(state)
	if err != nil {
		return fmt.Errorf("validate managed address state: %w", err)
	}
	addresses := validated.Addresses()
	next := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		next[address.Address.Unmap()] = struct{}{}
	}
	p.mu.Lock()
	p.managedAddresses = next
	p.mu.Unlock()
	return nil
}

// Policy 回傳目前的目的地政策。LocalAddresses 與 ManagedAddresses 是
// 唯讀共享視圖：內部快照以 build-new-then-swap 發佈、發佈後不可變，
// 呼叫方不得修改回傳的 map；每個出站連線都會呼叫此方法，避免逐次複製
// 大型地址集的資料路徑成本。
func (p *PolicyProvider) Policy() policy.DestinationPolicy {
	p.mu.RLock()
	local := p.localAddresses
	managed := p.managedAddresses
	p.mu.RUnlock()
	configuration := p.configuration()
	return policy.DestinationPolicy{
		AllowULA:         configuration.AllowULA,
		NAT64Prefix:      p.nat64Prefix(),
		LocalAddresses:   local,
		ManagedAddresses: managed,
	}
}

var _ interface {
	Sync(ipv6resource.State) error
} = (*PolicyProvider)(nil)
