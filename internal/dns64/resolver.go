package dns64

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
)

type RecordType uint16

const (
	TypeA    RecordType = 1
	TypeAAAA RecordType = 28

	minimumTTL  = 30 * time.Second
	maximumTTL  = 10 * time.Minute
	negativeTTL = 30 * time.Second
)

var (
	ErrNAT64Unavailable   = errors.New("NAT64 is unavailable")
	ErrNoAllowedAddresses = errors.New("DNS response has no allowed addresses")
)

type Endpoint struct {
	Name       string
	Address    netip.Addr
	Port       uint16
	ServerName string
}

type QueryResult struct {
	Addresses []netip.Addr
	TTL       time.Duration
}

type Queryer interface {
	Query(context.Context, Endpoint, string, RecordType) (QueryResult, error)
}

type Resolution struct {
	Addresses   []netip.Addr
	Source      string
	Synthesized bool
}

type cacheKey struct {
	name   string
	record RecordType
}

type cacheEntry struct {
	addresses []netip.Addr
	source    string
	expires   time.Time
}

type Resolver struct {
	mu        sync.RWMutex
	endpoints []Endpoint
	queryer   Queryer
	now       func() time.Time
	cache     map[cacheKey]cacheEntry
}

func NewResolver(endpoints []Endpoint, queryer Queryer, now func() time.Time) (*Resolver, error) {
	if err := validateEndpoints(endpoints); err != nil {
		return nil, err
	}
	if queryer == nil {
		return nil, errors.New("DNS queryer is required")
	}
	if now == nil {
		now = time.Now
	}
	return &Resolver{
		endpoints: append([]Endpoint(nil), endpoints...),
		queryer:   queryer,
		now:       now,
		cache:     make(map[cacheKey]cacheEntry),
	}, nil
}

func (r *Resolver) Endpoints() []Endpoint {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Endpoint(nil), r.endpoints...)
}

func (r *Resolver) UpdateEndpoints(endpoints []Endpoint) error {
	if err := validateEndpoints(endpoints); err != nil {
		return err
	}
	r.mu.Lock()
	r.endpoints = append([]Endpoint(nil), endpoints...)
	r.cache = make(map[cacheKey]cacheEntry)
	r.mu.Unlock()
	return nil
}

func (r *Resolver) Resolve(
	ctx context.Context,
	host string,
	destinationPolicy policy.DestinationPolicy,
	ulaOverride policy.ULAOverride,
	nat64Prefix netip.Prefix,
) (Resolution, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return Resolution{}, errors.New("destination host is required")
	}
	if nat64Prefix.IsValid() {
		if err := ValidateNAT64Prefix(nat64Prefix); err != nil {
			return Resolution{}, err
		}
		destinationPolicy.NAT64Prefix = nat64Prefix.Masked()
	}
	if literal, err := netip.ParseAddr(host); err == nil {
		return resolveLiteral(literal.Unmap(), destinationPolicy, ulaOverride, nat64Prefix)
	}

	name := normalizeName(host)
	aaaa, err := r.lookup(ctx, name, TypeAAAA)
	if err != nil {
		return Resolution{}, err
	}
	if len(aaaa.addresses) > 0 {
		allowed := allowedAddresses(aaaa.addresses, destinationPolicy, ulaOverride)
		if len(allowed) > 0 {
			return Resolution{Addresses: allowed, Source: aaaa.source}, nil
		}
		if !nat64Prefix.IsValid() {
			return Resolution{}, ErrNoAllowedAddresses
		}
	}

	a, err := r.lookup(ctx, name, TypeA)
	if err != nil {
		return Resolution{}, err
	}
	if !nat64Prefix.IsValid() {
		return Resolution{}, ErrNAT64Unavailable
	}
	allowed := make([]netip.Addr, 0, len(a.addresses))
	for _, address := range a.addresses {
		address = address.Unmap()
		if !address.Is4() || destinationPolicy.Check(address, ulaOverride) != nil {
			continue
		}
		synthesized := synthesize(nat64Prefix, address)
		if destinationPolicy.Check(synthesized, ulaOverride) == nil {
			allowed = append(allowed, synthesized)
		}
	}
	if len(allowed) == 0 {
		return Resolution{}, ErrNoAllowedAddresses
	}
	return Resolution{Addresses: allowed, Source: a.source, Synthesized: true}, nil
}

