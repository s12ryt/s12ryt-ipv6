//go:build linux

package network

import (
	"context"
	"net"
	"net/netip"

	"github.com/vishvananda/netlink"
)

type discoveryNetlinkDriver interface {
	LinkList() ([]netlink.Link, error)
	LinkByIndex(int) (netlink.Link, error)
	AddrList(netlink.Link, int) ([]netlink.Addr, error)
	RouteList(netlink.Link, int) ([]netlink.Route, error)
}

type linuxDiscoveryDriver struct {
	driver discoveryNetlinkDriver
}

func NewLinuxNetworkDiscovery() (NetworkDiscovery, error) {
	return newLinuxNetworkDiscovery(packageNetlinkDriver{})
}

func newLinuxNetworkDiscovery(driver discoveryNetlinkDriver) (NetworkDiscovery, error) {
	if driver == nil {
		return nil, net.UnknownNetworkError("network discovery driver is required")
	}
	return newNetworkDiscovery(&linuxDiscoveryDriver{driver: driver})
}

func (d *linuxDiscoveryDriver) Links(ctx context.Context) ([]discoveryLink, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	links, err := d.driver.LinkList()
	if err != nil {
		return nil, err
	}
	result := make([]discoveryLink, 0, len(links))
	for _, link := range links {
		if link == nil || link.Attrs() == nil {
			continue
		}
		attributes := link.Attrs()
		result = append(result, discoveryLink{
			Name: attributes.Name, Index: attributes.Index,
			Up: attributes.Flags&net.FlagUp != 0, Loopback: attributes.Flags&net.FlagLoopback != 0,
		})
	}
	return result, nil
}

func (d *linuxDiscoveryDriver) Addresses(ctx context.Context, index int) ([]netip.Prefix, error) {
	link, err := d.link(ctx, index)
	if err != nil {
		return nil, err
	}
	addresses, err := d.driver.AddrList(link, netlink.FAMILY_V6)
	if err != nil {
		return nil, err
	}
	result := make([]netip.Prefix, 0, len(addresses))
	for _, address := range addresses {
		if prefix, ok := prefixFromIPNet(address.IPNet); ok {
			result = append(result, prefix)
		}
	}
	return result, nil
}

func (d *linuxDiscoveryDriver) Routes(ctx context.Context, index int) ([]netip.Prefix, error) {
	link, err := d.link(ctx, index)
	if err != nil {
		return nil, err
	}
	routes, err := d.driver.RouteList(link, netlink.FAMILY_V6)
	if err != nil {
		return nil, err
	}
	result := make([]netip.Prefix, 0, len(routes))
	for _, route := range routes {
		if route.Dst == nil || (route.LinkIndex != 0 && route.LinkIndex != index) {
			continue
		}
		if prefix, ok := prefixFromIPNet(route.Dst); ok {
			result = append(result, prefix)
		}
	}
	return result, nil
}

func (d *linuxDiscoveryDriver) link(ctx context.Context, index int) (netlink.Link, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return d.driver.LinkByIndex(index)
}

func prefixFromIPNet(value *net.IPNet) (netip.Prefix, bool) {
	if value == nil {
		return netip.Prefix{}, false
	}
	ones, bits := value.Mask.Size()
	if ones < 0 || bits != 128 {
		return netip.Prefix{}, false
	}
	address, ok := netip.AddrFromSlice(value.IP)
	if !ok {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(address, ones), true
}

func (packageNetlinkDriver) LinkList() ([]netlink.Link, error) {
	return netlink.LinkList()
}

func (packageNetlinkDriver) LinkByIndex(index int) (netlink.Link, error) {
	return netlink.LinkByIndex(index)
}

func (packageNetlinkDriver) RouteList(link netlink.Link, family int) ([]netlink.Route, error) {
	return netlink.RouteList(link, family)
}
