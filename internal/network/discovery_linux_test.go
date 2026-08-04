//go:build linux

package network

import (
	"context"
	"net"
	"net/netip"
	"reflect"
	"testing"

	"github.com/vishvananda/netlink"
)

type fakeDiscoveryNetlinkDriver struct {
	links     []netlink.Link
	addresses map[int][]netlink.Addr
	routes    map[int][]netlink.Route
}

func (f *fakeDiscoveryNetlinkDriver) LinkList() ([]netlink.Link, error) {
	return append([]netlink.Link(nil), f.links...), nil
}

func (f *fakeDiscoveryNetlinkDriver) LinkByIndex(index int) (netlink.Link, error) {
	for _, link := range f.links {
		if link.Attrs().Index == index {
			return link, nil
		}
	}
	return nil, net.UnknownNetworkError("missing link")
}

func (f *fakeDiscoveryNetlinkDriver) AddrList(link netlink.Link, _ int) ([]netlink.Addr, error) {
	return append([]netlink.Addr(nil), f.addresses[link.Attrs().Index]...), nil
}

func (f *fakeDiscoveryNetlinkDriver) RouteList(link netlink.Link, _ int) ([]netlink.Route, error) {
	return append([]netlink.Route(nil), f.routes[link.Attrs().Index]...), nil
}

func TestLinuxNetworkDiscoveryConvertsNetlinkState(t *testing.T) {
	eth0 := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 7, Flags: net.FlagUp}}
	loopback := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "lo", Index: 1, Flags: net.FlagUp | net.FlagLoopback}}
	driver := &fakeDiscoveryNetlinkDriver{
		links: []netlink.Link{loopback, eth0},
		addresses: map[int][]netlink.Addr{
			7: {{IPNet: mustIPNet(t, "2001:4860:30::123/64")}},
		},
		routes: map[int][]netlink.Route{
			7: {{LinkIndex: 7, Dst: mustIPNet(t, "2001:4860:31::/64")}, {LinkIndex: 7, Dst: nil}},
		},
	}
	discovery, err := newLinuxNetworkDiscovery(driver)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := discovery.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := NetworkDiscoverySnapshot{
		Interfaces: []DiscoveredInterface{{Name: "eth0", Index: 7}},
		Prefixes: []DiscoveredPrefix{
			{Interface: "eth0", Prefix: netip.MustParsePrefix("2001:4860:30::/64"), Sources: []PrefixSource{PrefixSourceAddress}},
			{Interface: "eth0", Prefix: netip.MustParsePrefix("2001:4860:31::/64"), Sources: []PrefixSource{PrefixSourceRoute}},
		},
	}
	if !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("Discover() = %#v, want %#v", snapshot, want)
	}
}

func mustIPNet(t *testing.T, value string) *net.IPNet {
	t.Helper()
	prefix := netip.MustParsePrefix(value)
	return &net.IPNet{IP: net.IP(prefix.Addr().AsSlice()), Mask: net.CIDRMask(prefix.Bits(), 128)}
}