func (r *Resolver) lookup(ctx context.Context, name string, record RecordType) (cacheEntry, error) {
	key := cacheKey{name: name, record: record}
	now := r.now()
	r.mu.RLock()
	entry, ok := r.cache[key]
	endpoints := append([]Endpoint(nil), r.endpoints...)
	r.mu.RUnlock()
	if ok && now.Before(entry.expires) {
		entry.addresses = append([]netip.Addr(nil), entry.addresses...)
		return entry, nil
	}

	var failures []error
	for _, endpoint := range endpoints {
		result, err := r.queryer.Query(ctx, endpoint, name, record)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", endpoint.Name, err))
			continue
		}
		ttl := result.TTL
		if len(result.Addresses) == 0 {
			ttl = negativeTTL
		} else {
			ttl = clampTTL(ttl)
		}
		entry = cacheEntry{
			addresses: append([]netip.Addr(nil), result.Addresses...),
			source:    endpoint.Name,
			expires:   now.Add(ttl),
		}
		r.mu.Lock()
		r.cache[key] = entry
		r.mu.Unlock()
		return entry, nil
	}
	return cacheEntry{}, fmt.Errorf("all DNS resolvers failed: %w", errors.Join(failures...))
}

func resolveLiteral(address netip.Addr, destinationPolicy policy.DestinationPolicy, ulaOverride policy.ULAOverride, nat64Prefix netip.Prefix) (Resolution, error) {
	if err := destinationPolicy.Check(address, ulaOverride); err != nil {
		return Resolution{}, err
	}
	if address.Is4() {
		if !nat64Prefix.IsValid() {
			return Resolution{}, ErrNAT64Unavailable
		}
		synthesized := synthesize(nat64Prefix, address)
		if err := destinationPolicy.Check(synthesized, ulaOverride); err != nil {
			return Resolution{}, err
		}
		return Resolution{Addresses: []netip.Addr{synthesized}, Source: "literal", Synthesized: true}, nil
	}
	return Resolution{Addresses: []netip.Addr{address}, Source: "literal"}, nil
}

func allowedAddresses(addresses []netip.Addr, destinationPolicy policy.DestinationPolicy, ulaOverride policy.ULAOverride) []netip.Addr {
	allowed := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if address.Is6() && !address.Is4In6() && destinationPolicy.Check(address, ulaOverride) == nil {
			allowed = append(allowed, address)
		}
	}
	return allowed
}

func synthesize(prefix netip.Prefix, address netip.Addr) netip.Addr {
	bytes := prefix.Masked().Addr().As16()
	ipv4 := address.As4()
	copy(bytes[12:], ipv4[:])
	return netip.AddrFrom16(bytes)
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, ".")) + "."
}

func clampTTL(ttl time.Duration) time.Duration {
	if ttl < minimumTTL {
		return minimumTTL
	}
	if ttl > maximumTTL {
		return maximumTTL
	}
	return ttl
}

func validateEndpoint(endpoint Endpoint) error {
	if endpoint.Name == "" {
		return errors.New("name is required")
	}
	if !endpoint.Address.IsValid() || !endpoint.Address.Is6() || endpoint.Address.Is4In6() {
		return errors.New("address must be a literal IPv6 address")
	}
	if endpoint.Port == 0 {
		return errors.New("port is required")
	}
	if endpoint.ServerName == "" {
		return errors.New("TLS server name is required")
	}
	return nil
}

func validateEndpoints(endpoints []Endpoint) error {
	if len(endpoints) == 0 {
		return errors.New("at least one DNS resolver is required")
	}
	for i, endpoint := range endpoints {
		if err := validateEndpoint(endpoint); err != nil {
			return fmt.Errorf("resolver %d: %w", i, err)
		}
	}
	return nil
}
