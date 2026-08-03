package ipv6resource

import (
	"net/netip"
	"testing"
)

func TestNewPrefixTemplateAcceptsGlobalUnicastPrefixLengths(t *testing.T) {
	tests := []struct {
		name string
		cidr string
		want string
	}{
		{name: "entire global unicast space", cidr: "2000::/3", want: "2000::/3"},
		{name: "normalizes host bits", cidr: "2001:db8:1:2::1234/64", want: "2001:db8:1:2::/64"},
		{name: "point to point", cidr: "2001:db8::10/127", want: "2001:db8::10/127"},
		{name: "single address", cidr: "2001:db8::20/128", want: "2001:db8::20/128"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			template, err := NewPrefixTemplate("edge", tt.cidr, "eth0", ModeAddress)
			if err != nil {
				t.Fatalf("NewPrefixTemplate() error = %v", err)
			}
			if got := template.Prefix.String(); got != tt.want {
				t.Fatalf("normalized prefix = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewPrefixTemplateRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		cidr  string
		iface string
		mode  ConfigMode
	}{
		{name: "IPv4", cidr: "192.0.2.0/24", iface: "eth0", mode: ModeAddress},
		{name: "ULA", cidr: "fd00::/64", iface: "eth0", mode: ModeAddress},
		{name: "multicast", cidr: "ff00::/8", iface: "eth0", mode: ModeAddress},
		{name: "missing interface", cidr: "2001:db8::/64", mode: ModeAddress},
		{name: "unknown mode", cidr: "2001:db8::/64", iface: "eth0", mode: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewPrefixTemplate("edge", tt.cidr, tt.iface, tt.mode); err == nil {
				t.Fatal("NewPrefixTemplate() error = nil, want validation error")
			}
		})
	}
}

func TestValidateTemplateSetRejectsOverlap(t *testing.T) {
	existing, err := NewPrefixTemplate("primary", "2001:db8:100::/48", "eth0", ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := NewPrefixTemplate("nested", "2001:db8:100:1::/64", "eth1", ModeExternal)
	if err != nil {
		t.Fatal(err)
	}

	if err := ValidateTemplateSet([]PrefixTemplate{existing}, candidate); err == nil {
		t.Fatal("ValidateTemplateSet() error = nil, want overlap error")
	}
}

func TestGenerateAddressesUsesEntireSmallPrefix(t *testing.T) {
	prefix := netip.MustParsePrefix("2001:db8::10/127")

	got, err := GenerateAddresses(prefix, 2, nil)
	if err != nil {
		t.Fatalf("GenerateAddresses() error = %v", err)
	}
	want := []netip.Addr{
		netip.MustParseAddr("2001:db8::10"),
		netip.MustParseAddr("2001:db8::11"),
	}
	if len(got) != len(want) {
		t.Fatalf("len(addresses) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("addresses[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestGenerateAddressesSkipsOccupiedAndRejectsExhaustion(t *testing.T) {
	prefix := netip.MustParsePrefix("2001:db8::20/127")
	occupied := map[netip.Addr]struct{}{netip.MustParseAddr("2001:db8::20"): {}}

	got, err := GenerateAddresses(prefix, 1, occupied)
	if err != nil {
		t.Fatalf("GenerateAddresses() error = %v", err)
	}
	if want := netip.MustParseAddr("2001:db8::21"); got[0] != want {
		t.Fatalf("address = %s, want %s", got[0], want)
	}
	if _, err := GenerateAddresses(prefix, 2, occupied); err == nil {
		t.Fatal("GenerateAddresses() error = nil, want exhaustion error")
	}
}

func TestGenerateAddressesEnforcesPoolLimit(t *testing.T) {
	if _, err := GenerateAddresses(netip.MustParsePrefix("2001:db8::/64"), MaxPoolSize+1, nil); err == nil {
		t.Fatal("GenerateAddresses() error = nil, want pool size error")
	}
}
