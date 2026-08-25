package node

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/firewall"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

const (
	testRelayPortMin uint16 = 49152
	testRelayPortMax uint16 = 65535
)

type fakeRulesetReplacer struct {
	applied [][]firewall.Opening
	err     error
}

func (f *fakeRulesetReplacer) Replace(_ context.Context, openings []firewall.Opening) error {
	if f.err != nil {
		return f.err
	}
	f.applied = append(f.applied, append([]firewall.Opening(nil), openings...))
	return nil
}

func tcp6Endpoint(address string, port uint16) proxy.BindEndpoint {
	return proxy.BindEndpoint{
		Protocol: proxy.BindTCP,
		Family:   proxy.BindIPv6,
		Address:  netip.MustParseAddr(address),
		Port:     port,
	}
}

func udp6Endpoint(address string, port uint16) proxy.BindEndpoint {
	return proxy.BindEndpoint{
		Protocol: proxy.BindUDP,
		Family:   proxy.BindIPv6,
		Address:  netip.MustParseAddr(address),
		Port:     port,
	}
}

func TestFirewallCoordinatorCombinesManagementNodesAndRelays(t *testing.T) {
	replacer := &fakeRulesetReplacer{}
	management := []proxy.BindEndpoint{
		{Protocol: proxy.BindTCP, Family: proxy.BindIPv4, Port: 34466},
		{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Port: 34466},
	}
	coordinator, err := NewFirewallCoordinator(context.Background(), replacer, management, testRelayPortMin, testRelayPortMax)
	if err != nil {
		t.Fatal(err)
	}
	nodeA := []proxy.BindEndpoint{tcp6Endpoint("2001:4860:1::10", 52000)}
	nodeB := []proxy.BindEndpoint{tcp6Endpoint("2001:4860:1::11", 52000)}
	relay := udp6Endpoint("2001:4860:1::10", 53000)
	if err := coordinator.OpenNode(context.Background(), "node-a", nodeA); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.OpenNode(context.Background(), "node-b", nodeB); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Open(context.Background(), relay); err != nil {
		t.Fatal(err)
	}
	state := coordinator.State()
	if len(state) != 5 {
		t.Fatalf("opening count = %d, state %#v", len(state), state)
	}
	wantPurposes := map[string]int{"management": 2, "node:node-a": 1, "node:node-b": 1, "udp-relay": 1}
	gotPurposes := make(map[string]int)
	for _, opening := range state {
		gotPurposes[opening.Purpose]++
	}
	if !reflect.DeepEqual(gotPurposes, wantPurposes) {
		t.Fatalf("purposes = %#v, want %#v", gotPurposes, wantPurposes)
	}
	var relayOpening firewall.Opening
	found := false
	for _, opening := range state {
		if opening.Purpose == "udp-relay" {
			relayOpening = opening
			found = true
		}
	}
	if !found ||
		relayOpening.Protocol != firewall.ProtocolUDP ||
		relayOpening.Family != firewall.FamilyIPv6 ||
		relayOpening.Address != netip.MustParseAddr("2001:4860:1::10") ||
		relayOpening.Port != testRelayPortMin ||
		relayOpening.PortEnd != testRelayPortMax {
		t.Fatalf("relay opening = %#v, want aggregated UDP range on the relay address", relayOpening)
	}
}

func TestFirewallCoordinatorIgnoresCloseFromStaleNodeGeneration(t *testing.T) {
	replacer := &fakeRulesetReplacer{}
	coordinator, err := NewFirewallCoordinator(context.Background(), replacer, nil, testRelayPortMin, testRelayPortMax)
	if err != nil {
		t.Fatal(err)
	}
	oldEndpoints := []proxy.BindEndpoint{tcp6Endpoint("2001:4860:1::10", 52000)}
	newEndpoints := []proxy.BindEndpoint{tcp6Endpoint("2001:4860:1::11", 52001)}
	if err := coordinator.OpenNode(context.Background(), "node-a", oldEndpoints); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.OpenNode(context.Background(), "node-a", newEndpoints); err != nil {
		t.Fatal(err)
	}
	applyCount := len(replacer.applied)
	if err := coordinator.CloseNode(context.Background(), "node-a", oldEndpoints); err != nil {
		t.Fatal(err)
	}
	if len(replacer.applied) != applyCount {
		t.Fatal("stale generation close rewrote firewall rules")
	}
	state := coordinator.State()
	if len(state) != 1 || state[0].Address != newEndpoints[0].Address || state[0].Port != newEndpoints[0].Port {
		t.Fatalf("state after stale close = %#v", state)
	}
	if err := coordinator.CloseNode(context.Background(), "node-a", newEndpoints); err != nil {
		t.Fatal(err)
	}
	if len(coordinator.State()) != 0 {
		t.Fatalf("current generation was not removed: %#v", coordinator.State())
	}
}

