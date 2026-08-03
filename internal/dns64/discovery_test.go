package dns64

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
)

func TestDiscoverNAT64PrefixUsesHighestPriorityAndReportsConflict(t *testing.T) {
	queryer := &fakeQueryer{replies: map[queryKey]fakeReply{
		{endpoint: "primary", name: "ipv4only.arpa.", record: TypeAAAA}: {
			result: QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("64:ff9b::c000:aa")}, TTL: time.Minute},
		},
		{endpoint: "secondary", name: "ipv4only.arpa.", record: TypeAAAA}: {
			result: QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("2001:db8:64::c000:ab")}, TTL: time.Minute},
		},
	}}

	result, err := DiscoverNAT64Prefix(context.Background(), testEndpoints(), queryer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Prefix != netip.MustParsePrefix("64:ff9b::/96") || result.Source != "primary" || !result.Conflict {
		t.Fatalf("discovery = %#v", result)
	}
	if len(result.Observed) != 2 {
		t.Fatalf("observed = %#v", result.Observed)
	}
}

func TestDiscoverNAT64PrefixFallsBackAfterQueryFailure(t *testing.T) {
	queryer := &fakeQueryer{replies: map[queryKey]fakeReply{
		{endpoint: "primary", name: "ipv4only.arpa.", record: TypeAAAA}: {err: errors.New("down")},
		{endpoint: "secondary", name: "ipv4only.arpa.", record: TypeAAAA}: {
			result: QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("64:ff9b::c000:aa")}, TTL: time.Minute},
		},
	}}

	result, err := DiscoverNAT64Prefix(context.Background(), testEndpoints(), queryer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "secondary" || result.Prefix != netip.MustParsePrefix("64:ff9b::/96") {
		t.Fatalf("discovery = %#v", result)
	}
}

func TestDiscoverNAT64PrefixRejectsResponsesWithoutRFC7050Addresses(t *testing.T) {
	queryer := &fakeQueryer{replies: map[queryKey]fakeReply{
		{endpoint: "primary", name: "ipv4only.arpa.", record: TypeAAAA}: {
			result: QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111")}, TTL: time.Minute},
		},
	}}

	if _, err := DiscoverNAT64Prefix(context.Background(), testEndpoints()[:1], queryer); !errors.Is(err, ErrNAT64Unavailable) {
		t.Fatalf("DiscoverNAT64Prefix() error = %v, want ErrNAT64Unavailable", err)
	}
}

func TestValidateNAT64PrefixRequiresIPv6Slash96(t *testing.T) {
	for _, prefix := range []netip.Prefix{
		netip.MustParsePrefix("64:ff9b::/64"),
		netip.MustParsePrefix("192.0.2.0/24"),
		{},
	} {
		if err := ValidateNAT64Prefix(prefix); err == nil {
			t.Fatalf("ValidateNAT64Prefix(%s) error = nil", prefix)
		}
	}
	if err := ValidateNAT64Prefix(netip.MustParsePrefix("64:ff9b::/96")); err != nil {
		t.Fatalf("valid prefix rejected: %v", err)
	}
}
