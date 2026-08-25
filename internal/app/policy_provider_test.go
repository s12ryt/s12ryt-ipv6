package app

import (
	"errors"
	"net/netip"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/config"
	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
)

func TestPolicyProviderRefreshesHostAndManagedAddresses(t *testing.T) {
	hosts := []netip.Addr{
		netip.MustParseAddr("127.0.0.1"),
		netip.MustParseAddr("2001:4860::10"),
		netip.MustParseAddr("::ffff:192.0.2.10"),
	}
	configuration := config.Default()
	configuration.AllowULA = true
	nat64 := netip.MustParsePrefix("64:ff9b::/96")
	provider, err := NewPolicyProvider(PolicyProviderOptions{
		ScanHostAddresses: func() ([]netip.Addr, error) {
			return append([]netip.Addr(nil), hosts...), nil
		},
		Configuration: func() config.Config { return configuration },
		NAT64Prefix:   func() netip.Prefix { return nat64 },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.RefreshHostAddresses(); err != nil {
		t.Fatal(err)
	}

	store := ipv6resource.NewStore()
	template, err := ipv6resource.NewPrefixTemplate("wan", "2001:4860:100::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddTemplate(template); err != nil {
		t.Fatal(err)
	}
	managed := netip.MustParseAddr("2001:4860:100::1")
	if _, err := store.CreateFixedAddress("fixed", "wan", managed, ipv6resource.OwnershipAddress); err != nil {
		t.Fatal(err)
	}
	if err := provider.Sync(store.State()); err != nil {
		t.Fatal(err)
	}

	snapshot := provider.Policy()
	if !snapshot.AllowULA || snapshot.NAT64Prefix != nat64 {
		t.Fatalf("policy settings = %#v", snapshot)
	}
	for _, address := range []netip.Addr{
		netip.MustParseAddr("127.0.0.1"),
		netip.MustParseAddr("2001:4860::10"),
		netip.MustParseAddr("192.0.2.10"),
	} {
		if _, exists := snapshot.LocalAddresses[address]; !exists {
			t.Fatalf("local address %s is missing", address)
		}
	}
	if _, exists := snapshot.ManagedAddresses[managed]; !exists {
		t.Fatalf("managed address %s is missing", managed)
	}

	// Policy() 回傳的地址集是唯讀共享視圖：呼叫方不得修改（零複製契約見
	// TestPolicyProviderPolicyReturnsZeroCopyViews）；未更新前重複呼叫內容穩定。
	if next := provider.Policy(); len(next.LocalAddresses) != 3 || len(next.ManagedAddresses) != 1 {
		t.Fatalf("policy snapshot drifted without updates: %#v", next)
	}

	configuration.AllowULA = false
	nat64 = netip.Prefix{}
	next := provider.Policy()
	if next.AllowULA || next.NAT64Prefix.IsValid() {
		t.Fatalf("dynamic settings were not refreshed: %#v", next)
	}
}

func TestPolicyProviderKeepsPreviousSnapshotWhenRefreshFails(t *testing.T) {
	wantErr := errors.New("interface scan failed")
	fail := false
	provider, err := NewPolicyProvider(PolicyProviderOptions{
		ScanHostAddresses: func() ([]netip.Addr, error) {
			if fail {
				return nil, wantErr
			}
			return []netip.Addr{netip.MustParseAddr("2001:4860::20")}, nil
		},
		Configuration: config.Default,
		NAT64Prefix:   func() netip.Prefix { return netip.Prefix{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.RefreshHostAddresses(); err != nil {
		t.Fatal(err)
	}
	fail = true
	if err := provider.RefreshHostAddresses(); !errors.Is(err, wantErr) {
		t.Fatalf("RefreshHostAddresses() error = %v", err)
	}
	if _, exists := provider.Policy().LocalAddresses[netip.MustParseAddr("2001:4860::20")]; !exists {
		t.Fatal("failed refresh replaced the last valid host snapshot")
	}
}

func TestPolicyProviderRejectsInvalidUpdatesAndDependencies(t *testing.T) {
	valid := PolicyProviderOptions{
		ScanHostAddresses: func() ([]netip.Addr, error) { return nil, nil },
		Configuration:     config.Default,
		NAT64Prefix:       func() netip.Prefix { return netip.Prefix{} },
	}
	invalid := []PolicyProviderOptions{
		{Configuration: valid.Configuration, NAT64Prefix: valid.NAT64Prefix},
		{ScanHostAddresses: valid.ScanHostAddresses, NAT64Prefix: valid.NAT64Prefix},
		{ScanHostAddresses: valid.ScanHostAddresses, Configuration: valid.Configuration},
	}
	for _, options := range invalid {
		if _, err := NewPolicyProvider(options); err == nil {
			t.Fatalf("NewPolicyProvider(%#v) error = nil", options)
		}
	}

	provider, err := NewPolicyProvider(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Sync(ipv6resource.State{Addresses: []ipv6resource.CanonicalAddress{{
		Address: netip.MustParseAddr("2001:4860::30"), References: 1,
	}}}); err == nil {
		t.Fatal("Sync(invalid state) error = nil")
	}
	if len(provider.Policy().ManagedAddresses) != 0 {
		t.Fatal("invalid state mutated managed addresses")
	}
}

// TestPolicyProviderPolicyReturnsZeroCopyViews 守護 Policy() 的零複製契約：
// 每個出站代理連線都會呼叫 Policy()，地址池規模龐大時逐次複製兩個地址集
// 是資料路徑成本；內部快照以 build-new-then-swap 發佈，發佈後即不可變，
// 因此 Policy() 直接回傳唯讀共享視圖，不複製。
func TestPolicyProviderPolicyReturnsZeroCopyViews(t *testing.T) {
	provider, err := NewPolicyProvider(PolicyProviderOptions{
		ScanHostAddresses: func() ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("2001:4860::40")}, nil
		},
		Configuration: config.Default,
		NAT64Prefix:   func() netip.Prefix { return netip.Prefix{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.RefreshHostAddresses(); err != nil {
		t.Fatal(err)
	}
	first := provider.Policy()
	second := provider.Policy()
	if reflect.ValueOf(first.LocalAddresses).Pointer() != reflect.ValueOf(second.LocalAddresses).Pointer() {
		t.Fatal("Policy() copied the local address set")
	}
	if reflect.ValueOf(first.ManagedAddresses).Pointer() != reflect.ValueOf(second.ManagedAddresses).Pointer() {
		t.Fatal("Policy() copied the managed address set")
	}
}

// TestPolicyProviderPublishedViewsSurviveLaterUpdates 守護 swap 語義：
// 已發佈的視圖在後續 Sync/RefreshHostAddresses 後必須維持原內容，
// 避免未來有人把更新改成就地修改共享 map。
func TestPolicyProviderPublishedViewsSurviveLaterUpdates(t *testing.T) {
	scan := []netip.Addr{netip.MustParseAddr("2001:4860::50")}
	provider, err := NewPolicyProvider(PolicyProviderOptions{
		ScanHostAddresses: func() ([]netip.Addr, error) {
			return append([]netip.Addr(nil), scan...), nil
		},
		Configuration: config.Default,
		NAT64Prefix:   func() netip.Prefix { return netip.Prefix{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.RefreshHostAddresses(); err != nil {
		t.Fatal(err)
	}
	first := netip.MustParseAddr("2001:4860:100::60")
	if err := provider.Sync(managedAddressState(t, first)); err != nil {
		t.Fatal(err)
	}
	published := provider.Policy()

	scan = []netip.Addr{netip.MustParseAddr("2001:4860::70")}
	if err := provider.RefreshHostAddresses(); err != nil {
		t.Fatal(err)
	}
	second := netip.MustParseAddr("2001:4860:100::80")
	if err := provider.Sync(managedAddressState(t, second)); err != nil {
		t.Fatal(err)
	}

	if _, exists := published.LocalAddresses[netip.MustParseAddr("2001:4860::50")]; !exists {
		t.Fatal("published local view lost its address after later updates")
	}
	if _, exists := published.ManagedAddresses[first]; !exists {
		t.Fatal("published managed view lost its address after later updates")
	}
	next := provider.Policy()
	if _, exists := next.LocalAddresses[netip.MustParseAddr("2001:4860::70")]; !exists {
		t.Fatal("latest view did not reflect the refreshed host addresses")
	}
	if _, exists := next.LocalAddresses[netip.MustParseAddr("2001:4860::50")]; exists {
		t.Fatal("latest view kept the stale host address")
	}
	if _, exists := next.ManagedAddresses[second]; !exists {
		t.Fatal("latest view did not reflect the synced managed addresses")
	}
	if _, exists := next.ManagedAddresses[first]; exists {
		t.Fatal("latest view kept the stale managed address")
	}
}

func managedAddressState(t *testing.T, address netip.Addr) ipv6resource.State {
	t.Helper()
	store := ipv6resource.NewStore()
	template, err := ipv6resource.NewPrefixTemplate("wan", "2001:4860:100::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddTemplate(template); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateFixedAddress("fixed", "wan", address, ipv6resource.OwnershipAddress); err != nil {
		t.Fatal(err)
	}
	return store.State()
}

// TestPolicyProviderConcurrentPolicySyncAndRefresh 以 -race 驗證
// Policy() 共享視圖與 Sync/RefreshHostAddresses 的併發安全。
func TestPolicyProviderConcurrentPolicySyncAndRefresh(t *testing.T) {
	scan := []netip.Addr{netip.MustParseAddr("2001:4860::90")}
	provider, err := NewPolicyProvider(PolicyProviderOptions{
		ScanHostAddresses: func() ([]netip.Addr, error) {
			return append([]netip.Addr(nil), scan...), nil
		},
		Configuration: config.Default,
		NAT64Prefix:   func() netip.Prefix { return netip.Prefix{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	states := []ipv6resource.State{
		managedAddressState(t, netip.MustParseAddr("2001:4860:100::a0")),
		managedAddressState(t, netip.MustParseAddr("2001:4860:100::a1")),
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if err := provider.RefreshHostAddresses(); err != nil {
					t.Errorf("RefreshHostAddresses() error = %v", err)
					return
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; ; i = (i + 1) % len(states) {
			select {
			case <-stop:
				return
			default:
				if err := provider.Sync(states[i]); err != nil {
					t.Errorf("Sync() error = %v", err)
					return
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				policy := provider.Policy()
				_ = len(policy.LocalAddresses) + len(policy.ManagedAddresses)
			}
		}
	}()
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}
