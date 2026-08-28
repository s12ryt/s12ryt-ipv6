//go:build linux

package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var ErrDADFailed = errors.New("IPv6 duplicate address detection failed")

type netlinkDriver interface {
	LinkByName(string) (netlink.Link, error)
	AddrList(netlink.Link, int) ([]netlink.Addr, error)
	AddrAdd(netlink.Link, *netlink.Addr) error
	AddrDel(netlink.Link, *netlink.Addr) error
	RouteListFiltered(int, *netlink.Route, uint64) ([]netlink.Route, error)
	RouteAdd(*netlink.Route) error
	RouteDel(*netlink.Route) error
}

type bindValidator interface {
	Validate(context.Context, AddressRef, bool) error
}

type linuxKernel struct {
	driver       netlinkDriver
	binder       bindValidator
	pollInterval time.Duration
}

func NewLinuxKernel() (Kernel, error) {
	return newLinuxKernel(packageNetlinkDriver{}, systemBindValidator{}, 100*time.Millisecond), nil
}

func newLinuxKernel(driver netlinkDriver, binder bindValidator, pollInterval time.Duration) *linuxKernel {
	return &linuxKernel{driver: driver, binder: binder, pollInterval: pollInterval}
}

func (k *linuxKernel) AddressExists(ctx context.Context, ref AddressRef) (bool, error) {
	link, err := k.link(ctx, ref.Interface)
	if err != nil {
		return false, err
	}
	addresses, err := k.driver.AddrList(link, netlink.FAMILY_V6)
	if err != nil {
		return false, fmt.Errorf("list IPv6 addresses on %s: %w", ref.Interface, err)
	}
	return findAddress(addresses, ref.Address) != nil, nil
}

func (k *linuxKernel) InterfaceAddresses(ctx context.Context, interfaceName string) ([]netip.Addr, error) {
	link, err := k.link(ctx, interfaceName)
	if err != nil {
		return nil, err
	}
	addresses, err := k.driver.AddrList(link, netlink.FAMILY_V6)
	if err != nil {
		return nil, fmt.Errorf("list IPv6 addresses on %s: %w", interfaceName, err)
	}
	result := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		if address.IPNet == nil {
			continue
		}
		if addr, ok := netip.AddrFromSlice(address.IPNet.IP); ok {
			result = append(result, addr.Unmap())
		}
	}
	return result, nil
}

func (k *linuxKernel) AddAddress(ctx context.Context, ref AddressRef) error {
	link, err := k.link(ctx, ref.Interface)
	if err != nil {
		return err
	}
	if err := k.driver.AddrAdd(link, addressToNetlink(ref.Address)); err != nil {
		return fmt.Errorf("add IPv6 address %s to %s: %w", ref.Address, ref.Interface, err)
	}
	return nil
}

func (k *linuxKernel) RemoveAddress(ctx context.Context, ref AddressRef) error {
	link, err := k.link(ctx, ref.Interface)
	if err != nil {
		return err
	}
	if err := k.driver.AddrDel(link, addressToNetlink(ref.Address)); err != nil {
		return fmt.Errorf("delete IPv6 address %s from %s: %w", ref.Address, ref.Interface, err)
	}
	return nil
}

