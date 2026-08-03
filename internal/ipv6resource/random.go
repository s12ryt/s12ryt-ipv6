package ipv6resource

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
)

const randomAddressAttempts = 128

func RandomAddress(prefix netip.Prefix, occupied map[netip.Addr]struct{}, entropy io.Reader) (netip.Addr, error) {
	prefix = prefix.Masked()
	if !prefix.IsValid() || !prefix.Addr().Is6() || prefix.Bits() < globalUnicast.Bits() || !globalUnicast.Contains(prefix.Addr()) {
		return netip.Addr{}, errors.New("prefix must be within IPv6 global unicast 2000::/3")
	}
	if entropy == nil {
		return netip.Addr{}, errors.New("entropy source is required")
	}
	if prefixIsExhausted(prefix, occupied) {
		return netip.Addr{}, fmt.Errorf("prefix %s has no available addresses", prefix)
	}

	network := prefix.Addr().As16()
	for range randomAddressAttempts {
		var random [16]byte
		if _, err := io.ReadFull(entropy, random[:]); err != nil {
			return netip.Addr{}, fmt.Errorf("read address entropy: %w", err)
		}
		candidateBytes := random
		fullBytes := prefix.Bits() / 8
		copy(candidateBytes[:fullBytes], network[:fullBytes])
		if remaining := prefix.Bits() % 8; remaining != 0 {
			mask := byte(0xff << (8 - remaining))
			candidateBytes[fullBytes] = network[fullBytes]&mask | random[fullBytes]&^mask
		}
		candidate := netip.AddrFrom16(candidateBytes)
		if _, exists := occupied[candidate]; !exists {
			return candidate, nil
		}
	}

	fallback, err := GenerateAddresses(prefix, 1, occupied)
	if err != nil {
		return netip.Addr{}, err
	}
	return fallback[0], nil
}

func prefixIsExhausted(prefix netip.Prefix, occupied map[netip.Addr]struct{}) bool {
	hostBits := 128 - prefix.Bits()
	if hostBits > 12 {
		return false
	}
	capacity := 1 << hostBits
	used := 0
	for address := range occupied {
		if prefix.Contains(address) {
			used++
		}
	}
	return used >= capacity
}
