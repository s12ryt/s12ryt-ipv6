package ipv6resource

import (
	"net/netip"
	"testing"
)

func TestGenerateAddressesFromWalksForwardPastReleasedAddresses(t *testing.T) {
	prefix := netip.MustParsePrefix(walkTestPrefix)
	first, err := GenerateAddressesFrom(prefix, 2, nil, netip.Addr{})
	if err != nil {
		t.Fatalf("GenerateAddressesFrom() error = %v", err)
	}
	if want := []netip.Addr{netip.MustParseAddr("2001:db8:1::"), netip.MustParseAddr("2001:db8:1::1")}; !sameAddresses(first, want) {
		t.Fatalf("first generation = %v, want %v", first, want)
	}
	// A completed drain releases the first two addresses again: the persisted walk
	// position must keep the next generation moving forward.
	second, err := GenerateAddressesFrom(prefix, 2, nil, first[len(first)-1].Next())
	if err != nil {
		t.Fatalf("GenerateAddressesFrom() error = %v", err)
	}
	if want := []netip.Addr{netip.MustParseAddr("2001:db8:1::2"), netip.MustParseAddr("2001:db8:1::3")}; !sameAddresses(second, want) {
		t.Fatalf("second generation = %v, want %v", second, want)
	}
}

func TestGenerateAddressesFromWrapsAtPrefixEnd(t *testing.T) {
	prefix := netip.MustParsePrefix("2001:db8:1::/124")
	occupied := map[netip.Addr]struct{}{
		netip.MustParseAddr("2001:db8:1::e"): {},
		netip.MustParseAddr("2001:db8:1::f"): {},
	}
	generated, err := GenerateAddressesFrom(prefix, 2, occupied, netip.MustParseAddr("2001:db8:1::e"))
	if err != nil {
		t.Fatalf("GenerateAddressesFrom() error = %v", err)
	}
	if want := []netip.Addr{netip.MustParseAddr("2001:db8:1::"), netip.MustParseAddr("2001:db8:1::1")}; !sameAddresses(generated, want) {
		t.Fatalf("wrapped generation = %v, want %v", generated, want)
	}
}

func TestGenerateAddressesFromRejectsExhaustedPrefix(t *testing.T) {
	prefix := netip.MustParsePrefix("2001:db8:1::/126")
	occupied := make(map[netip.Addr]struct{})
	for address := prefix.Addr(); prefix.Contains(address); address = address.Next() {
		occupied[address] = struct{}{}
	}
	if _, err := GenerateAddressesFrom(prefix, 1, occupied, netip.Addr{}); err == nil {
		t.Fatal("GenerateAddressesFrom() error = nil, want exhaustion error")
	}
}
