package ipv6resource

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

const MaxPoolSize = 4096

var globalUnicast = netip.MustParsePrefix("2000::/3")

type ConfigMode string

const (
	ModeAddress            ConfigMode = "address"
	ModeLocalRouteFreebind ConfigMode = "local-route-freebind"
	ModeExternal           ConfigMode = "external"
)

type PrefixTemplate struct {
	Name      string       `yaml:"name"`
	Prefix    netip.Prefix `yaml:"prefix"`
	Interface string       `yaml:"interface"`
	Mode      ConfigMode   `yaml:"mode"`
}

func NewPrefixTemplate(name, cidr, iface string, mode ConfigMode) (PrefixTemplate, error) {
	if strings.TrimSpace(name) == "" {
		return PrefixTemplate{}, errors.New("template name is required")
	}
	if strings.TrimSpace(iface) == "" {
		return PrefixTemplate{}, errors.New("network interface is required")
	}
	if !validMode(mode) {
		return PrefixTemplate{}, fmt.Errorf("unsupported configuration mode %q", mode)
	}

	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return PrefixTemplate{}, fmt.Errorf("parse IPv6 prefix: %w", err)
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is6() || prefix.Bits() < globalUnicast.Bits() || !globalUnicast.Contains(prefix.Addr()) {
		return PrefixTemplate{}, errors.New("prefix must be within IPv6 global unicast 2000::/3")
	}

	return PrefixTemplate{
		Name:      strings.TrimSpace(name),
		Prefix:    prefix,
		Interface: strings.TrimSpace(iface),
		Mode:      mode,
	}, nil
}

func ValidateTemplateSet(existing []PrefixTemplate, candidate PrefixTemplate) error {
	for _, current := range existing {
		if current.Name == candidate.Name {
			return fmt.Errorf("template name %q already exists", candidate.Name)
		}
		if prefixesOverlap(current.Prefix, candidate.Prefix) {
			return fmt.Errorf("prefix %s overlaps template %q (%s)", candidate.Prefix, current.Name, current.Prefix)
		}
	}
	return nil
}

func GenerateAddresses(prefix netip.Prefix, count int, occupied map[netip.Addr]struct{}) ([]netip.Addr, error) {
	prefix = prefix.Masked()
	if !prefix.IsValid() || !prefix.Addr().Is6() || prefix.Bits() < globalUnicast.Bits() || !globalUnicast.Contains(prefix.Addr()) {
		return nil, errors.New("prefix must be within IPv6 global unicast 2000::/3")
	}
	if count < 1 || count > MaxPoolSize {
		return nil, fmt.Errorf("pool size must be between 1 and %d", MaxPoolSize)
	}

	addresses := make([]netip.Addr, 0, count)
	for address := prefix.Addr(); address.IsValid() && prefix.Contains(address); address = address.Next() {
		if _, exists := occupied[address]; exists {
			continue
		}
		addresses = append(addresses, address)
		if len(addresses) == count {
			return addresses, nil
		}
	}

	return nil, fmt.Errorf("prefix %s does not have %d available addresses", prefix, count)
}

func validMode(mode ConfigMode) bool {
	switch mode {
	case ModeAddress, ModeLocalRouteFreebind, ModeExternal:
		return true
	default:
		return false
	}
}

func prefixesOverlap(left, right netip.Prefix) bool {
	return left.Contains(right.Addr()) || right.Contains(left.Addr())
}