func (k *linuxKernel) WaitAddressReady(ctx context.Context, ref AddressRef) error {
	link, err := k.link(ctx, ref.Interface)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(k.pollInterval)
	defer ticker.Stop()
	for {
		addresses, err := k.driver.AddrList(link, netlink.FAMILY_V6)
		if err != nil {
			return fmt.Errorf("list IPv6 addresses during DAD on %s: %w", ref.Interface, err)
		}
		address := findAddress(addresses, ref.Address)
		if address != nil {
			if address.Flags&unix.IFA_F_DADFAILED != 0 {
				return fmt.Errorf("%w: %s on %s", ErrDADFailed, ref.Address, ref.Interface)
			}
			if address.Flags&unix.IFA_F_TENTATIVE == 0 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// WaitAddressesReady shares a single poller per interface instead of one
// goroutine per address. Error aggregation keeps the per-address wrapping
// used by the single-address variant ("wait for address %s DAD: %w") so the
// resulting errors stay identical for callers.
func (k *linuxKernel) WaitAddressesReady(ctx context.Context, refs []AddressRef) error {
	if len(refs) == 0 {
		return nil
	}
	var groups []string
	byInterface := make(map[string][]AddressRef, 1)
	for _, ref := range refs {
		if _, seen := byInterface[ref.Interface]; !seen {
			groups = append(groups, ref.Interface)
		}
		byInterface[ref.Interface] = append(byInterface[ref.Interface], ref)
	}
	links := make(map[string]netlink.Link, len(groups))
	for _, name := range groups {
		link, err := k.link(ctx, name)
		if err != nil {
			return err
		}
		links[name] = link
	}

	pending := make([]AddressRef, len(refs))
	copy(pending, refs)
	ticker := time.NewTicker(k.pollInterval)
	defer ticker.Stop()
	for {
		var failures []error
		ready := make(map[AddressRef]struct{})
		failed := make(map[AddressRef]struct{})
		for _, name := range groups {
			addresses, err := k.driver.AddrList(links[name], netlink.FAMILY_V6)
			if err != nil {
				wrapped := fmt.Errorf("list IPv6 addresses during DAD on %s: %w", name, err)
				for _, ref := range byInterface[name] {
					failures = append(failures, fmt.Errorf("wait for address %s DAD: %w", ref.Address, wrapped))
					failed[ref] = struct{}{}
				}
				continue
			}
			for _, ref := range byInterface[name] {
				if _, done := ready[ref]; done {
					continue
				}
				address := findAddress(addresses, ref.Address)
				if address == nil {
					continue
				}
				if address.Flags&unix.IFA_F_DADFAILED != 0 {
					failures = append(failures, fmt.Errorf("wait for address %s DAD: %w", ref.Address,
						fmt.Errorf("%w: %s on %s", ErrDADFailed, ref.Address, ref.Interface)))
					failed[ref] = struct{}{}
					continue
				}
				if address.Flags&unix.IFA_F_TENTATIVE == 0 {
					ready[ref] = struct{}{}
				}
			}
		}
		pending = slices.DeleteFunc(pending, func(ref AddressRef) bool {
			_, done := ready[ref]
			return done
		})
		if len(failures) > 0 {
			// A failure aborts the whole wait; remaining addresses see the
			// cancellation just like the per-address waiters used to.
			for _, ref := range pending {
				if _, hit := failed[ref]; hit {
					continue
				}
				failures = append(failures, fmt.Errorf("wait for address %s DAD: %w", ref.Address, context.Canceled))
			}
			return errors.Join(failures...)
		}
		if len(pending) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			for _, ref := range pending {
				failures = append(failures, fmt.Errorf("wait for address %s DAD: %w", ref.Address, ctx.Err()))
			}
			return errors.Join(failures...)
		case <-ticker.C:
		}
	}
}

func (k *linuxKernel) LocalRouteExists(ctx context.Context, ref RouteRef) (bool, error) {
	link, err := k.link(ctx, ref.Interface)
	if err != nil {
		return false, err
	}
	filter := routeToNetlink(link.Attrs().Index, ref.Prefix)
	routes, err := k.driver.RouteListFiltered(
		netlink.FAMILY_V6,
		filter,
		netlink.RT_FILTER_DST|netlink.RT_FILTER_TABLE|netlink.RT_FILTER_OIF|netlink.RT_FILTER_TYPE,
	)
	if err != nil {
		return false, fmt.Errorf("list local route %s on %s: %w", ref.Prefix, ref.Interface, err)
	}
	return len(routes) > 0, nil
}

func (k *linuxKernel) AddLocalRoute(ctx context.Context, ref RouteRef) error {
	link, err := k.link(ctx, ref.Interface)
	if err != nil {
		return err
	}
	if err := k.driver.RouteAdd(routeToNetlink(link.Attrs().Index, ref.Prefix)); err != nil {
		return fmt.Errorf("add local route %s on %s: %w", ref.Prefix, ref.Interface, err)
	}
	return nil
}

func (k *linuxKernel) RemoveLocalRoute(ctx context.Context, ref RouteRef) error {
	link, err := k.link(ctx, ref.Interface)
	if err != nil {
		return err
	}
	if err := k.driver.RouteDel(routeToNetlink(link.Attrs().Index, ref.Prefix)); err != nil {
		return fmt.Errorf("delete local route %s on %s: %w", ref.Prefix, ref.Interface, err)
	}
	return nil
}

func (k *linuxKernel) ValidateBindable(ctx context.Context, ref AddressRef, freebind bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return k.binder.Validate(ctx, ref, freebind)
}

func (k *linuxKernel) link(ctx context.Context, name string) (netlink.Link, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	link, err := k.driver.LinkByName(name)
	if err != nil {
		return nil, fmt.Errorf("find network interface %s: %w", name, err)
	}
	return link, nil
}

type packageNetlinkDriver struct{}

func (packageNetlinkDriver) LinkByName(name string) (netlink.Link, error) {
	return netlink.LinkByName(name)
}
func (packageNetlinkDriver) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return netlink.AddrList(link, family)
}
func (packageNetlinkDriver) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrAdd(link, addr)
}
func (packageNetlinkDriver) AddrDel(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrDel(link, addr)
}
func (packageNetlinkDriver) RouteListFiltered(family int, filter *netlink.Route, mask uint64) ([]netlink.Route, error) {
	return netlink.RouteListFiltered(family, filter, mask)
}
func (packageNetlinkDriver) RouteAdd(route *netlink.Route) error { return netlink.RouteAdd(route) }
func (packageNetlinkDriver) RouteDel(route *netlink.Route) error { return netlink.RouteDel(route) }

