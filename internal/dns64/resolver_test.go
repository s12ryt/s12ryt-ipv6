package dns64

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
)

type queryKey struct {
	endpoint string
	name     string
	record   RecordType
}

type fakeReply struct {
	result QueryResult
	err    error
}

type fakeQueryer struct {
	mu      sync.Mutex
	replies map[queryKey]fakeReply
	calls   []queryKey
}

func (f *fakeQueryer) Query(_ context.Context, endpoint Endpoint, name string, record RecordType) (QueryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := queryKey{endpoint: endpoint.Name, name: name, record: record}
	f.calls = append(f.calls, key)
	reply, ok := f.replies[key]
	if !ok {
		return QueryResult{}, errors.New("unexpected query")
	}
	return reply.result, reply.err
}

func (f *fakeQueryer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func TestResolverCacheDropsExpiredEntriesWhenInserting(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	reply := func(name string) (queryKey, fakeReply) {
		return queryKey{endpoint: "primary", name: name, record: TypeAAAA}, fakeReply{
			result: QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111")}, TTL: time.Minute},
		}
	}
	aKey, aReply := reply("a.example.")
	bKey, bReply := reply("b.example.")
	cKey, cReply := reply("c.example.")
	queryer := &fakeQueryer{replies: map[queryKey]fakeReply{aKey: aReply, bKey: bReply, cKey: cReply}}
	resolver, err := NewResolver(testEndpoints()[:1], queryer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	resolver.cacheMaxEntries = 2

	for _, name := range []string{"a.example.", "b.example."} {
		if _, err := resolver.Resolve(context.Background(), name, policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(resolver.cache) != 2 {
		t.Fatalf("cache size = %d, want 2", len(resolver.cache))
	}

	now = now.Add(90 * time.Second)
	if _, err := resolver.Resolve(context.Background(), "c.example.", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{}); err != nil {
		t.Fatal(err)
	}
	if len(resolver.cache) != 1 {
		t.Fatalf("expired entries were not evicted, cache = %d entries: %#v", len(resolver.cache), resolver.cache)
	}
	if _, ok := resolver.cache[cacheKey{name: "c.example.", record: TypeAAAA}]; !ok {
		t.Fatalf("fresh entry missing after eviction: %#v", resolver.cache)
	}
}

func TestResolverCacheEvictsEarliestExpiryWhenNoExpiredEntriesExist(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{replies: map[queryKey]fakeReply{
		{endpoint: "primary", name: "short.example.", record: TypeAAAA}: {
			result: QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111")}, TTL: time.Minute},
		},
		{endpoint: "primary", name: "long.example.", record: TypeAAAA}: {
			result: QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("2606:4700:4700::2222")}, TTL: 10 * time.Minute},
		},
		{endpoint: "primary", name: "fresh.example.", record: TypeAAAA}: {
			result: QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("2606:4700:4700::3333")}, TTL: 5 * time.Minute},
		},
	}}
	resolver, err := NewResolver(testEndpoints()[:1], queryer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	resolver.cacheMaxEntries = 2

	if _, err := resolver.Resolve(context.Background(), "short.example.", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Second)
	if _, err := resolver.Resolve(context.Background(), "long.example.", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Second)
	if _, err := resolver.Resolve(context.Background(), "fresh.example.", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{}); err != nil {
		t.Fatal(err)
	}

	if len(resolver.cache) != 2 {
		t.Fatalf("cache size = %d, want 2", len(resolver.cache))
	}
	if _, ok := resolver.cache[cacheKey{name: "short.example.", record: TypeAAAA}]; ok {
		t.Fatalf("earliest-expiry entry survived eviction: %#v", resolver.cache)
	}
	for _, name := range []string{"long.example.", "fresh.example."} {
		if _, ok := resolver.cache[cacheKey{name: name, record: TypeAAAA}]; !ok {
			t.Fatalf("entry %s missing after eviction: %#v", name, resolver.cache)
		}
	}
}

func TestResolverFailsOverInOrderAndClampsShortPositiveTTL(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{replies: map[queryKey]fakeReply{
		{endpoint: "primary", name: "example.com.", record: TypeAAAA}: {err: errors.New("primary unavailable")},
		{endpoint: "secondary", name: "example.com.", record: TypeAAAA}: {
			result: QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111")}, TTL: 5 * time.Second},
		},
	}}
	resolver, err := NewResolver(testEndpoints(), queryer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	first, err := resolver.Resolve(context.Background(), "example.com", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Source != "secondary" || len(first.Addresses) != 1 || first.Synthesized {
		t.Fatalf("resolution = %#v", first)
	}
	if queryer.callCount() != 2 {
		t.Fatalf("query count = %d, want 2", queryer.callCount())
	}

	now = now.Add(29 * time.Second)
	if _, err := resolver.Resolve(context.Background(), "example.com.", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{}); err != nil {
		t.Fatal(err)
	}
	if queryer.callCount() != 2 {
		t.Fatalf("cache miss before clamped TTL: %d calls", queryer.callCount())
	}

	now = now.Add(2 * time.Second)
	if _, err := resolver.Resolve(context.Background(), "example.com", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{}); err != nil {
		t.Fatal(err)
	}
	if queryer.callCount() != 4 {
		t.Fatalf("cache did not expire after clamped TTL: %d calls", queryer.callCount())
	}
}

func TestResolverClampsLongPositiveTTLAndUsesThirtySecondNegativeTTL(t *testing.T) {
	t.Run("positive maximum", func(t *testing.T) {
		now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
		queryer := &fakeQueryer{replies: map[queryKey]fakeReply{
			{endpoint: "primary", name: "long.example.", record: TypeAAAA}: {
				result: QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111")}, TTL: time.Hour},
			},
		}}
		resolver, err := NewResolver(testEndpoints()[:1], queryer, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if _, err := resolver.Resolve(context.Background(), "long.example", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{}); err != nil {
			t.Fatal(err)
		}
		now = now.Add(10*time.Minute - time.Second)
		if _, err := resolver.Resolve(context.Background(), "long.example", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{}); err != nil {
			t.Fatal(err)
		}
		if queryer.callCount() != 1 {
			t.Fatalf("query count before maximum TTL = %d", queryer.callCount())
		}
		now = now.Add(2 * time.Second)
		if _, err := resolver.Resolve(context.Background(), "long.example", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{}); err != nil {
			t.Fatal(err)
		}
		if queryer.callCount() != 2 {
			t.Fatalf("query count after maximum TTL = %d", queryer.callCount())
		}
	})

	t.Run("negative fixed TTL", func(t *testing.T) {
		now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
		queryer := &fakeQueryer{replies: map[queryKey]fakeReply{
			{endpoint: "primary", name: "v4-only.example.", record: TypeAAAA}: {result: QueryResult{TTL: time.Hour}},
			{endpoint: "primary", name: "v4-only.example.", record: TypeA}: {
				result: QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}, TTL: time.Hour},
			},
		}}
		resolver, err := NewResolver(testEndpoints()[:1], queryer, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		prefix := netip.MustParsePrefix("64:ff9b::/96")
		if _, err := resolver.Resolve(context.Background(), "v4-only.example", policy.DestinationPolicy{}, policy.ULAInherit, prefix); err != nil {
			t.Fatal(err)
		}
		now = now.Add(29 * time.Second)
		if _, err := resolver.Resolve(context.Background(), "v4-only.example", policy.DestinationPolicy{}, policy.ULAInherit, prefix); err != nil {
			t.Fatal(err)
		}
		if queryer.callCount() != 2 {
			t.Fatalf("negative cache missed early: %d calls", queryer.callCount())
		}
		now = now.Add(2 * time.Second)
		if _, err := resolver.Resolve(context.Background(), "v4-only.example", policy.DestinationPolicy{}, policy.ULAInherit, prefix); err != nil {
			t.Fatal(err)
		}
		if queryer.callCount() != 3 {
			t.Fatalf("negative cache did not expire: %d calls", queryer.callCount())
		}
	})
}

func TestResolverSynthesizesAllowedARecordsAndDropsBlockedRecords(t *testing.T) {
	queryer := &fakeQueryer{replies: map[queryKey]fakeReply{
		{endpoint: "primary", name: "v4-only.example.", record: TypeAAAA}: {result: QueryResult{TTL: time.Hour}},
		{endpoint: "primary", name: "v4-only.example.", record: TypeA}: {
			result: QueryResult{Addresses: []netip.Addr{
				netip.MustParseAddr("10.0.0.1"),
				netip.MustParseAddr("8.8.8.8"),
			}, TTL: time.Minute},
		},
	}}
	resolver, err := NewResolver(testEndpoints()[:1], queryer, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	prefix := netip.MustParsePrefix("64:ff9b::/96")
	result, err := resolver.Resolve(context.Background(), "v4-only.example", policy.DestinationPolicy{NAT64Prefix: prefix}, policy.ULAInherit, prefix)
	if err != nil {
		t.Fatal(err)
	}
	want := netip.MustParseAddr("64:ff9b::808:808")
	if !result.Synthesized || result.Source != "primary" || len(result.Addresses) != 1 || result.Addresses[0] != want {
		t.Fatalf("resolution = %#v, want synthesized %s", result, want)
	}
}

func TestResolverUsesAllowedAAAAWithoutFallingBackToA(t *testing.T) {
	queryer := &fakeQueryer{replies: map[queryKey]fakeReply{
		{endpoint: "primary", name: "mixed.example.", record: TypeAAAA}: {
			result: QueryResult{Addresses: []netip.Addr{
				netip.MustParseAddr("2001:db8::1"),
				netip.MustParseAddr("2606:4700:4700::1111"),
			}, TTL: time.Minute},
		},
	}}
	resolver, err := NewResolver(testEndpoints()[:1], queryer, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := resolver.Resolve(context.Background(), "mixed.example", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Addresses) != 1 || result.Addresses[0] != netip.MustParseAddr("2606:4700:4700::1111") {
		t.Fatalf("addresses = %v", result.Addresses)
	}
	if queryer.callCount() != 1 {
		t.Fatalf("A fallback occurred despite AAAA answer: %d calls", queryer.callCount())
	}
}

func TestResolverResynthesizesPublicDNS64AnswerWithActiveCustomPrefix(t *testing.T) {
	queryer := &fakeQueryer{replies: map[queryKey]fakeReply{
		{endpoint: "primary", name: "v4-only.example.", record: TypeAAAA}: {
			result: QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("64:ff9b::808:808")}, TTL: time.Minute},
		},
		{endpoint: "primary", name: "v4-only.example.", record: TypeA}: {
			result: QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}, TTL: time.Minute},
		},
	}}
	resolver, err := NewResolver(testEndpoints()[:1], queryer, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	custom := netip.MustParsePrefix("2001:4860:64::/96")
	result, err := resolver.Resolve(context.Background(), "v4-only.example", policy.DestinationPolicy{}, policy.ULAInherit, custom)
	if err != nil {
		t.Fatal(err)
	}
	want := netip.MustParseAddr("2001:4860:64::808:808")
	if !result.Synthesized || len(result.Addresses) != 1 || result.Addresses[0] != want {
		t.Fatalf("resolution = %#v, want synthesized %s", result, want)
	}
}

func TestResolverReappliesPolicyToCachedAnswers(t *testing.T) {
	address := netip.MustParseAddr("2606:4700:4700::1111")
	queryer := &fakeQueryer{replies: map[queryKey]fakeReply{
		{endpoint: "primary", name: "changing.example.", record: TypeAAAA}: {
			result: QueryResult{Addresses: []netip.Addr{address}, TTL: time.Minute},
		},
	}}
	resolver, err := NewResolver(testEndpoints()[:1], queryer, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), "changing.example", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{}); err != nil {
		t.Fatal(err)
	}
	blocked := policy.DestinationPolicy{ManagedAddresses: map[netip.Addr]struct{}{address: {}}}
	if _, err := resolver.Resolve(context.Background(), "changing.example", blocked, policy.ULAInherit, netip.Prefix{}); !errors.Is(err, ErrNoAllowedAddresses) {
		t.Fatalf("Resolve() error = %v, want ErrNoAllowedAddresses", err)
	}
	if queryer.callCount() != 1 {
		t.Fatalf("cached answer was queried again: %d calls", queryer.callCount())
	}
}

func TestResolverHandlesIPLiteralWithoutDNSAndRequiresNAT64ForIPv4(t *testing.T) {
	queryer := &fakeQueryer{replies: make(map[queryKey]fakeReply)}
	resolver, err := NewResolver(testEndpoints()[:1], queryer, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), "8.8.8.8", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{}); !errors.Is(err, ErrNAT64Unavailable) {
		t.Fatalf("IPv4 Resolve() error = %v, want ErrNAT64Unavailable", err)
	}
	result, err := resolver.Resolve(context.Background(), "2606:4700:4700::1111", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{})
	if err != nil || len(result.Addresses) != 1 {
		t.Fatalf("IPv6 literal result=%#v error=%v", result, err)
	}
	if queryer.callCount() != 0 {
		t.Fatalf("literal resolution made %d DNS calls", queryer.callCount())
	}
}

