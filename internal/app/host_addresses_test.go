package app

import (
	"errors"
	"net"
	"net/netip"
	"reflect"
	"testing"
)

type staticNetAddress string

func (a staticNetAddress) Network() string { return "test" }
func (a staticNetAddress) String() string  { return string(a) }

func TestCollectHostAddressesNormalizesAndDeduplicates(t *testing.T) {
	addresses, err := collectHostAddresses([]net.Addr{
		&net.IPNet{IP: net.ParseIP("2001:4860::10"), Mask: net.CIDRMask(64, 128)},
		&net.IPAddr{IP: net.ParseIP("127.0.0.1")},
		staticNetAddress("::ffff:192.0.2.10/128"),
		staticNetAddress("2001:4860::10/64"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Addr{
		netip.MustParseAddr("127.0.0.1"),
		netip.MustParseAddr("192.0.2.10"),
		netip.MustParseAddr("2001:4860::10"),
	}
	if !reflect.DeepEqual(addresses, want) {
		t.Fatalf("collectHostAddresses() = %v, want %v", addresses, want)
	}
}

func TestCollectHostAddressesRejectsInvalidInput(t *testing.T) {
	if _, err := collectHostAddresses([]net.Addr{staticNetAddress("not-an-address")}); err == nil {
		t.Fatal("collectHostAddresses(invalid) error = nil")
	}
}

func TestScanHostAddressesPropagatesInterfaceErrors(t *testing.T) {
	wantErr := errors.New("enumeration failed")
	if _, err := scanHostAddresses(func() ([]net.Addr, error) { return nil, wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("scanHostAddresses() error = %v", err)
	}
}
