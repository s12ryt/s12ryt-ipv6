package node

import (
	"errors"
	"net/netip"
	"reflect"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

func inboundResourceState(t *testing.T) ipv6resource.State {
	t.Helper()
	store := ipv6resource.NewStore()
	addressTemplate, err := ipv6resource.NewPrefixTemplate(
		"addressed", "2001:4860:200::/120", "eth0", ipv6resource.ModeAddress,
	)
	if err != nil {
		t.Fatal(err)
	}
	freebindTemplate, err := ipv6resource.NewPrefixTemplate(
		"routed", "2001:4860:201::/120", "eth1", ipv6resource.ModeLocalRouteFreebind,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, template := range []ipv6resource.PrefixTemplate{addressTemplate, freebindTemplate} {
		if err := store.AddTemplate(template); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.CreateFixedAddress(
		"fixed-in", "addressed", netip.MustParseAddr("2001:4860:200::10"), ipv6resource.OwnershipAddress,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool("pool-in", ipv6resource.PoolInbound, "routed", 2, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool("pool-out", ipv6resource.PoolSharedOutbound, "addressed", 1, nil); err != nil {
		t.Fatal(err)
	}
	return store.State()
}

func TestInboundRegistryResolvesIPv4FixedAndPoolResources(t *testing.T) {
	registry, err := NewInboundRegistry()
	if err != nil {
		t.Fatal(err)
	}
	state := inboundResourceState(t)
	if err := registry.Sync(state); err != nil {
		t.Fatal(err)
	}

	ipv4 := validConfig("v4", "v4")
	ipv4.Inbound = nil
	ipv4.InboundMode = InboundIPv4
	resolved, err := registry.Resolve(ipv4)
	if err != nil {
		t.Fatal(err)
	}
	wantIPv4 := []proxy.BindSpec{{Protocol: proxy.BindTCP, Family: proxy.BindIPv4}}
	if !reflect.DeepEqual(resolved.Inbound, wantIPv4) {
		t.Fatalf("IPv4 inbound = %#v, want %#v", resolved.Inbound, wantIPv4)
	}

	fixed := validConfig("fixed", "fixed")
	fixed.Inbound = nil
	fixed.InboundMode = InboundIPv6
	fixed.InboundResource = "fixed-in"
	resolved, err = registry.Resolve(fixed)
	if err != nil {
		t.Fatal(err)
	}
	wantFixed := []proxy.BindSpec{{
		Protocol: proxy.BindTCP, Family: proxy.BindIPv6,
		Address: netip.MustParseAddr("2001:4860:200::10"), Interface: "eth0",
	}}
	if !reflect.DeepEqual(resolved.Inbound, wantFixed) {
		t.Fatalf("fixed inbound = %#v, want %#v", resolved.Inbound, wantFixed)
	}

	pool := validConfig("pool", "pool")
	pool.Inbound = nil
	pool.InboundMode = InboundDual
	pool.InboundResource = "pool-in"
	resolved, err = registry.Resolve(pool)
	if err != nil {
		t.Fatal(err)
	}
	wantPool := []proxy.BindSpec{{Protocol: proxy.BindTCP, Family: proxy.BindIPv4}}
	for _, address := range state.Pools[0].Active {
		wantPool = append(wantPool, proxy.BindSpec{
			Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: address,
			Interface: "eth1", Freebind: true,
		})
	}
	if !reflect.DeepEqual(resolved.Inbound, wantPool) {
		t.Fatalf("pool inbound = %#v, want %#v", resolved.Inbound, wantPool)
	}
	if len(pool.Inbound) != 0 {
		t.Fatal("Resolve mutated input config")
	}
}

func TestInboundRegistryRefreshesPoolResolution(t *testing.T) {
	registry, _ := NewInboundRegistry()
	state := inboundResourceState(t)
	if err := registry.Sync(state); err != nil {
		t.Fatal(err)
	}
	config := validConfig("pool", "pool")
	config.Inbound = nil
	config.InboundMode = InboundIPv6
	config.InboundResource = "pool-in"
	before, err := registry.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}

	store, err := ipv6resource.NewStoreFromState(state)
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := store.RefreshPool("pool-in")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Sync(store.State()); err != nil {
		t.Fatal(err)
	}
	after, err := registry.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(before.Inbound, after.Inbound) {
		t.Fatal("refreshed inbound pool kept stale listeners")
	}
	for index, binding := range after.Inbound {
		if binding.Address != refreshed.Active[index] {
			t.Fatalf("binding %d address = %s, want %s", index, binding.Address, refreshed.Active[index])
		}
	}
}

func TestInboundRegistryRejectsInvalidAndAmbiguousResources(t *testing.T) {
	registry, _ := NewInboundRegistry()
	state := inboundResourceState(t)
	if err := registry.Sync(state); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		mode     InboundMode
		resource string
	}{
		{name: "IPv4 with resource", mode: InboundIPv4, resource: "fixed-in"},
		{name: "IPv6 without resource", mode: InboundIPv6},
		{name: "missing resource", mode: InboundIPv6, resource: "missing"},
		{name: "outbound pool", mode: InboundIPv6, resource: "pool-out"},
		{name: "unsupported mode", mode: InboundMode("other"), resource: "fixed-in"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validConfig("node", "node")
			config.Inbound = nil
			config.InboundMode = test.mode
			config.InboundResource = test.resource
			if _, err := registry.Resolve(config); err == nil {
				t.Fatal("Resolve() error = nil")
			}
		})
	}

	ambiguous := state
	ambiguous.Fixed = append(ambiguous.Fixed, ipv6resource.FixedAddress{
		Name: "pool-in", Template: "addressed", Address: netip.MustParseAddr("2001:4860:200::20"),
		Ownership: ipv6resource.OwnershipAddress,
	})
	ambiguous.Addresses = append(ambiguous.Addresses, ipv6resource.CanonicalAddress{
		Address: netip.MustParseAddr("2001:4860:200::20"), Template: "addressed",
		Ownership: ipv6resource.OwnershipAddress, References: 1,
	})
	if err := registry.Sync(ambiguous); !errors.Is(err, ErrAmbiguousInbound) {
		t.Fatalf("Sync(ambiguous) error = %v", err)
	}
	if _, err := NewInboundRegistry(); err != nil {
		t.Fatalf("NewInboundRegistry() error = %v", err)
	}
}

func TestDeclarativeInboundConfigValidation(t *testing.T) {
	config := validConfig("node", "node")
	config.Inbound = nil
	config.InboundMode = InboundDual
	config.InboundResource = "pool-in"
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	config.InboundMode = InboundIPv4
	if err := config.Validate(); err == nil {
		t.Fatal("IPv4 declarative inbound accepted an IPv6 resource")
	}
	config.InboundResource = ""
	if err := config.Validate(); err != nil {
		t.Fatalf("IPv4 Validate() error = %v", err)
	}
	config.InboundMode = InboundMode("invalid")
	if err := config.Validate(); err == nil {
		t.Fatal("unsupported inbound mode was accepted")
	}
}
