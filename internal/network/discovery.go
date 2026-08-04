package network

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
)

type PrefixSource string

const (
	PrefixSourceAddress PrefixSource = "address"
	PrefixSourceRoute   PrefixSource = "route"
)

type DiscoveredInterface struct {
	Name  string
	Index int
}

type DiscoveredPrefix struct {
	Interface string
	Prefix    netip.Prefix
	Sources   []PrefixSource
}

type NetworkDiscoverySnapshot struct {
	Interfaces []DiscoveredInterface
	Prefixes   []DiscoveredPrefix
}

type NetworkDiscovery interface {
	Discover(context.Context) (NetworkDiscoverySnapshot, error)
}

type discoveryLink struct {
	Name     string
	Index    int
	Up       bool
	Loopback bool
}

type networkDiscoveryDriver interface {
	Links(context.Context) ([]discoveryLink, error)
	Addresses(context.Context, int) ([]netip.Prefix, error)
	Routes(context.Context, int) ([]netip.Prefix, error)
}

type networkDiscovery struct {
	driver networkDiscoveryDriver
}

type discoveryPrefixIdentity struct {
	interfaceName string
	prefix        netip.Prefix
}

func newNetworkDiscovery(driver networkDiscoveryDriver) (*networkDiscovery, error) {
	if driver == nil {
		return nil, errors.New("network discovery driver is required")
	}
	return &networkDiscovery{driver: driver}, nil
}

func (d *networkDiscovery) Discover(ctx context.Context) (NetworkDiscoverySnapshot, error) {
	if ctx == nil {
		return NetworkDiscoverySnapshot{}, errors.New("network discovery context is required")
	}
	if err := ctx.Err(); err != nil {
		return NetworkDiscoverySnapshot{}, err
	}
	links, err := d.driver.Links(ctx)
	if err != nil {
		return NetworkDiscoverySnapshot{}, fmt.Errorf("list network interfaces: %w", err)
	}
	links = selectableLinks(links)
	result := NetworkDiscoverySnapshot{
		Interfaces: make([]DiscoveredInterface, 0, len(links)),
		Prefixes:   make([]DiscoveredPrefix, 0),
	}
	type sourceSet struct {
		address bool
		route   bool
	}
	candidates := make(map[discoveryPrefixIdentity]sourceSet)
	for _, link := range links {
		if err := ctx.Err(); err != nil {
			return NetworkDiscoverySnapshot{}, err
		}
		result.Interfaces = append(result.Interfaces, DiscoveredInterface{Name: link.Name, Index: link.Index})
		addresses, err := d.driver.Addresses(ctx, link.Index)
		if err != nil {
			return NetworkDiscoverySnapshot{}, fmt.Errorf("list IPv6 addresses on %s: %w", link.Name, err)
		}
		routes, err := d.driver.Routes(ctx, link.Index)
		if err != nil {
			return NetworkDiscoverySnapshot{}, fmt.Errorf("list IPv6 routes on %s: %w", link.Name, err)
		}
		for _, prefix := range addresses {
			if normalized, ok := discoveredGlobalPrefix(prefix); ok {
				key := discoveryPrefixIdentity{interfaceName: link.Name, prefix: normalized}
				value := candidates[key]
				value.address = true
				candidates[key] = value
			}
		}
		for _, prefix := range routes {
			if normalized, ok := discoveredGlobalPrefix(prefix); ok {
				key := discoveryPrefixIdentity{interfaceName: link.Name, prefix: normalized}
				value := candidates[key]
				value.route = true
				candidates[key] = value
			}
		}
	}
	for key, sources := range candidates {
		candidate := DiscoveredPrefix{Interface: key.interfaceName, Prefix: key.prefix}
		if sources.address {
			candidate.Sources = append(candidate.Sources, PrefixSourceAddress)
		}
		if sources.route {
			candidate.Sources = append(candidate.Sources, PrefixSourceRoute)
		}
		result.Prefixes = append(result.Prefixes, candidate)
	}
	sort.Slice(result.Prefixes, func(left, right int) bool {
		if result.Prefixes[left].Interface != result.Prefixes[right].Interface {
			return result.Prefixes[left].Interface < result.Prefixes[right].Interface
		}
		leftPrefix, rightPrefix := result.Prefixes[left].Prefix, result.Prefixes[right].Prefix
		if comparison := leftPrefix.Addr().Compare(rightPrefix.Addr()); comparison != 0 {
			return comparison < 0
		}
		return leftPrefix.Bits() < rightPrefix.Bits()
	})
	return result, nil
}

func selectableLinks(values []discoveryLink) []discoveryLink {
	result := make([]discoveryLink, 0, len(values))
	seen := make(map[string]struct{})
	for _, link := range values {
		if !link.Up || link.Loopback || link.Name == "" || link.Index <= 0 {
			continue
		}
		key := fmt.Sprintf("%d\x00%s", link.Index, link.Name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, link)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Name != result[right].Name {
			return result[left].Name < result[right].Name
		}
		return result[left].Index < result[right].Index
	})
	return result
}

func discoveredGlobalPrefix(value netip.Prefix) (netip.Prefix, bool) {
	if !value.IsValid() || value.Bits() < 3 {
		return netip.Prefix{}, false
	}
	address := value.Addr()
	if !address.Is6() || address.Is4In6() || address.Zone() != "" {
		return netip.Prefix{}, false
	}
	normalized := netip.PrefixFrom(address, value.Bits()).Masked()
	if !netip.MustParsePrefix("2000::/3").Contains(normalized.Addr()) {
		return netip.Prefix{}, false
	}
	return normalized, true
}