type systemBindValidator struct{}

func (systemBindValidator) Validate(ctx context.Context, ref AddressRef, freebind bool) error {
	control := func(_ string, _ string, raw syscall.RawConn) error {
		var socketErr error
		if err := raw.Control(func(fd uintptr) {
			if err := unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, ref.Interface); err != nil {
				socketErr = err
				return
			}
			if freebind {
				socketErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_FREEBIND, 1)
			}
		}); err != nil {
			return err
		}
		return socketErr
	}
	listenConfig := net.ListenConfig{Control: control}
	address := net.JoinHostPort(ref.Address.String(), "0")
	listener, err := listenConfig.Listen(ctx, "tcp6", address)
	if err != nil {
		return fmt.Errorf("validate TCP bind to %s on %s: %w", ref.Address, ref.Interface, err)
	}
	if err := listener.Close(); err != nil {
		return fmt.Errorf("close TCP bind validation socket: %w", err)
	}
	packet, err := listenConfig.ListenPacket(ctx, "udp6", address)
	if err != nil {
		return fmt.Errorf("validate UDP bind to %s on %s: %w", ref.Address, ref.Interface, err)
	}
	if err := packet.Close(); err != nil {
		return fmt.Errorf("close UDP bind validation socket: %w", err)
	}
	return nil
}

func addressToNetlink(address netip.Addr) *netlink.Addr {
	return &netlink.Addr{IPNet: &net.IPNet{IP: net.IP(address.AsSlice()), Mask: net.CIDRMask(128, 128)}}
}

func findAddress(addresses []netlink.Addr, target netip.Addr) *netlink.Addr {
	for i := range addresses {
		if addresses[i].IPNet != nil && addresses[i].IPNet.IP.Equal(net.IP(target.AsSlice())) {
			return &addresses[i]
		}
	}
	return nil
}

func routeToNetlink(linkIndex int, prefix netip.Prefix) *netlink.Route {
	ones, bits := prefix.Masked().Bits(), 128
	return &netlink.Route{
		LinkIndex: linkIndex,
		Dst: &net.IPNet{
			IP:   net.IP(prefix.Masked().Addr().AsSlice()),
			Mask: net.CIDRMask(ones, bits),
		},
		Scope: netlink.SCOPE_HOST,
		Table: unix.RT_TABLE_LOCAL,
		Type:  unix.RTN_LOCAL,
	}
}
