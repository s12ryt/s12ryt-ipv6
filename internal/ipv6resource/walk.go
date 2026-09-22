package ipv6resource

import (
	"errors"
	"fmt"
	"net/netip"
)

// GenerateAddressesFrom generates count available addresses, starting the scan at
// start when it is a valid IPv6 address inside prefix and walking towards higher
// addresses. The scan wraps back to the first address of the prefix at the end so
// small prefixes stay fully usable. Callers persist the address that follows the
// last generated address so successive pool refreshes keep walking forward
// instead of reusing the lowest addresses that a completed drain just released.
func GenerateAddressesFrom(prefix netip.Prefix, count int, occupied map[netip.Addr]struct{}, start netip.Addr) ([]netip.Addr, error) {
	prefix = prefix.Masked()
	if !prefix.IsValid() || !prefix.Addr().Is6() || prefix.Bits() < globalUnicast.Bits() || !globalUnicast.Contains(prefix.Addr()) {
		return nil, errors.New("prefix must be within IPv6 global unicast 2000::/3")
	}
	if count < 1 || count > MaxPoolSize {
		return nil, fmt.Errorf("pool size must be between 1 and %d", MaxPoolSize)
	}

	begin := prefix.Addr()
	if start.IsValid() && !start.Is4In6() && prefix.Contains(start) {
		begin = start
	}

	addresses := make([]netip.Addr, 0, count)
	for address := begin; ; {
		if _, exists := occupied[address]; !exists {
			addresses = append(addresses, address)
			if len(addresses) == count {
				return addresses, nil
			}
		}
		next := address.Next()
		if !next.IsValid() || !prefix.Contains(next) {
			next = prefix.Addr()
		}
		if next == begin {
			break
		}
		address = next
	}

	return nil, fmt.Errorf("prefix %s does not have %d available addresses", prefix, count)
}

// generateAutomatic produces automatic pool addresses for a template, resuming at
// the walk position persisted for the template prefix. The caller must hold the
// store lock.
func (s *Store) generateAutomatic(templateName string, count int) ([]netip.Addr, error) {
	if count <= 0 {
		return nil, nil
	}
	prefix := s.templates[templateName].Prefix
	generated, err := GenerateAddressesFrom(prefix, count, s.occupied(), s.nextAddresses[prefix.String()])
	if err != nil {
		return nil, err
	}
	s.advanceNextAddress(prefix, generated)
	return generated, nil
}

// advanceNextAddress records the walk position that follows the last generated
// address, wrapping back to the first address of the prefix at the end. The caller
// must hold the store lock.
func (s *Store) advanceNextAddress(prefix netip.Prefix, generated []netip.Addr) {
	if len(generated) == 0 {
		return
	}
	if s.nextAddresses == nil {
		s.nextAddresses = make(map[string]netip.Addr)
	}
	next := generated[len(generated)-1].Next()
	if !next.IsValid() || !prefix.Contains(next) {
		next = prefix.Addr()
	}
	s.nextAddresses[prefix.String()] = next
}
