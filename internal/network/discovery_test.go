package network

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"
)

type fakeDiscoveryDriver struct {
	links       []discoveryLink
	addresses   map[int][]netip.Prefix
	routes      map[int][]netip.Prefix
	linksErr    error
	addressErrs map[int]error
	routeErrs   map[int]error
}

func (f *fakeDiscoveryDriver) Links(context.Context) ([]discoveryLink, error) {
	return append([]discoveryLink(nil), f.links...), f.linksErr
}

func (f *fakeDiscoveryDriver) Addresses(_ context.Context, index int) ([]netip.Prefix, error) {
	return append([]netip.Prefix(nil), f.addresses[index]...), f.addressErrs[index]
}

func (f *fakeDiscoveryDriver) Routes(_ context.Context, index int) ([]netip.Prefix, error) {
	return append([]netip.Prefix(nil), f.routes[index]...), f.routeErrs[index]
}

func TestNetworkDiscoveryFiltersMergesAndSortsCandidates(t *testing.T) {
	driver := &fakeDiscoveryDriver{
		links: []discoveryLink{
			{Name: "eth2", Index: 12, Up: true},
			{Name: "lo", Index: 1, Up: true, Loopback: true},
			{Name: "eth0", Index: 10},
			{Name: "eth1", Index: 11, Up: true},
		},
		addresses: map[int][]netip.Prefix{
			11: {
				netip.MustParsePrefix("2001:4860:10::1234/64"),
				netip.MustParsePrefix("fd00::1/64"),
				netip.MustParsePrefix("192.0.2.1/24"),
			},
			12: {netip.MustParsePrefix("2001:4860:20::9/128")},
		},
		routes: map[int][]netip.Prefix{
			11: {
				netip.MustParsePrefix("::/0"),
				netip.MustParsePrefix("2001:4860:10::/64"),
				netip.MustParsePrefix("2001:4860:11::/64"),
			},
			12: {netip.MustParsePrefix("fe80::/64")},
		},
	}
	discovery, err := newNetworkDiscovery(driver)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := discovery.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantInterfaces := []DiscoveredInterface{{Name: "eth1", Index: 11}, {Name: "eth2", Index: 12}}
	if !reflect.DeepEqual(snapshot.Interfaces, wantInterfaces) {
		t.Fatalf("Interfaces = %#v, want %#v", snapshot.Interfaces, wantInterfaces)
	}
	wantPrefixes := []DiscoveredPrefix{
		{Interface: "eth1", Prefix: netip.MustParsePrefix("2001:4860:10::/64"), Sources: []PrefixSource{PrefixSourceAddress, PrefixSourceRoute}},
		{Interface: "eth1", Prefix: netip.MustParsePrefix("2001:4860:11::/64"), Sources: []PrefixSource{PrefixSourceRoute}},
		{Interface: "eth2", Prefix: netip.MustParsePrefix("2001:4860:20::9/128"), Sources: []PrefixSource{PrefixSourceAddress}},
	}
	if !reflect.DeepEqual(snapshot.Prefixes, wantPrefixes) {
		t.Fatalf("Prefixes = %#v, want %#v", snapshot.Prefixes, wantPrefixes)
	}

	snapshot.Interfaces[0].Name = "changed"
	snapshot.Prefixes[0].Sources[0] = "changed"
	again, err := discovery.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again.Interfaces[0].Name != "eth1" || again.Prefixes[0].Sources[0] != PrefixSourceAddress {
		t.Fatalf("Discover() returned aliased data: %#v", again)
	}
}

func TestNetworkDiscoveryReturnsInterfaceScopedErrorsWithoutPartialResults(t *testing.T) {
	driver := &fakeDiscoveryDriver{
		links:       []discoveryLink{{Name: "eth0", Index: 7, Up: true}},
		addresses:   map[int][]netip.Prefix{},
		routes:      map[int][]netip.Prefix{},
		addressErrs: map[int]error{7: errors.New("secret netlink detail")},
	}
	discovery, err := newNetworkDiscovery(driver)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := discovery.Discover(context.Background()); err == nil {
		t.Fatal("Discover() error = nil, want address listing error")
	}

	driver.addressErrs = map[int]error{}
	driver.routeErrs = map[int]error{7: errors.New("route unavailable")}
	if _, err := discovery.Discover(context.Background()); err == nil {
		t.Fatal("Discover() error = nil, want route listing error")
	}
}

func TestNetworkDiscoveryValidatesDependenciesAndContext(t *testing.T) {
	if _, err := newNetworkDiscovery(nil); err == nil {
		t.Fatal("newNetworkDiscovery(nil) error = nil")
	}
	discovery, err := newNetworkDiscovery(&fakeDiscoveryDriver{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discovery.Discover(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Discover() error = %v, want context.Canceled", err)
	}
}