func TestFirewallCoordinatorKeepsCommittedStateWhenApplyFails(t *testing.T) {
	replacer := &fakeRulesetReplacer{}
	coordinator, err := NewFirewallCoordinator(context.Background(), replacer, nil, testRelayPortMin, testRelayPortMax)
	if err != nil {
		t.Fatal(err)
	}
	replacer.err = errors.New("nft transaction failed")
	failed := []proxy.BindEndpoint{tcp6Endpoint("2001:4860:1::10", 52000)}
	if err := coordinator.OpenNode(context.Background(), "node-a", failed); err == nil {
		t.Fatal("OpenNode() error = nil")
	}
	replacer.err = nil
	relay := udp6Endpoint("2001:4860:1::20", 53000)
	if err := coordinator.Open(context.Background(), relay); err != nil {
		t.Fatal(err)
	}
	state := coordinator.State()
	if len(state) != 1 || state[0].Purpose != "udp-relay" {
		t.Fatalf("failed node update leaked into committed state: %#v", state)
	}
}

func TestFirewallCoordinatorReferenceCountsRelayScopesAcrossPorts(t *testing.T) {
	replacer := &fakeRulesetReplacer{}
	coordinator, err := NewFirewallCoordinator(context.Background(), replacer, nil, testRelayPortMin, testRelayPortMax)
	if err != nil {
		t.Fatal(err)
	}
	first := udp6Endpoint("2001:4860:1::20", 53000)
	second := udp6Endpoint("2001:4860:1::20", 54000)
	if err := coordinator.Open(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	afterFirstOpen := len(replacer.applied)
	if err := coordinator.Open(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if len(replacer.applied) != afterFirstOpen {
		t.Fatalf("second association on the same address rewrote rules: before=%d after=%d", afterFirstOpen, len(replacer.applied))
	}
	if err := coordinator.Close(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if len(replacer.applied) != afterFirstOpen {
		t.Fatal("non-final relay close rewrote firewall rules")
	}
	if state := coordinator.State(); len(state) != 1 || state[0].Purpose != "udp-relay" {
		t.Fatalf("state after partial close = %#v", state)
	}
	if err := coordinator.Close(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if len(replacer.applied) != afterFirstOpen+1 {
		t.Fatal("final relay close did not rewrite firewall rules")
	}
	if len(coordinator.State()) != 0 {
		t.Fatal("last relay close did not remove opening")
	}
}

func TestFirewallCoordinatorTracksRelayScopesPerAddress(t *testing.T) {
	replacer := &fakeRulesetReplacer{}
	coordinator, err := NewFirewallCoordinator(context.Background(), replacer, nil, testRelayPortMin, testRelayPortMax)
	if err != nil {
		t.Fatal(err)
	}
	alpha := udp6Endpoint("2001:4860:1::30", 53000)
	beta := udp6Endpoint("2001:4860:1::31", 54000)
	wildcard := proxy.BindEndpoint{Protocol: proxy.BindUDP, Family: proxy.BindIPv6, Port: 55000}
	for _, endpoint := range []proxy.BindEndpoint{alpha, beta, wildcard} {
		if err := coordinator.Open(context.Background(), endpoint); err != nil {
			t.Fatal(err)
		}
	}
	state := coordinator.State()
	if len(state) != 3 {
		t.Fatalf("relay opening count = %d, state %#v", len(state), state)
	}
	for _, opening := range state {
		if opening.Purpose != "udp-relay" ||
			opening.Protocol != firewall.ProtocolUDP ||
			opening.Port != testRelayPortMin ||
			opening.PortEnd != testRelayPortMax {
			t.Fatalf("relay opening = %#v, want one aggregated range per address", opening)
		}
	}
	if err := coordinator.Close(context.Background(), alpha); err != nil {
		t.Fatal(err)
	}
	if got := len(coordinator.State()); got != 2 {
		t.Fatalf("openings after closing alpha = %d, want 2", got)
	}
	if err := coordinator.Close(context.Background(), beta); err != nil {
		t.Fatal(err)
	}
	if got := len(coordinator.State()); got != 1 {
		t.Fatalf("openings after closing beta = %d, want 1", got)
	}
	if err := coordinator.Close(context.Background(), wildcard); err != nil {
		t.Fatal(err)
	}
	if got := len(coordinator.State()); got != 0 {
		t.Fatalf("openings after closing wildcard = %d, want 0", got)
	}
}

func TestFirewallCoordinatorValidatesConstruction(t *testing.T) {
	management := []proxy.BindEndpoint{{Protocol: proxy.BindUDP, Family: proxy.BindIPv4, Port: 34466}}
	if _, err := NewFirewallCoordinator(context.Background(), nil, nil, testRelayPortMin, testRelayPortMax); err == nil {
		t.Fatal("NewFirewallCoordinator(nil) error = nil")
	}
	if _, err := NewFirewallCoordinator(context.Background(), &fakeRulesetReplacer{}, management, testRelayPortMin, testRelayPortMax); err == nil {
		t.Fatal("UDP management endpoint error = nil")
	}
	cases := []struct {
		name     string
		min, max uint16
	}{
		{name: "zero min", min: 0, max: testRelayPortMax},
		{name: "zero max", min: testRelayPortMin, max: 0},
		{name: "inverted range", min: testRelayPortMax, max: testRelayPortMin},
	}
	for _, testCase := range cases {
		if _, err := NewFirewallCoordinator(context.Background(), &fakeRulesetReplacer{}, nil, testCase.min, testCase.max); err == nil {
			t.Fatalf("%s: error = nil", testCase.name)
		}
	}
}
