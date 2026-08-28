//go:build linux

package network

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type fakeNetlinkDriver struct {
	link           netlink.Link
	addresses      []netlink.Addr
	addressLists   [][]netlink.Addr
	addrListCalls  int
	addrListErr    error
	addedAddress   *netlink.Addr
	deletedAddress *netlink.Addr
	routes         []netlink.Route
	addedRoute     *netlink.Route
	deletedRoute   *netlink.Route
}

func (f *fakeNetlinkDriver) LinkByName(string) (netlink.Link, error) { return f.link, nil }

func (f *fakeNetlinkDriver) AddrList(netlink.Link, int) ([]netlink.Addr, error) {
	f.addrListCalls++
	if f.addrListErr != nil {
		return nil, f.addrListErr
	}
	if len(f.addressLists) > 0 {
		result := f.addressLists[0]
		f.addressLists = f.addressLists[1:]
		return result, nil
	}
	return f.addresses, nil
}

func (f *fakeNetlinkDriver) AddrAdd(_ netlink.Link, addr *netlink.Addr) error {
	copy := *addr
	f.addedAddress = &copy
	return nil
}

func (f *fakeNetlinkDriver) AddrDel(_ netlink.Link, addr *netlink.Addr) error {
	copy := *addr
	f.deletedAddress = &copy
	return nil
}

func (f *fakeNetlinkDriver) RouteListFiltered(int, *netlink.Route, uint64) ([]netlink.Route, error) {
	return f.routes, nil
}

func (f *fakeNetlinkDriver) RouteAdd(route *netlink.Route) error {
	copy := *route
	f.addedRoute = &copy
	return nil
}

func (f *fakeNetlinkDriver) RouteDel(route *netlink.Route) error {
	copy := *route
	f.deletedRoute = &copy
	return nil
}

type fakeBindValidator struct {
	ref      AddressRef
	freebind bool
	err      error
}

func (f *fakeBindValidator) Validate(_ context.Context, ref AddressRef, freebind bool) error {
	f.ref = ref
	f.freebind = freebind
	return f.err
}

func TestLinuxKernelAddsAndDeletesIPv6AddressAs128(t *testing.T) {
	driver := &fakeNetlinkDriver{link: &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 7}}}
	kernel := newLinuxKernel(driver, &fakeBindValidator{}, time.Millisecond)
	ref := AddressRef{Interface: "eth0", Address: netip.MustParseAddr("2001:4860:1::12")}
	if err := kernel.AddAddress(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if driver.addedAddress == nil || driver.addedAddress.IPNet.String() != "2001:4860:1::12/128" {
		t.Fatalf("added address = %#v, want /128", driver.addedAddress)
	}
	if err := kernel.RemoveAddress(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if driver.deletedAddress == nil || driver.deletedAddress.IPNet.String() != "2001:4860:1::12/128" {
		t.Fatalf("deleted address = %#v, want /128", driver.deletedAddress)
	}
}

func TestLinuxKernelWaitsUntilDADReadyAndRejectsDADFailure(t *testing.T) {
	ref := AddressRef{Interface: "eth0", Address: netip.MustParseAddr("2001:4860:1::12")}
	ipnet := &net.IPNet{IP: net.IP(ref.Address.AsSlice()), Mask: net.CIDRMask(128, 128)}
	driver := &fakeNetlinkDriver{
		link: &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 7}},
		addressLists: [][]netlink.Addr{
			{{IPNet: ipnet, Flags: unix.IFA_F_TENTATIVE}},
			{{IPNet: ipnet}},
		},
	}
	kernel := newLinuxKernel(driver, &fakeBindValidator{}, time.Millisecond)
	if err := kernel.WaitAddressReady(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	driver.addressLists = [][]netlink.Addr{{{IPNet: ipnet, Flags: unix.IFA_F_DADFAILED}}}
	if err := kernel.WaitAddressReady(context.Background(), ref); !errors.Is(err, ErrDADFailed) {
		t.Fatalf("WaitAddressReady() error = %v, want ErrDADFailed", err)
	}
}

