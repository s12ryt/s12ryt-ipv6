package ipv6resource

import (
	"bytes"
	"errors"
	"net/netip"
	"testing"
)

func TestRandomAddressMasksEntropyIntoPrefix(t *testing.T) {
	prefix := netip.MustParsePrefix("2001:db8:abcd::/56")
	entropy := bytes.NewReader(bytes.Repeat([]byte{0xff}, 16))

	address, err := RandomAddress(prefix, nil, entropy)
	if err != nil {
		t.Fatalf("RandomAddress() error = %v", err)
	}
	if !prefix.Contains(address) {
		t.Fatalf("address %s is outside %s", address, prefix)
	}
	if want := netip.MustParseAddr("2001:db8:abcd:ff:ffff:ffff:ffff:ffff"); address != want {
		t.Fatalf("address = %s, want %s", address, want)
	}
}

func TestRandomAddressRetriesCollision(t *testing.T) {
	prefix := netip.MustParsePrefix("2001:db8::/120")
	occupied := map[netip.Addr]struct{}{netip.MustParseAddr("2001:db8::1"): {}}
	first := append(make([]byte, 15), 1)
	second := append(make([]byte, 15), 2)

	address, err := RandomAddress(prefix, occupied, bytes.NewReader(append(first, second...)))
	if err != nil {
		t.Fatalf("RandomAddress() error = %v", err)
	}
	if want := netip.MustParseAddr("2001:db8::2"); address != want {
		t.Fatalf("address = %s, want %s", address, want)
	}
}

func TestRandomAddressReportsExhaustionAndEntropyFailure(t *testing.T) {
	single := netip.MustParsePrefix("2001:db8::1/128")
	occupied := map[netip.Addr]struct{}{single.Addr(): {}}
	if _, err := RandomAddress(single, occupied, bytes.NewReader(make([]byte, 16))); err == nil {
		t.Fatal("RandomAddress() error = nil, want exhaustion error")
	}

	if _, err := RandomAddress(netip.MustParsePrefix("2001:db8::/64"), nil, failingReader{}); err == nil {
		t.Fatal("RandomAddress() error = nil, want entropy error")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}
