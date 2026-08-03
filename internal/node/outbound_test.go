package node

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/dns64"
	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type outboundResolver struct{}

func (outboundResolver) Resolve(context.Context, string, policy.DestinationPolicy, policy.ULAOverride, netip.Prefix) (dns64.Resolution, error) {
	return dns64.Resolution{Addresses: []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111")}, Source: "test"}, nil
}

type outboundConnector struct {
	mu      sync.Mutex
	sources []netip.Addr
}

func (c *outboundConnector) DialContext(_ context.Context, _ string, _ netip.AddrPort, source netip.Addr, _ time.Duration) (net.Conn, error) {
	c.mu.Lock()
	c.sources = append(c.sources, source)
	c.mu.Unlock()
	client, server := net.Pipe()
	go func() {
		_, _ = io.Copy(io.Discard, server)
		_ = server.Close()
	}()
	return client, nil
}

func (c *outboundConnector) Sources() []netip.Addr {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]netip.Addr(nil), c.sources...)
}

func outboundResourceStore(t *testing.T, poolCapacity int) *ipv6resource.Store {
	t.Helper()
	store := ipv6resource.NewStore()
	template, err := ipv6resource.NewPrefixTemplate("wan", "2001:4860:100::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddTemplate(template); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateFixedAddress("fixed-primary", "wan", netip.MustParseAddr("2001:4860:100::1"), ipv6resource.OwnershipAddress); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool("shared-primary", ipv6resource.PoolSharedOutbound, "wan", poolCapacity, nil); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestOutboundRegistryBuildsFixedAndRoundRobinPoolDialers(t *testing.T) {
	connector := &outboundConnector{}
	registry, err := NewOutboundRegistry(OutboundRegistryOptions{
		Resolver: outboundResolver{}, Policy: func() policy.DestinationPolicy { return policy.DestinationPolicy{} },
		NAT64Prefix: func() netip.Prefix { return netip.Prefix{} },
		Connector: func(template ipv6resource.PrefixTemplate) (proxy.Connector, error) {
			if template.Name != "wan" {
				t.Fatalf("connector template = %q", template.Name)
			}
			return connector, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Sync(outboundResourceStore(t, 2).State()); err != nil {
		t.Fatal(err)
	}

	fixedConfig := validConfig("fixed", "fixed")
	fixedConfig.Outbound = "fixed-primary"
	fixedDialer, err := registry.BuildDialer(fixedConfig)
	if err != nil {
		t.Fatal(err)
	}
	fixedConn, _, err := fixedDialer.Dial(context.Background(), "tcp", "example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	_ = fixedConn.Close()

	poolConfig := validConfig("pool", "pool")
	poolConfig.Outbound = "shared-primary"
	poolDialer, err := registry.BuildDialer(poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		conn, _, dialErr := poolDialer.Dial(context.Background(), "tcp", "example.com", 443)
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		_ = conn.Close()
	}
	want := []netip.Addr{
		netip.MustParseAddr("2001:4860:100::1"),
		netip.MustParseAddr("2001:4860:100::"),
		netip.MustParseAddr("2001:4860:100::2"),
	}
	if got := connector.Sources(); !reflect.DeepEqual(got, want) {
		t.Fatalf("dial sources = %v, want %v", got, want)
	}
}

func TestOutboundRegistryRefreshesExistingDialersAndDrainsOldLeases(t *testing.T) {
	connector := &outboundConnector{}
	drained := make(chan netip.Addr, 2)
	registry, err := NewOutboundRegistry(OutboundRegistryOptions{
		Resolver: outboundResolver{}, Policy: func() policy.DestinationPolicy { return policy.DestinationPolicy{} },
		NAT64Prefix: func() netip.Prefix { return netip.Prefix{} },
		Connector:   func(ipv6resource.PrefixTemplate) (proxy.Connector, error) { return connector, nil },
		OnDrained: func(resource string, address netip.Addr) {
			if resource != "shared-primary" {
				t.Errorf("drained resource = %q", resource)
			}
			drained <- address
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := outboundResourceStore(t, 1)
	if err := registry.Sync(store.State()); err != nil {
		t.Fatal(err)
	}
	config := validConfig("pool", "pool")
	config.Outbound = "shared-primary"
	dialer, err := registry.BuildDialer(config)
	if err != nil {
		t.Fatal(err)
	}
	oldConn, oldMetadata, err := dialer.Dial(context.Background(), "tcp", "example.com", 443)
	if err != nil {
		t.Fatal(err)
	}

	refreshed, err := store.RefreshPool("shared-primary")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Sync(store.State()); err != nil {
		t.Fatal(err)
	}
	newConn, newMetadata, err := dialer.Dial(context.Background(), "tcp", "example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	if newMetadata.Source != refreshed.Active[0] {
		t.Fatalf("new source = %s, want %s", newMetadata.Source, refreshed.Active[0])
	}
	select {
	case address := <-drained:
		t.Fatalf("source %s drained before old connection closed", address)
	default:
	}
	_ = oldConn.Close()
	select {
	case address := <-drained:
		if address != oldMetadata.Source {
			t.Fatalf("drained address = %s, want %s", address, oldMetadata.Source)
		}
	case <-time.After(time.Second):
		t.Fatal("old source was not reported drained")
	}
	_ = newConn.Close()
}

func TestOutboundRegistryForceDrainsNamedPoolConnections(t *testing.T) {
	connector := &outboundConnector{}
	drained := make(chan netip.Addr, 1)
	registry, err := NewOutboundRegistry(OutboundRegistryOptions{
		Resolver: outboundResolver{}, Policy: func() policy.DestinationPolicy { return policy.DestinationPolicy{} },
		NAT64Prefix: func() netip.Prefix { return netip.Prefix{} },
		Connector:   func(ipv6resource.PrefixTemplate) (proxy.Connector, error) { return connector, nil },
		OnDrained:   func(_ string, address netip.Addr) { drained <- address },
	})
	if err != nil {
		t.Fatal(err)
	}
	store := outboundResourceStore(t, 1)
	if err := registry.Sync(store.State()); err != nil {
		t.Fatal(err)
	}
	config := validConfig("pool", "pool")
	config.Outbound = "shared-primary"
	dialer, err := registry.BuildDialer(config)
	if err != nil {
		t.Fatal(err)
	}
	connection, metadata, err := dialer.Dial(context.Background(), "tcp", "example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RefreshPool("shared-primary"); err != nil {
		t.Fatal(err)
	}
	if err := registry.Sync(store.State()); err != nil {
		t.Fatal(err)
	}

	if err := registry.ForceDrain("shared-primary", []netip.Addr{metadata.Source}); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := connection.Read(make([]byte, 1)); err == nil {
		t.Fatal("forced outbound connection remained open")
	}
	select {
	case address := <-drained:
		if address != metadata.Source {
			t.Fatalf("drained address = %s, want %s", address, metadata.Source)
		}
	case <-time.After(time.Second):
		t.Fatal("forced outbound source was not reported drained")
	}
	if err := registry.ForceDrain("shared-primary", []netip.Addr{metadata.Source}); err != nil {
		t.Fatalf("ForceDrain(already completed) = %v", err)
	}
	if err := registry.ForceDrain("missing", []netip.Addr{metadata.Source}); err == nil {
		t.Fatal("ForceDrain(missing pool) error = nil")
	}
}

func TestOutboundRegistryRejectsInvalidResourcesAndDependencies(t *testing.T) {
	valid := OutboundRegistryOptions{
		Resolver: outboundResolver{}, Policy: func() policy.DestinationPolicy { return policy.DestinationPolicy{} },
		NAT64Prefix: func() netip.Prefix { return netip.Prefix{} },
		Connector:   func(ipv6resource.PrefixTemplate) (proxy.Connector, error) { return &outboundConnector{}, nil },
	}
	invalid := []OutboundRegistryOptions{
		{},
		{Resolver: outboundResolver{}},
		{Resolver: outboundResolver{}, Policy: valid.Policy, NAT64Prefix: valid.NAT64Prefix},
	}
	for _, options := range invalid {
		if _, err := NewOutboundRegistry(options); err == nil {
			t.Fatalf("NewOutboundRegistry(%#v) error = nil", options)
		}
	}
	registry, _ := NewOutboundRegistry(valid)
	store := outboundResourceStore(t, 2)
	if _, err := store.CreatePool("inbound-only", ipv6resource.PoolInbound, "wan", 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := registry.Sync(store.State()); err != nil {
		t.Fatal(err)
	}
	config := validConfig("node", "node")
	config.Outbound = "inbound-only"
	if _, err := registry.BuildDialer(config); err == nil {
		t.Fatal("BuildDialer(inbound pool) error = nil")
	}
	config.Outbound = "missing"
	if _, err := registry.BuildDialer(config); err == nil {
		t.Fatal("BuildDialer(missing) error = nil")
	}

	ambiguous := outboundResourceStore(t, 2).State()
	ambiguous.Fixed = append(ambiguous.Fixed, ipv6resource.FixedAddress{
		Name: "shared-primary", Template: "wan", Address: netip.MustParseAddr("2001:4860:100::30"),
		Ownership: ipv6resource.OwnershipAddress,
	})
	ambiguous.Addresses = append(ambiguous.Addresses, ipv6resource.CanonicalAddress{
		Address: netip.MustParseAddr("2001:4860:100::30"), Template: "wan",
		Ownership: ipv6resource.OwnershipAddress, References: 1,
	})
	if err := registry.Sync(ambiguous); !errors.Is(err, ErrAmbiguousOutbound) {
		t.Fatalf("Sync(ambiguous) error = %v", err)
	}
}
