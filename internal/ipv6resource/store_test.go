package ipv6resource

import (
	"net/netip"
	"strings"
	"testing"
)

func TestStorePinsCanonicalAddressIntoPool(t *testing.T) {
	store := NewStore()
	template := mustTemplate(t, "edge", "2001:db8:10::/120")
	if err := store.AddTemplate(template); err != nil {
		t.Fatal(err)
	}
	fixed, err := store.CreateFixedAddress("egress-a", "edge", netip.MustParseAddr("2001:db8:10::8"), OwnershipAddress)
	if err != nil {
		t.Fatal(err)
	}

	pool, err := store.CreatePool("shared-a", PoolSharedOutbound, "edge", 3, []string{"egress-a"})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	if got, want := len(pool.Active), 3; got != want {
		t.Fatalf("len(pool.Active) = %d, want %d", got, want)
	}
	if pool.Active[0] != fixed.Address {
		t.Fatalf("first pool address = %s, want pinned %s", pool.Active[0], fixed.Address)
	}
	canonical := store.Address(fixed.Address)
	if canonical == nil || canonical.References != 2 {
		t.Fatalf("canonical references = %#v, want 2", canonical)
	}
}

func TestStoreRefreshPoolRetainsPinnedAndDrainsPreviousAutomaticAddresses(t *testing.T) {
	store := NewStore()
	if err := store.AddTemplate(mustTemplate(t, "edge", "2001:db8:20::/120")); err != nil {
		t.Fatal(err)
	}
	fixed, err := store.CreateFixedAddress("pinned", "edge", netip.MustParseAddr("2001:db8:20::f0"), OwnershipAddress)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.CreatePool("inbound", PoolInbound, "edge", 3, []string{"pinned"})
	if err != nil {
		t.Fatal(err)
	}
	oldAutomatic := append([]netip.Addr(nil), before.Active[1:]...)

	after, err := store.RefreshPool("inbound")
	if err != nil {
		t.Fatalf("RefreshPool() error = %v", err)
	}
	if after.Active[0] != fixed.Address {
		t.Fatalf("pinned address changed to %s", after.Active[0])
	}
	if len(after.Draining) != 1 {
		t.Fatalf("len(draining) = %d, want 1", len(after.Draining))
	}
	if !sameAddresses(after.Draining[0].Addresses, oldAutomatic) {
		t.Fatalf("draining addresses = %v, want %v", after.Draining[0].Addresses, oldAutomatic)
	}
	for _, old := range oldAutomatic {
		for _, active := range after.Active {
			if old == active {
				t.Fatalf("old automatic address %s remained active", old)
			}
		}
	}
}

func TestStoreRejectsDeletingReferencedTemplateAndAddress(t *testing.T) {
	store := NewStore()
	if err := store.AddTemplate(mustTemplate(t, "edge", "2001:db8:30::/120")); err != nil {
		t.Fatal(err)
	}
	address, err := store.CreateFixedAddress("fixed", "edge", netip.MustParseAddr("2001:db8:30::1"), OwnershipAddress)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool("shared", PoolSharedOutbound, "edge", 2, []string{"fixed"}); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteTemplate("edge"); err == nil || !strings.Contains(err.Error(), "shared") {
		t.Fatalf("DeleteTemplate() error = %v, want references", err)
	}
	if err := store.DeleteFixedAddress("fixed"); err == nil {
		t.Fatal("DeleteFixedAddress() error = nil, want pool reference error")
	}
	if got := store.Address(address.Address); got == nil {
		t.Fatal("referenced canonical address was removed")
	}
}

