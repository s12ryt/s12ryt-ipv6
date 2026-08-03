package node

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type stateSynchronizer struct {
	name  string
	log   *[]string
	err   error
	state ipv6resource.State
}

func (s *stateSynchronizer) Sync(state ipv6resource.State) error {
	*s.log = append(*s.log, s.name)
	if s.err != nil {
		return s.err
	}
	s.state = state
	return nil
}

type syncingInboundResolver struct {
	stateSynchronizer
	resolved Config
}

func (r *syncingInboundResolver) Resolve(Config) (Config, error) {
	return cloneConfig(r.resolved), nil
}

type bindingRefresher struct {
	log      *[]string
	nodes    []Node
	resolver InboundConfigResolver
	callback InboundDrainedObserver
	err      error
}

func (r *bindingRefresher) List() []Node { return append([]Node(nil), r.nodes...) }

func (r *bindingRefresher) RefreshInboundBindings(_ context.Context, resolver InboundConfigResolver, callback InboundDrainedObserver) error {
	*r.log = append(*r.log, "nodes")
	r.resolver = resolver
	r.callback = callback
	return r.err
}

type drainPreparer struct {
	log   *[]string
	state ipv6resource.State
	nodes []Node
	err   error
}

func (p *drainPreparer) Prepare(state ipv6resource.State, nodes []Node) error {
	*p.log = append(*p.log, "drains")
	if p.err != nil {
		return p.err
	}
	p.state = state
	p.nodes = append([]Node(nil), nodes...)
	return nil
}

func TestRuntimeResourceSynchronizerUpdatesRegistriesBeforeNodes(t *testing.T) {
	log := []string{}
	policy := &stateSynchronizer{name: "policy", log: &log}
	outbound := &stateSynchronizer{name: "outbound", log: &log}
	inbound := &syncingInboundResolver{stateSynchronizer: stateSynchronizer{name: "inbound", log: &log}, resolved: validConfig("node-a", "Node A")}
	nodes := &bindingRefresher{log: &log, nodes: []Node{{Config: validConfig("node-a", "Node A"), Status: StatusRunning}}}
	drains := &drainPreparer{log: &log}
	type drainedEvent struct {
		node, resource string
		address        netip.Addr
	}
	var drained []drainedEvent
	synchronizer, err := NewRuntimeResourceSynchronizer(RuntimeResourceSynchronizerOptions{
		Policy:   policy,
		Outbound: outbound,
		Inbound:  inbound,
		Nodes:    nodes,
		Drains:   drains,
		Timeout:  time.Second,
		OnInboundDrained: func(nodeID, resource string, endpoint proxy.BindEndpoint) {
			drained = append(drained, drainedEvent{node: nodeID, resource: resource, address: endpoint.Address})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := ipv6resource.State{NextBatch: 3}
	if err := synchronizer.Sync(state); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(log, []string{"drains", "policy", "outbound", "inbound", "nodes"}) {
		t.Fatalf("unexpected sync order: %v", log)
	}
	if len(drains.nodes) != 1 || drains.nodes[0].Config.ID != "node-a" {
		t.Fatalf("drain preparation nodes = %#v", drains.nodes)
	}
	if nodes.resolver != inbound {
		t.Fatal("node refresher did not receive synchronized inbound resolver")
	}
	endpoint := proxy.BindEndpoint{Address: netip.MustParseAddr("2001:4860::1")}
	nodes.callback("node-a", "pool-in", endpoint)
	if len(drained) != 1 || drained[0].node != "node-a" || drained[0].resource != "pool-in" || drained[0].address != endpoint.Address {
		t.Fatalf("unexpected drain forwarding: %#v", drained)
	}
}

func TestRuntimeResourceSynchronizerStopsAtFirstFailure(t *testing.T) {
	log := []string{}
	policy := &stateSynchronizer{name: "policy", log: &log}
	outbound := &stateSynchronizer{name: "outbound", log: &log}
	inbound := &syncingInboundResolver{stateSynchronizer: stateSynchronizer{name: "inbound", log: &log, err: errors.New("invalid resources")}}
	nodes := &bindingRefresher{log: &log}
	drains := &drainPreparer{log: &log}
	synchronizer, err := NewRuntimeResourceSynchronizer(RuntimeResourceSynchronizerOptions{
		Policy: policy, Outbound: outbound, Inbound: inbound, Nodes: nodes, Drains: drains, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := synchronizer.Sync(ipv6resource.State{}); err == nil {
		t.Fatal("expected inbound sync error")
	}
	if !reflect.DeepEqual(log, []string{"drains", "policy", "outbound", "inbound"}) {
		t.Fatalf("nodes ran after registry failure: %v", log)
	}
}

func TestRuntimeResourceSynchronizerValidatesOptions(t *testing.T) {
	log := []string{}
	policy := &stateSynchronizer{name: "policy", log: &log}
	outbound := &stateSynchronizer{name: "outbound", log: &log}
	inbound := &syncingInboundResolver{stateSynchronizer: stateSynchronizer{name: "inbound", log: &log}}
	nodes := &bindingRefresher{log: &log}
	drains := &drainPreparer{log: &log}
	tests := []RuntimeResourceSynchronizerOptions{
		{Outbound: outbound, Inbound: inbound, Nodes: nodes, Drains: drains, Timeout: time.Second},
		{Policy: policy, Inbound: inbound, Nodes: nodes, Drains: drains, Timeout: time.Second},
		{Policy: policy, Outbound: outbound, Nodes: nodes, Drains: drains, Timeout: time.Second},
		{Policy: policy, Outbound: outbound, Inbound: inbound, Drains: drains, Timeout: time.Second},
		{Policy: policy, Outbound: outbound, Inbound: inbound, Nodes: nodes, Timeout: time.Second},
		{Policy: policy, Outbound: outbound, Inbound: inbound, Nodes: nodes, Drains: drains},
	}
	for _, options := range tests {
		if _, err := NewRuntimeResourceSynchronizer(options); err == nil {
			t.Fatalf("expected invalid options error: %#v", options)
		}
	}
}