func TestLinuxKernelInterfaceAddressesReturnsAddresses(t *testing.T) {
	ip1 := &net.IPNet{IP: net.IP(netip.MustParseAddr("2001:4860:1::1").AsSlice()), Mask: net.CIDRMask(128, 128)}
	ip2 := &net.IPNet{IP: net.IP(netip.MustParseAddr("2001:4860:1::2").AsSlice()), Mask: net.CIDRMask(128, 128)}
	driver := &fakeNetlinkDriver{
		link:      &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 7}},
		addresses: []netlink.Addr{{IPNet: ip1}, {IPNet: ip2}},
	}
	kernel := newLinuxKernel(driver, &fakeBindValidator{}, time.Millisecond)

	addresses, err := kernel.InterfaceAddresses(context.Background(), "eth0")
	if err != nil {
		t.Fatal(err)
	}
	want1 := netip.MustParseAddr("2001:4860:1::1")
	want2 := netip.MustParseAddr("2001:4860:1::2")
	if len(addresses) != 2 || addresses[0] != want1 || addresses[1] != want2 {
		t.Fatalf("InterfaceAddresses() = %v, want [%s %s]", addresses, want1, want2)
	}

	driver.addrListErr = errors.New("netlink down")
	if _, err := kernel.InterfaceAddresses(context.Background(), "eth0"); err == nil || !strings.Contains(err.Error(), "list IPv6 addresses on eth0") {
		t.Fatalf("InterfaceAddresses() error = %v, want wrapped list error", err)
	}
}

func TestLinuxKernelWaitAddressesReadyPollsInterfaceOncePerInterval(t *testing.T) {
	ip1 := &net.IPNet{IP: net.IP(netip.MustParseAddr("2001:4860:1::1").AsSlice()), Mask: net.CIDRMask(128, 128)}
	ip2 := &net.IPNet{IP: net.IP(netip.MustParseAddr("2001:4860:1::2").AsSlice()), Mask: net.CIDRMask(128, 128)}
	driver := &fakeNetlinkDriver{
		link: &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 7}},
		addressLists: [][]netlink.Addr{
			{{IPNet: ip1, Flags: unix.IFA_F_TENTATIVE}, {IPNet: ip2, Flags: unix.IFA_F_TENTATIVE}},
			{{IPNet: ip1}, {IPNet: ip2}},
		},
	}
	kernel := newLinuxKernel(driver, &fakeBindValidator{}, time.Millisecond)
	refs := []AddressRef{
		{Interface: "eth0", Address: netip.MustParseAddr("2001:4860:1::1")},
		{Interface: "eth0", Address: netip.MustParseAddr("2001:4860:1::2")},
	}

	if err := kernel.WaitAddressesReady(context.Background(), refs); err != nil {
		t.Fatal(err)
	}

	if driver.addrListCalls != 2 {
		t.Fatalf("AddrList calls = %d, want 2 (single shared poller, not one per address)", driver.addrListCalls)
	}
}

