package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/dns64"
	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type connectivityQueryer struct {
	fail map[string]error
}

func (q connectivityQueryer) Query(_ context.Context, endpoint dns64.Endpoint, _ string, _ dns64.RecordType) (dns64.QueryResult, error) {
	if err := q.fail[endpoint.Name]; err != nil {
		return dns64.QueryResult{}, err
	}
	return dns64.QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("64:ff9b::c000:aa")}, TTL: time.Minute}, nil
}

type connectivityResolver struct {
	failNative bool
	failNAT64  bool
}

func (r connectivityResolver) Resolve(_ context.Context, host string, _ policy.DestinationPolicy, _ policy.ULAOverride, _ netip.Prefix) (dns64.Resolution, error) {
	switch host {
	case connectivityNativeHost:
		if r.failNative {
			return dns64.Resolution{}, errors.New("secret native resolver detail")
		}
		return dns64.Resolution{Addresses: []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111")}, Source: "cloudflare"}, nil
	case connectivityNAT64IPv4:
		if r.failNAT64 {
			return dns64.Resolution{}, errors.New("secret NAT64 resolver detail")
		}
		return dns64.Resolution{Addresses: []netip.Addr{netip.MustParseAddr("64:ff9b::101:101")}, Source: "cloudflare", Synthesized: true}, nil
	default:
		return dns64.Resolution{}, errors.New("unexpected host")
	}
}

type connectivityConnector struct {
	mu      sync.Mutex
	dials   []connectivityDial
	failFor map[netip.Addr]error
}

type connectivityDial struct {
	template    string
	network     string
	destination netip.AddrPort
	source      netip.Addr
}

func (c *connectivityConnector) forTemplate(template ipv6resource.PrefixTemplate) proxy.Connector {
	return connectorFunc(func(_ context.Context, network string, destination netip.AddrPort, source netip.Addr, _ time.Duration) (net.Conn, error) {
		c.mu.Lock()
		c.dials = append(c.dials, connectivityDial{template: template.Name, network: network, destination: destination, source: source})
		err := c.failFor[source]
		c.mu.Unlock()
		if err != nil {
			return nil, err
		}
		client, server := net.Pipe()
		go func() {
			_, _ = io.Copy(io.Discard, server)
			_ = server.Close()
		}()
		return client, nil
	})
}

type connectorFunc func(context.Context, string, netip.AddrPort, netip.Addr, time.Duration) (net.Conn, error)

func (f connectorFunc) DialContext(ctx context.Context, network string, destination netip.AddrPort, source netip.Addr, timeout time.Duration) (net.Conn, error) {
	return f(ctx, network, destination, source, timeout)
}

func connectivityState(t *testing.T) ipv6resource.State {
	t.Helper()
	store := ipv6resource.NewStore()
	template, err := ipv6resource.NewPrefixTemplate("wan", "2001:4860:300::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddTemplate(template); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateFixedAddress("fixed-z", "wan", netip.MustParseAddr("2001:4860:300::10"), ipv6resource.OwnershipAddress); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateFixedAddress("fixed-a", "wan", netip.MustParseAddr("2001:4860:300::11"), ipv6resource.OwnershipAddress); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool("pool-z", ipv6resource.PoolSharedOutbound, "wan", 1, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool("pool-a", ipv6resource.PoolDedicatedOutbound, "wan", 1, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool("inbound-only", ipv6resource.PoolInbound, "wan", 1, nil); err != nil {
		t.Fatal(err)
	}
	return store.State()
}

func TestConnectivityTesterChecksDoTNativeNAT64AndEveryOutboundResource(t *testing.T) {
	state := connectivityState(t)
	connector := &connectivityConnector{}
	tester, err := NewConnectivityTester(ConnectivityTesterOptions{
		Queryer: connectivityQueryer{}, Resolver: connectivityResolver{},
		Endpoints: func() []dns64.Endpoint {
			return []dns64.Endpoint{
				{Name: "cloudflare", Address: netip.MustParseAddr("2606:4700:4700::64"), Port: 853, ServerName: "cloudflare-dns.com"},
				{Name: "google", Address: netip.MustParseAddr("2001:4860:4860::6464"), Port: 853, ServerName: "dns.google"},
			}
		},
		Resources:   func() ipv6resource.State { return state },
		Policy:      func() policy.DestinationPolicy { return policy.DestinationPolicy{} },
		NAT64Prefix: func() netip.Prefix { return netip.MustParsePrefix("64:ff9b::/96") },
		Connector: func(template ipv6resource.PrefixTemplate) (proxy.Connector, error) {
			return connector.forTemplate(template), nil
		},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	checks, err := tester.Test(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(checks))
	for _, check := range checks {
		if !check.Success || check.Error != "" {
			t.Fatalf("check = %#v, want success", check)
		}
		got = append(got, check.Kind+":"+check.Name)
	}
	want := []string{
		"dot:cloudflare", "dot:google", "native-ipv6:原生 IPv6", "nat64:NAT64",
		"fixed-outbound:fixed-a", "fixed-outbound:fixed-z", "pool-outbound:pool-a", "pool-outbound:pool-z",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("checks = %v, want %v", got, want)
	}
	for _, dial := range connector.dials {
		if dial.network != "tcp6" || !dial.destination.Addr().Is6() || dial.source.Is4() {
			t.Fatalf("dial escaped IPv6-only path: %#v", dial)
		}
	}
	if len(connector.dials) != 6 {
		t.Fatalf("dial count = %d, want native + NAT64 + four resources", len(connector.dials))
	}
}

func TestConnectivityTesterKeepsIndividualFailuresSanitized(t *testing.T) {
	state := connectivityState(t)
	failingSource := state.Fixed[0].Address
	connector := &connectivityConnector{failFor: map[netip.Addr]error{failingSource: errors.New("secret upstream detail")}}
	tester, err := NewConnectivityTester(ConnectivityTesterOptions{
		Queryer:  connectivityQueryer{fail: map[string]error{"cloudflare": errors.New("secret TLS detail")}},
		Resolver: connectivityResolver{failNAT64: true},
		Endpoints: func() []dns64.Endpoint {
			return []dns64.Endpoint{{Name: "cloudflare", Address: netip.MustParseAddr("2606:4700:4700::64"), Port: 853, ServerName: "cloudflare-dns.com"}}
		},
		Resources:   func() ipv6resource.State { return state },
		Policy:      func() policy.DestinationPolicy { return policy.DestinationPolicy{} },
		NAT64Prefix: func() netip.Prefix { return netip.MustParsePrefix("64:ff9b::/96") },
		Connector: func(template ipv6resource.PrefixTemplate) (proxy.Connector, error) {
			return connector.forTemplate(template), nil
		},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	checks, err := tester.Test(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, check := range checks {
		joined += check.Error + "\n"
	}
	if strings.Contains(joined, "secret") {
		t.Fatalf("connectivity errors leaked internals: %q", joined)
	}
	if checks[0].Success || checks[0].Error != "DoT query failed" {
		t.Fatalf("DoT failure = %#v", checks[0])
	}
	if checks[2].Success || checks[2].Error != "NAT64 resolution failed" {
		t.Fatalf("NAT64 failure = %#v", checks[2])
	}
}

func TestConnectivityTesterRejectsInvalidStateAndDependencies(t *testing.T) {
	valid := ConnectivityTesterOptions{
		Queryer: connectivityQueryer{}, Resolver: connectivityResolver{},
		Endpoints: func() []dns64.Endpoint {
			return []dns64.Endpoint{{Name: "cloudflare", Address: netip.MustParseAddr("2606:4700:4700::64"), Port: 853, ServerName: "cloudflare-dns.com"}}
		},
		Resources:   func() ipv6resource.State { return connectivityState(t) },
		Policy:      func() policy.DestinationPolicy { return policy.DestinationPolicy{} },
		NAT64Prefix: func() netip.Prefix { return netip.MustParsePrefix("64:ff9b::/96") },
		Connector: func(ipv6resource.PrefixTemplate) (proxy.Connector, error) {
			return (&connectivityConnector{}).forTemplate(ipv6resource.PrefixTemplate{}), nil
		},
		Timeout: time.Second,
	}
	invalid := []ConnectivityTesterOptions{
		{},
		{Queryer: valid.Queryer, Resolver: valid.Resolver},
		{Queryer: valid.Queryer, Resolver: valid.Resolver, Endpoints: valid.Endpoints, Resources: valid.Resources, Policy: valid.Policy, NAT64Prefix: valid.NAT64Prefix, Connector: valid.Connector},
	}
	for _, options := range invalid {
		if _, err := NewConnectivityTester(options); err == nil {
			t.Fatalf("NewConnectivityTester(%#v) error = nil", options)
		}
	}

	tester, err := NewConnectivityTester(valid)
	if err != nil {
		t.Fatal(err)
	}
	broken := connectivityState(t)
	broken.Templates = nil
	tester.resources = func() ipv6resource.State { return broken }
	if _, err := tester.Test(context.Background()); err == nil {
		t.Fatal("Test(invalid resource state) error = nil")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tester.Test(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Test(canceled) error = %v", err)
	}
}