func TestStoreReleasesDrainingBatchOnlyWhenExplicitlyCompleted(t *testing.T) {
	store := NewStore()
	if err := store.AddTemplate(mustTemplate(t, "edge", "2001:db8:40::/120")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool("shared", PoolSharedOutbound, "edge", 2, nil); err != nil {
		t.Fatal(err)
	}
	pool, err := store.RefreshPool("shared")
	if err != nil {
		t.Fatal(err)
	}
	batchID := pool.Draining[0].ID
	draining := append([]netip.Addr(nil), pool.Draining[0].Addresses...)

	if err := store.CompleteDrain("shared", batchID); err != nil {
		t.Fatalf("CompleteDrain() error = %v", err)
	}
	if got := store.Pool("shared"); len(got.Draining) != 0 {
		t.Fatalf("draining batches remain: %v", got.Draining)
	}
	for _, address := range draining {
		if got := store.Address(address); got != nil {
			t.Fatalf("drained canonical address %s remains: %#v", address, got)
		}
	}
}

func TestStoreCompletesDrainingAddressesIndividually(t *testing.T) {
	store := NewStore()
	template, err := NewPrefixTemplate("wan", "2001:4860:20::/120", "eth0", ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddTemplate(template); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool("shared", PoolSharedOutbound, "wan", 2, nil); err != nil {
		t.Fatal(err)
	}
	refreshed, err := store.RefreshPool("shared")
	if err != nil {
		t.Fatal(err)
	}
	batch := refreshed.Draining[0]

	completed, err := store.CompleteDrainedAddress("shared", batch.Addresses[0])
	if err != nil {
		t.Fatal(err)
	}
	if completed {
		t.Fatal("first address unexpectedly completed the whole batch")
	}
	remaining := store.Pool("shared")
	if len(remaining.Draining) != 1 || len(remaining.Draining[0].Addresses) != 1 {
		t.Fatalf("remaining drain = %#v", remaining.Draining)
	}
	if store.Address(batch.Addresses[0]) != nil {
		t.Fatalf("drained address %s was not released", batch.Addresses[0])
	}

	completed, err = store.CompleteDrainedAddress("shared", batch.Addresses[1])
	if err != nil {
		t.Fatal(err)
	}
	if !completed || len(store.Pool("shared").Draining) != 0 {
		t.Fatalf("final drain did not remove batch: completed=%v pool=%#v", completed, store.Pool("shared"))
	}
	if _, err := store.CompleteDrainedAddress("shared", batch.Addresses[1]); err == nil {
		t.Fatal("completing an unknown drained address returned nil")
	}
}

func TestStoreListSnapshotsAreSortedAndCannotMutateState(t *testing.T) {
	store := NewStore()
	for _, template := range []PrefixTemplate{
		mustTemplate(t, "zeta", "2001:db8:2::/120"),
		mustTemplate(t, "alpha", "2001:db8:1::/120"),
	} {
		if err := store.AddTemplate(template); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.CreateFixedAddress("z-fixed", "zeta", netip.MustParseAddr("2001:db8:2::1"), OwnershipAddress); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateFixedAddress("a-fixed", "alpha", netip.MustParseAddr("2001:db8:1::1"), OwnershipAddress); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool("z-pool", PoolSharedOutbound, "zeta", 1, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool("a-pool", PoolInbound, "alpha", 1, nil); err != nil {
		t.Fatal(err)
	}

	templates := store.Templates()
	if len(templates) != 2 || templates[0].Name != "alpha" || templates[1].Name != "zeta" {
		t.Fatalf("Templates() = %#v", templates)
	}
	fixed := store.FixedAddresses()
	if len(fixed) != 2 || fixed[0].Name != "a-fixed" || fixed[1].Name != "z-fixed" {
		t.Fatalf("FixedAddresses() = %#v", fixed)
	}
	pools := store.Pools()
	if len(pools) != 2 || pools[0].Name != "a-pool" || pools[1].Name != "z-pool" {
		t.Fatalf("Pools() = %#v", pools)
	}
	addresses := store.Addresses()
	if len(addresses) != 4 || addresses[0].Address.String() != "2001:db8:1::" {
		t.Fatalf("Addresses() = %#v", addresses)
	}

	pools[0].Active[0] = netip.MustParseAddr("2001:db8:ffff::1")
	if got := store.Pool("a-pool"); got.Active[0].String() != "2001:db8:1::" {
		t.Fatalf("mutated pool snapshot changed store: %#v", got)
	}
}

func TestStoreDeletePoolReleasesActivePinnedAndDrainingReferences(t *testing.T) {
	store := NewStore()
	if err := store.AddTemplate(mustTemplate(t, "edge", "2001:db8:50::/120")); err != nil {
		t.Fatal(err)
	}
	fixed, err := store.CreateFixedAddress("fixed", "edge", netip.MustParseAddr("2001:db8:50::f0"), OwnershipAddress)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.CreatePool("shared", PoolSharedOutbound, "edge", 3, []string{"fixed"})
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := store.RefreshPool("shared")
	if err != nil {
		t.Fatal(err)
	}
	allAutomatic := append([]netip.Addr(nil), before.Active[1:]...)
	allAutomatic = append(allAutomatic, refreshed.Active[1:]...)

	if err := store.DeletePool("shared"); err != nil {
		t.Fatalf("DeletePool() error = %v", err)
	}
	if store.Pool("shared") != nil {
		t.Fatal("deleted pool remains")
	}
	if got := store.Address(fixed.Address); got == nil || got.References != 1 {
		t.Fatalf("fixed canonical after pool deletion = %#v, want one fixed reference", got)
	}
	for _, address := range allAutomatic {
		if got := store.Address(address); got != nil {
			t.Fatalf("automatic address %s remains after pool deletion: %#v", address, got)
		}
	}
	if err := store.DeletePool("shared"); err == nil {
		t.Fatal("DeletePool(missing) succeeded")
	}
}

func mustTemplate(t *testing.T, name, cidr string) PrefixTemplate {
	t.Helper()
	template, err := NewPrefixTemplate(name, cidr, "eth0", ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	return template
}

func sameAddresses(left, right []netip.Addr) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
