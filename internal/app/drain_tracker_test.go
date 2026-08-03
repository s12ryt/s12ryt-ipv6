package app

import (
	"net/netip"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/node"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type completedDrain struct {
	pool    string
	address netip.Addr
}

func TestDrainTrackerWaitsForEveryRunningInboundConsumer(t *testing.T) {
	var completed []completedDrain
	tracker, err := NewDrainTracker(func(pool string, address netip.Addr) {
		completed = append(completed, completedDrain{pool: pool, address: address})
	})
	if err != nil {
		t.Fatal(err)
	}
	address := netip.MustParseAddr("2001:4860:1::10")
	state := drainingPoolState(t, ipv6resource.PoolInbound, "pool-in", address)
	nodes := []node.Node{
		{Config: drainingNodeConfig("node-a", "pool-in"), Status: node.StatusRunning},
		{Config: drainingNodeConfig("node-b", "pool-in"), Status: node.StatusRunning},
		{Config: drainingNodeConfig("node-stopped", "pool-in"), Status: node.StatusStopped},
	}
	if err := tracker.Prepare(state, nodes); err != nil {
		t.Fatal(err)
	}
	endpoint := proxy.BindEndpoint{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: address, Port: 55000}
	tracker.InboundDrained("node-a", "pool-in", endpoint)
	tracker.InboundDrained("node-a", "pool-in", endpoint)
	if len(completed) != 0 {
		t.Fatalf("completed before every consumer drained: %#v", completed)
	}
	tracker.InboundDrained("node-b", "pool-in", endpoint)
	if len(completed) != 1 || completed[0] != (completedDrain{pool: "pool-in", address: address}) {
		t.Fatalf("unexpected completion: %#v", completed)
	}
}

func TestDrainTrackerUsesOneAggregateOutboundConsumer(t *testing.T) {
	var completed []completedDrain
	tracker, err := NewDrainTracker(func(pool string, address netip.Addr) {
		completed = append(completed, completedDrain{pool: pool, address: address})
	})
	if err != nil {
		t.Fatal(err)
	}
	address := netip.MustParseAddr("2001:4860:1::20")
	state := drainingPoolState(t, ipv6resource.PoolSharedOutbound, "pool-out", address)
	if err := tracker.Prepare(state, nil); err != nil {
		t.Fatal(err)
	}
	tracker.OutboundDrained("pool-out", address)
	tracker.OutboundDrained("pool-out", address)
	if len(completed) != 1 {
		t.Fatalf("outbound completion count = %d", len(completed))
	}
}

func TestDrainTrackerImmediatelyCompletesUnusedInboundAndIgnoresStaleSignals(t *testing.T) {
	var completed []completedDrain
	tracker, err := NewDrainTracker(func(pool string, address netip.Addr) {
		completed = append(completed, completedDrain{pool: pool, address: address})
	})
	if err != nil {
		t.Fatal(err)
	}
	address := netip.MustParseAddr("2001:4860:1::30")
	state := drainingPoolState(t, ipv6resource.PoolInbound, "pool-in", address)
	if err := tracker.Prepare(state, nil); err != nil {
		t.Fatal(err)
	}
	if len(completed) != 1 {
		t.Fatalf("unused inbound completion count = %d", len(completed))
	}

	if err := tracker.Prepare(ipv6resource.State{}, nil); err != nil {
		t.Fatal(err)
	}
	tracker.InboundDrained("old-node", "pool-in", proxy.BindEndpoint{Address: address})
	tracker.OutboundDrained("pool-in", address)
	if len(completed) != 1 {
		t.Fatalf("stale signal completed a removed drain: %#v", completed)
	}
}

func TestDrainTrackerValidatesStateAndCallback(t *testing.T) {
	if _, err := NewDrainTracker(nil); err == nil {
		t.Fatal("expected nil callback error")
	}
	tracker, err := NewDrainTracker(func(string, netip.Addr) {})
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.Prepare(ipv6resource.State{Pools: []ipv6resource.Pool{{Name: "broken", Kind: ipv6resource.PoolInbound, Template: "missing", Capacity: 1}}}, nil); err == nil {
		t.Fatal("expected invalid state error")
	}
}

func drainingNodeConfig(id, resource string) node.Config {
	return node.Config{ID: id, InboundMode: node.InboundIPv6, InboundResource: resource}
}

func drainingPoolState(t *testing.T, kind ipv6resource.PoolKind, name string, draining netip.Addr) ipv6resource.State {
	t.Helper()
	store := ipv6resource.NewStore()
	template, err := ipv6resource.NewPrefixTemplate("wan", "2001:4860:1::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddTemplate(template); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePool(name, kind, template.Name, 1, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RefreshPool(name); err != nil {
		t.Fatal(err)
	}
	state := store.State()
	found := false
	for _, pool := range state.Pools {
		if pool.Name != name || len(pool.Draining) != 1 || len(pool.Draining[0].Addresses) != 1 {
			continue
		}
		old := pool.Draining[0].Addresses[0]
		for index := range state.Addresses {
			if state.Addresses[index].Address == old {
				state.Addresses[index].Address = draining
			}
		}
		for poolIndex := range state.Pools {
			if state.Pools[poolIndex].Name == name {
				state.Pools[poolIndex].Draining[0].Addresses[0] = draining
			}
		}
		found = true
		break
	}
	if !found {
		t.Fatal("test pool did not produce a draining address")
	}
	return state
}