func TestLinuxKernelWaitAddressesReadyAggregatesDADFailure(t *testing.T) {
	ip1 := &net.IPNet{IP: net.IP(netip.MustParseAddr("2001:4860:1::1").AsSlice()), Mask: net.CIDRMask(128, 128)}
	ip2 := &net.IPNet{IP: net.IP(netip.MustParseAddr("2001:4860:1::2").AsSlice()), Mask: net.CIDRMask(128, 128)}
	ip3 := &net.IPNet{IP: net.IP(netip.MustParseAddr("2001:4860:1::3").AsSlice()), Mask: net.CIDRMask(128, 128)}
	driver := &fakeNetlinkDriver{
		link: &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 7}},
		addressLists: [][]netlink.Addr{
			{{IPNet: ip1}, {IPNet: ip2, Flags: unix.IFA_F_DADFAILED}, {IPNet: ip3, Flags: unix.IFA_F_TENTATIVE}},
		},
	}
	kernel := newLinuxKernel(driver, &fakeBindValidator{}, time.Millisecond)
	refs := []AddressRef{
		{Interface: "eth0", Address: netip.MustParseAddr("2001:4860:1::1")},
		{Interface: "eth0", Address: netip.MustParseAddr("2001:4860:1::2")},
		{Interface: "eth0", Address: netip.MustParseAddr("2001:4860:1::3")},
	}

	err := kernel.WaitAddressesReady(context.Background(), refs)
	if err == nil {
		t.Fatal("WaitAddressesReady() error = nil, want DAD failure aggregation")
	}
	if !errors.Is(err, ErrDADFailed) {
		t.Fatalf("WaitAddressesReady() error = %v, want ErrDADFailed", err)
	}
	message := err.Error()
	if !strings.Contains(message, "wait for address 2001:4860:1::2 DAD") || !strings.Contains(message, "2001:4860:1::2 on eth0") {
		t.Fatalf("error = %q, want failed address wrapped per ref", message)
	}
	if !strings.Contains(message, "wait for address 2001:4860:1::3 DAD") || !strings.Contains(message, "context canceled") {
		t.Fatalf("error = %q, want pending addresses reported as canceled", message)
	}
	if strings.Contains(message, "wait for address 2001:4860:1::1 DAD") {
		t.Fatalf("error = %q, ready address must not be reported as failed", message)
	}
}

func TestLinuxKernelWaitAddressesReadyPropagatesListError(t *testing.T) {
	driver := &fakeNetlinkDriver{
		link:        &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 7}},
		addrListErr: errors.New("netlink down"),
	}
	kernel := newLinuxKernel(driver, &fakeBindValidator{}, time.Millisecond)
	refs := []AddressRef{
		{Interface: "eth0", Address: netip.MustParseAddr("2001:4860:1::1")},
		{Interface: "eth0", Address: netip.MustParseAddr("2001:4860:1::2")},
	}

	err := kernel.WaitAddressesReady(context.Background(), refs)
	if err == nil {
		t.Fatal("WaitAddressesReady() error = nil, want list error propagation")
	}
	message := err.Error()
	if !strings.Contains(message, "wait for address 2001:4860:1::1 DAD") || !strings.Contains(message, "list IPv6 addresses during DAD on eth0") {
		t.Fatalf("error = %q, want per-ref wrapped list error", message)
	}
	if !strings.Contains(message, "wait for address 2001:4860:1::2 DAD") {
		t.Fatalf("error = %q, want every waiting address reported", message)
	}
}

func TestLinuxKernelBuildsOwnedLocalRouteAndDelegatesFreebind(t *testing.T) {
	driver := &fakeNetlinkDriver{link: &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 7}}}
	binder := &fakeBindValidator{}
	kernel := newLinuxKernel(driver, binder, time.Millisecond)
	route := RouteRef{Interface: "eth0", Prefix: netip.MustParsePrefix("2001:4860:1::/64")}
	if err := kernel.AddLocalRoute(context.Background(), route); err != nil {
		t.Fatal(err)
	}
	if driver.addedRoute == nil || driver.addedRoute.LinkIndex != 7 || driver.addedRoute.Table != unix.RT_TABLE_LOCAL || driver.addedRoute.Type != unix.RTN_LOCAL || driver.addedRoute.Dst.String() != route.Prefix.String() {
		t.Fatalf("added route = %#v", driver.addedRoute)
	}
	ref := AddressRef{Interface: "eth0", Address: netip.MustParseAddr("2001:4860:1::12")}
	if err := kernel.ValidateBindable(context.Background(), ref, true); err != nil {
		t.Fatal(err)
	}
	if binder.ref != ref || !binder.freebind {
		t.Fatalf("bind validation = ref %#v freebind %t", binder.ref, binder.freebind)
	}
}
