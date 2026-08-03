package dns64

import (
	"context"
	"errors"
	"net/netip"
)

type Discovery struct {
	Prefix   netip.Prefix
	Source   string
	Conflict bool
	Observed map[string]netip.Prefix
}

func DiscoverNAT64Prefix(ctx context.Context, endpoints []Endpoint, queryer Queryer) (Discovery, error) {
	result := Discovery{Observed: make(map[string]netip.Prefix)}
	for _, endpoint := range endpoints {
		if validateEndpoint(endpoint) != nil {
			continue
		}
		answer, err := queryer.Query(ctx, endpoint, "ipv4only.arpa.", TypeAAAA)
		if err != nil {
			continue
		}
		prefix, ok := prefixFromRFC7050(answer.Addresses)
		if !ok {
			continue
		}
		result.Observed[endpoint.Name] = prefix
		if !result.Prefix.IsValid() {
			result.Prefix = prefix
			result.Source = endpoint.Name
		} else if result.Prefix != prefix {
			result.Conflict = true
		}
	}
	if !result.Prefix.IsValid() {
		return Discovery{}, ErrNAT64Unavailable
	}
	return result, nil
}

func ValidateNAT64Prefix(prefix netip.Prefix) error {
	if !prefix.IsValid() || !prefix.Addr().Is6() || prefix.Addr().Is4In6() || prefix.Bits() != 96 {
		return errors.New("NAT64 prefix must be an IPv6 /96")
	}
	if prefix != prefix.Masked() {
		return errors.New("NAT64 prefix must be canonical")
	}
	return nil
}

func prefixFromRFC7050(addresses []netip.Addr) (netip.Prefix, bool) {
	for _, address := range addresses {
		if !address.Is6() || address.Is4In6() {
			continue
		}
		bytes := address.As16()
		if bytes[12] != 192 || bytes[13] != 0 || bytes[14] != 0 || (bytes[15] != 170 && bytes[15] != 171) {
			continue
		}
		for i := 12; i < 16; i++ {
			bytes[i] = 0
		}
		prefix := netip.PrefixFrom(netip.AddrFrom16(bytes), 96)
		if ValidateNAT64Prefix(prefix) == nil {
			return prefix, true
		}
	}
	return netip.Prefix{}, false
}
