package policy

import (
	"errors"
	"fmt"
	"net/netip"
)

type ULAOverride string

const (
	ULAInherit ULAOverride = "inherit"
	ULAAllow   ULAOverride = "allow"
	ULADeny    ULAOverride = "deny"
)

type DestinationPolicy struct {
	AllowULA         bool
	NAT64Prefix      netip.Prefix
	LocalAddresses   map[netip.Addr]struct{}
	ManagedAddresses map[netip.Addr]struct{}
}

var (
	ulaPrefix         = netip.MustParsePrefix("fc00::/7")
	publicIPv6Prefix  = netip.MustParsePrefix("2000::/3")
	specialIPv4Ranges = mustPrefixes(
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"192.31.196.0/24",
		"192.52.193.0/24",
		"192.88.99.0/24",
		"192.168.0.0/16",
		"192.175.48.0/24",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"224.0.0.0/4",
		"240.0.0.0/4",
	)
	specialIPv6Ranges = mustPrefixes(
		"100::/64",
		"2001::/23",
		"2001:db8::/32",
		"2002::/16",
		"3fff::/20",
		"5f00::/16",
	)
)

func (p DestinationPolicy) Check(address netip.Addr, ulaOverride ULAOverride) error {
	if !address.IsValid() {
		return errors.New("destination address is invalid")
	}
	address = address.Unmap()
	if _, blocked := p.LocalAddresses[address]; blocked {
		return fmt.Errorf("destination %s belongs to the host", address)
	}
	if _, blocked := p.ManagedAddresses[address]; blocked {
		return fmt.Errorf("destination %s is managed by this service", address)
	}

	if address.Is4() {
		return checkPublicIPv4(address)
	}
	if embedded, ok := decodeNAT64(p.NAT64Prefix, address); ok {
		return checkPublicIPv4(embedded)
	}
	if ulaPrefix.Contains(address) {
		if allowULA(p.AllowULA, ulaOverride) {
			return nil
		}
		return fmt.Errorf("ULA destination %s is blocked", address)
	}
	if !publicIPv6Prefix.Contains(address) || inAnyPrefix(address, specialIPv6Ranges) {
		return fmt.Errorf("non-global IPv6 destination %s is blocked", address)
	}
	return nil
}

func checkPublicIPv4(address netip.Addr) error {
	if !address.Is4() || !address.IsGlobalUnicast() || inAnyPrefix(address, specialIPv4Ranges) {
		return fmt.Errorf("non-global IPv4 destination %s is blocked", address)
	}
	return nil
}

func decodeNAT64(prefix netip.Prefix, address netip.Addr) (netip.Addr, bool) {
	if !prefix.IsValid() || !prefix.Addr().Is6() || prefix.Bits() != 96 || !prefix.Contains(address) {
		return netip.Addr{}, false
	}
	bytes := address.As16()
	return netip.AddrFrom4([4]byte{bytes[12], bytes[13], bytes[14], bytes[15]}), true
}

func allowULA(global bool, override ULAOverride) bool {
	switch override {
	case ULAAllow:
		return true
	case ULADeny:
		return false
	default:
		return global
	}
}

func inAnyPrefix(address netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func mustPrefixes(values ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}
