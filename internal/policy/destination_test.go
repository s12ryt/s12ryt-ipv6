package policy

import (
	"net/netip"
	"testing"
)

func TestDestinationPolicyAllowsPublicAddresses(t *testing.T) {
	p := DestinationPolicy{}

	for _, raw := range []string{"2606:4700:4700::1111", "8.8.8.8"} {
		if err := p.Check(netip.MustParseAddr(raw), ULAInherit); err != nil {
			t.Errorf("Check(%s) error = %v, want nil", raw, err)
		}
	}
}

func TestDestinationPolicyRejectsSpecialIPv4(t *testing.T) {
	p := DestinationPolicy{}
	blocked := []string{
		"0.0.0.0", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.1.1",
		"172.16.0.1", "192.0.2.1", "192.168.1.1", "198.18.0.1", "198.51.100.1",
		"203.0.113.1", "224.0.0.1", "255.255.255.255",
	}

	for _, raw := range blocked {
		if err := p.Check(netip.MustParseAddr(raw), ULAInherit); err == nil {
			t.Errorf("Check(%s) error = nil, want blocked", raw)
		}
	}
}

func TestDestinationPolicyAppliesULAOverride(t *testing.T) {
	ula := netip.MustParseAddr("fd00::1")

	if err := (DestinationPolicy{AllowULA: false}).Check(ula, ULAInherit); err == nil {
		t.Fatal("inherited deny accepted ULA")
	}
	if err := (DestinationPolicy{AllowULA: false}).Check(ula, ULAAllow); err != nil {
		t.Fatalf("node allow rejected ULA: %v", err)
	}
	if err := (DestinationPolicy{AllowULA: true}).Check(ula, ULADeny); err == nil {
		t.Fatal("node deny accepted ULA")
	}
}

func TestDestinationPolicyRejectsLocalAndManagedAddresses(t *testing.T) {
	local := netip.MustParseAddr("2001:db8::10")
	managed := netip.MustParseAddr("2001:db8::20")
	p := DestinationPolicy{
		LocalAddresses:   map[netip.Addr]struct{}{local: {}},
		ManagedAddresses: map[netip.Addr]struct{}{managed: {}},
	}

	for _, addr := range []netip.Addr{local, managed} {
		if err := p.Check(addr, ULAInherit); err == nil {
			t.Errorf("Check(%s) error = nil, want local/managed block", addr)
		}
	}
}

func TestDestinationPolicyDecodesNAT64BeforeCheckingIPv4(t *testing.T) {
	p := DestinationPolicy{NAT64Prefix: netip.MustParsePrefix("64:ff9b::/96")}

	if err := p.Check(netip.MustParseAddr("64:ff9b::c000:201"), ULAInherit); err == nil {
		t.Fatal("documentation IPv4 embedded in NAT64 prefix was accepted")
	}
	if err := p.Check(netip.MustParseAddr("64:ff9b::808:808"), ULAInherit); err != nil {
		t.Fatalf("public IPv4 embedded in NAT64 prefix was rejected: %v", err)
	}
}

func TestDestinationPolicyRejectsOtherNonGlobalIPv6(t *testing.T) {
	p := DestinationPolicy{}
	for _, raw := range []string{"::", "::1", "fe80::1", "ff02::1"} {
		if err := p.Check(netip.MustParseAddr(raw), ULAInherit); err == nil {
			t.Errorf("Check(%s) error = nil, want blocked", raw)
		}
	}
}