func TestNewResolverRejectsInvalidEndpoints(t *testing.T) {
	tests := []Endpoint{
		{Name: "v4", Address: netip.MustParseAddr("1.1.1.1"), Port: 853, ServerName: "example"},
		{Name: "no-port", Address: netip.MustParseAddr("2606:4700:4700::64"), ServerName: "example"},
		{Name: "no-tls-name", Address: netip.MustParseAddr("2606:4700:4700::64"), Port: 853},
	}
	for _, endpoint := range tests {
		if _, err := NewResolver([]Endpoint{endpoint}, &fakeQueryer{}, time.Now); err == nil {
			t.Fatalf("NewResolver(%#v) error = nil", endpoint)
		}
	}
}

func TestResolverUpdateEndpointsCommitsAtomicallyAndClearsCache(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{replies: map[queryKey]fakeReply{
		{endpoint: "primary", name: "example.com.", record: TypeAAAA}: {
			result: QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111")}, TTL: time.Minute},
		},
		{endpoint: "replacement", name: "example.com.", record: TypeAAAA}: {
			result: QueryResult{Addresses: []netip.Addr{netip.MustParseAddr("2001:4860:4860::8888")}, TTL: time.Minute},
		},
	}}
	resolver, err := NewResolver(testEndpoints()[:1], queryer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), "example.com", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{}); err != nil {
		t.Fatal(err)
	}

	replacement := []Endpoint{{
		Name: "replacement", Address: netip.MustParseAddr("2001:4860:4860::6464"),
		Port: 853, ServerName: "dns.google",
	}}
	if err := resolver.UpdateEndpoints(replacement); err != nil {
		t.Fatalf("UpdateEndpoints() error = %v", err)
	}
	replacement[0].Name = "mutated"
	gotEndpoints := resolver.Endpoints()
	if len(gotEndpoints) != 1 || gotEndpoints[0].Name != "replacement" {
		t.Fatalf("Endpoints() = %#v", gotEndpoints)
	}
	gotEndpoints[0].Name = "also-mutated"
	if resolver.Endpoints()[0].Name != "replacement" {
		t.Fatal("Endpoints() returned aliased state")
	}

	result, err := resolver.Resolve(context.Background(), "example.com", policy.DestinationPolicy{}, policy.ULAInherit, netip.Prefix{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "replacement" || result.Addresses[0] != netip.MustParseAddr("2001:4860:4860::8888") {
		t.Fatalf("resolution after update = %#v", result)
	}
	if queryer.callCount() != 2 {
		t.Fatalf("query count = %d, want cache invalidated and queried twice", queryer.callCount())
	}
}

func TestResolverUpdateEndpointsRejectsInvalidCandidateWithoutChangingState(t *testing.T) {
	resolver, err := NewResolver(testEndpoints()[:1], &fakeQueryer{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	before := resolver.Endpoints()
	if err := resolver.UpdateEndpoints([]Endpoint{{Name: "v4", Address: netip.MustParseAddr("1.1.1.1"), Port: 853, ServerName: "dns.example"}}); err == nil {
		t.Fatal("UpdateEndpoints(invalid) error = nil")
	}
	if got := resolver.Endpoints(); len(got) != 1 || got[0] != before[0] {
		t.Fatalf("invalid update changed endpoints: before=%#v after=%#v", before, got)
	}
}

func testEndpoints() []Endpoint {
	return []Endpoint{
		{Name: "primary", Address: netip.MustParseAddr("2606:4700:4700::64"), Port: 853, ServerName: "cloudflare-dns.com"},
		{Name: "secondary", Address: netip.MustParseAddr("2001:4860:4860::6464"), Port: 853, ServerName: "dns.google"},
	}
}
