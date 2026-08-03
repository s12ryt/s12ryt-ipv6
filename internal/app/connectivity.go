package app

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/admin"
	"github.com/s12ryt/s12ryt-ipv6/internal/dns64"
	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

const (
	connectivityNativeHost = "ipv6.cloudflare.com"
	connectivityNAT64IPv4  = "1.1.1.1"
	connectivityPort       = 443
)

type ConnectivityTesterOptions struct {
	Queryer     dns64.Queryer
	Resolver    proxy.DestinationResolver
	Endpoints   func() []dns64.Endpoint
	Resources   func() ipv6resource.State
	Policy      func() policy.DestinationPolicy
	NAT64Prefix func() netip.Prefix
	Connector   func(ipv6resource.PrefixTemplate) (proxy.Connector, error)
	Timeout     time.Duration
}

type ConnectivityTester struct {
	queryer     dns64.Queryer
	resolver    proxy.DestinationResolver
	endpoints   func() []dns64.Endpoint
	resources   func() ipv6resource.State
	policy      func() policy.DestinationPolicy
	nat64Prefix func() netip.Prefix
	connector   func(ipv6resource.PrefixTemplate) (proxy.Connector, error)
	timeout     time.Duration
}

type connectivitySource struct {
	name     string
	kind     string
	address  netip.Addr
	template ipv6resource.PrefixTemplate
}

func NewConnectivityTester(options ConnectivityTesterOptions) (*ConnectivityTester, error) {
	if options.Queryer == nil || options.Resolver == nil {
		return nil, errors.New("connectivity DNS dependencies are required")
	}
	if options.Endpoints == nil || options.Resources == nil {
		return nil, errors.New("connectivity state providers are required")
	}
	if options.Policy == nil || options.NAT64Prefix == nil || options.Connector == nil {
		return nil, errors.New("connectivity network dependencies are required")
	}
	if options.Timeout <= 0 {
		return nil, errors.New("connectivity timeout must be positive")
	}
	return &ConnectivityTester{
		queryer: options.Queryer, resolver: options.Resolver,
		endpoints: options.Endpoints, resources: options.Resources,
		policy: options.Policy, nat64Prefix: options.NAT64Prefix,
		connector: options.Connector, timeout: options.Timeout,
	}, nil
}

func (t *ConnectivityTester) Test(ctx context.Context) ([]admin.ConnectivityCheck, error) {
	if ctx == nil {
		return nil, errors.New("connectivity context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store, err := ipv6resource.NewStoreFromState(t.resources())
	if err != nil {
		return nil, fmt.Errorf("validate connectivity resources: %w", err)
	}
	sources, err := connectivitySources(store)
	if err != nil {
		return nil, err
	}

	checks := make([]admin.ConnectivityCheck, 0, len(t.endpoints())+2+len(sources))
	for _, endpoint := range t.endpoints() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		check := admin.ConnectivityCheck{Name: endpoint.Name, Kind: "dot", Address: endpoint.Address.String(), Success: true}
		if _, queryErr := t.queryer.Query(ctx, endpoint, "ipv4only.arpa.", dns64.TypeAAAA); queryErr != nil {
			check.Success = false
			check.Error = "DoT query failed"
		}
		checks = append(checks, check)
	}

	native, nativeErr := t.resolve(ctx, connectivityNativeHost, netip.Prefix{}, false)
	checks = append(checks, t.destinationCheck(ctx, "原生 IPv6", "native-ipv6", native, nativeErr, sources, "native IPv6"))
	nat64, nat64Err := t.resolve(ctx, connectivityNAT64IPv4, t.nat64Prefix(), true)
	checks = append(checks, t.destinationCheck(ctx, "NAT64", "nat64", nat64, nat64Err, sources, "NAT64"))

	for _, source := range sources {
		check := admin.ConnectivityCheck{Name: source.name, Kind: source.kind, Address: source.address.String()}
		if nativeErr != nil {
			check.Error = "native IPv6 resolution failed"
		} else if err := t.dial(ctx, source, native); err != nil {
			check.Error = "outbound connection failed"
		} else {
			check.Success = true
		}
		checks = append(checks, check)
	}
	return checks, nil
}

func (t *ConnectivityTester) resolve(ctx context.Context, host string, prefix netip.Prefix, requireSynthesized bool) (netip.AddrPort, error) {
	resolution, err := t.resolver.Resolve(ctx, host, t.policy(), policy.ULAInherit, prefix)
	if err != nil {
		return netip.AddrPort{}, err
	}
	if requireSynthesized && !resolution.Synthesized {
		return netip.AddrPort{}, errors.New("resolver did not synthesize NAT64 destination")
	}
	for _, address := range resolution.Addresses {
		if address.IsValid() && address.Is6() && !address.Is4In6() {
			return netip.AddrPortFrom(address, connectivityPort), nil
		}
	}
	return netip.AddrPort{}, errors.New("resolver returned no native IPv6 destination")
}

func (t *ConnectivityTester) destinationCheck(
	ctx context.Context,
	name string,
	kind string,
	destination netip.AddrPort,
	resolveErr error,
	sources []connectivitySource,
	errorPrefix string,
) admin.ConnectivityCheck {
	check := admin.ConnectivityCheck{Name: name, Kind: kind}
	if destination.IsValid() {
		check.Address = destination.Addr().String()
	}
	if resolveErr != nil {
		check.Error = errorPrefix + " resolution failed"
		return check
	}
	if len(sources) == 0 {
		check.Error = "no outbound source available"
		return check
	}
	if err := t.dial(ctx, sources[0], destination); err != nil {
		check.Error = errorPrefix + " connection failed"
		return check
	}
	check.Success = true
	return check
}

func (t *ConnectivityTester) dial(ctx context.Context, source connectivitySource, destination netip.AddrPort) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	connector, err := t.connector(source.template)
	if err != nil || connector == nil {
		return errors.New("connector unavailable")
	}
	connection, err := connector.DialContext(ctx, "tcp6", destination, source.address, t.timeout)
	if err != nil {
		return err
	}
	if connection == nil {
		return errors.New("connector returned nil connection")
	}
	return connection.Close()
}

func connectivitySources(store *ipv6resource.Store) ([]connectivitySource, error) {
	templates := make(map[string]ipv6resource.PrefixTemplate)
	for _, template := range store.Templates() {
		templates[template.Name] = template
	}
	sources := make([]connectivitySource, 0)
	for _, fixed := range store.FixedAddresses() {
		template, exists := templates[fixed.Template]
		if !exists {
			return nil, fmt.Errorf("fixed outbound %q references missing template", fixed.Name)
		}
		sources = append(sources, connectivitySource{name: fixed.Name, kind: "fixed-outbound", address: fixed.Address, template: template})
	}
	for _, pool := range store.Pools() {
		if pool.Kind == ipv6resource.PoolInbound {
			continue
		}
		if len(pool.Active) == 0 {
			return nil, fmt.Errorf("outbound pool %q has no active address", pool.Name)
		}
		template, exists := templates[pool.Template]
		if !exists {
			return nil, fmt.Errorf("outbound pool %q references missing template", pool.Name)
		}
		sources = append(sources, connectivitySource{name: pool.Name, kind: "pool-outbound", address: pool.Active[0], template: template})
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].kind != sources[j].kind {
			return sources[i].kind < sources[j].kind
		}
		return sources[i].name < sources[j].name
	})
	return sources, nil
}

var _ interface {
	Test(context.Context) ([]admin.ConnectivityCheck, error)
} = (*ConnectivityTester)(nil)
