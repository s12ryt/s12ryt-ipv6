package app

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/config"
	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
)

func TestPolicyProviderRefreshesHostAndManagedAddresses(t *testing.T) {
	hosts := []netip.Addr{
		netip.MustParseAddr("127.0.0.1"),
		netip.MustParseAddr("2001:4860::10"),
		netip.MustParseAddr("::ffff:192.0.2.10"),
	}
	configuration := config.Default()
	configuration.AllowULA = true
	nat64 := netip.MustParsePrefix("64:ff9b::/96")
	provider, err := NewPolicyProvider(PolicyProviderOptions{
		ScanHostAddresses: func() ([]netip.Addr, error) {
			return append([]netip.Addr(nil), hosts...), nil
		},
		Configuration: func() config.Config { return configuration },
		NAT64Prefix:   func() netip.Prefix { return nat64 },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.RefreshHostAddresses(); err != nil {
		t.Fatal(err)
	}

	store := ipv6resource.NewStore()
	template, err := ipv6resource.NewPrefixTemplate("wan", "2001:4860:100::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddTemplate(template); err != nil {
		t.Fatal(err)
	}
	managed := netip.MustParseAddr("2001:4860:100::1")
	if _, err := store.CreateFixedAddress("fixed", "wan", managed, ipv6resource.OwnershipAddress); err != nil {
		t.Fatal(err)
	}
	if err := provider.Sync(store.State()); err != nil {
		t.Fatal(err)
	}

	snapshot := provider.Policy()
	if !snapshot.AllowULA || snapshot.NAT64Prefix != nat64 {
		t.Fatalf("policy settings = %#v", snapshot)
	}
	for _, address := range []netip.Addr{
		netip.MustParseAddr("127.0.0.1"),
		netip.MustParseAddr("2001:4860::10"),
		netip.MustParseAddr("192.0.2.10"),
	} {
		if _, exists := snapshot.LocalAddresses[address]; !exists {
			t.Fatalf("local address %s is missing", address)
		}
	}
	if _, exists := snapshot.ManagedAddresses[managed]; !exists {
		t.Fatalf("managed address %s is missing", managed)
	}

	delete(snapshot.LocalAddresses, netip.MustParseAddr("127.0.0.1"))
	delete(snapshot.ManagedAddresses, managed)
	if next := provider.Policy(); len(next.LocalAddresses) != 3 || len(next.ManagedAddresses) != 1 {
		t.Fatalf("caller mutated provider state: %#v", next)
	}

	configuration.AllowULA = false
	nat64 = netip.Prefix{}
	next := provider.Policy()
	if next.AllowULA || next.NAT64Prefix.IsValid() {
		t.Fatalf("dynamic settings were not refreshed: %#v", next)
	}
}

func TestPolicyProviderKeepsPreviousSnapshotWhenRefreshFails(t *testing.T) {
	wantErr := errors.New("interface scan failed")
	fail := false
	provider, err := NewPolicyProvider(PolicyProviderOptions{
		ScanHostAddresses: func() ([]netip.Addr, error) {
			if fail {
				return nil, wantErr
			}
			return []netip.Addr{netip.MustParseAddr("2001:4860::20")}, nil
		},
		Configuration: config.Default,
		NAT64Prefix:   func() netip.Prefix { return netip.Prefix{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.RefreshHostAddresses(); err != nil {
		t.Fatal(err)
	}
	fail = true
	if err := provider.RefreshHostAddresses(); !errors.Is(err, wantErr) {
		t.Fatalf("RefreshHostAddresses() error = %v", err)
	}
	if _, exists := provider.Policy().LocalAddresses[netip.MustParseAddr("2001:4860::20")]; !exists {
		t.Fatal("failed refresh replaced the last valid host snapshot")
	}
}

func TestPolicyProviderRejectsInvalidUpdatesAndDependencies(t *testing.T) {
	valid := PolicyProviderOptions{
		ScanHostAddresses: func() ([]netip.Addr, error) { return nil, nil },
		Configuration:     config.Default,
		NAT64Prefix:       func() netip.Prefix { return netip.Prefix{} },
	}
	invalid := []PolicyProviderOptions{
		{Configuration: valid.Configuration, NAT64Prefix: valid.NAT64Prefix},
		{ScanHostAddresses: valid.ScanHostAddresses, NAT64Prefix: valid.NAT64Prefix},
		{ScanHostAddresses: valid.ScanHostAddresses, Configuration: valid.Configuration},
	}
	for _, options := range invalid {
		if _, err := NewPolicyProvider(options); err == nil {
			t.Fatalf("NewPolicyProvider(%#v) error = nil", options)
		}
	}

	provider, err := NewPolicyProvider(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Sync(ipv6resource.State{Addresses: []ipv6resource.CanonicalAddress{{
		Address: netip.MustParseAddr("2001:4860::30"), References: 1,
	}}}); err == nil {
		t.Fatal("Sync(invalid state) error = nil")
	}
	if len(provider.Policy().ManagedAddresses) != 0 {
		t.Fatal("invalid state mutated managed addresses")
	}
}
