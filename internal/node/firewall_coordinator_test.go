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
	coordinator, err := NewFirewallCoordinator(context.Background(), replacer, management)
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
}

func TestFirewallCoordinatorIgnoresCloseFromStaleNodeGeneration(t *testing.T) {
	replacer := &fakeRulesetReplacer{}
	coordinator, err := NewFirewallCoordinator(context.Background(), replacer, nil)
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
	coordinator, err := NewFirewallCoordinator(context.Background(), replacer, nil)
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

func TestFirewallCoordinatorReferenceCountsRelayOpenings(t *testing.T) {
	replacer := &fakeRulesetReplacer{}
	coordinator, err := NewFirewallCoordinator(context.Background(), replacer, nil)
	if err != nil {
		t.Fatal(err)
	}
	relay := udp6Endpoint("2001:4860:1::20", 53000)
	if err := coordinator.Open(context.Background(), relay); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Open(context.Background(), relay); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(context.Background(), relay); err != nil {
		t.Fatal(err)
	}
	if len(coordinator.State()) != 1 {
		t.Fatal("first relay close removed a referenced opening")
	}
	if err := coordinator.Close(context.Background(), relay); err != nil {
		t.Fatal(err)
	}
	if len(coordinator.State()) != 0 {
		t.Fatal("last relay close did not remove opening")
	}
}

func TestFirewallCoordinatorValidatesConstruction(t *testing.T) {
	management := []proxy.BindEndpoint{{Protocol: proxy.BindUDP, Family: proxy.BindIPv4, Port: 34466}}
	if _, err := NewFirewallCoordinator(context.Background(), nil, nil); err == nil {
		t.Fatal("NewFirewallCoordinator(nil) error = nil")
	}
	if _, err := NewFirewallCoordinator(context.Background(), &fakeRulesetReplacer{}, management); err == nil {
		t.Fatal("UDP management endpoint error = nil")
	}
}
