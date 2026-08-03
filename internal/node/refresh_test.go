package node

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type refreshRuntime struct {
	config    Config
	callbacks []func(proxy.BindEndpoint)
	forced    [][]netip.Addr
	err       error
}

func (r *refreshRuntime) Port() uint16 { return 55000 }
func (r *refreshRuntime) Stop(context.Context) error {
	return nil
}
func (r *refreshRuntime) RefreshBindings(_ context.Context, config Config, callback func(proxy.BindEndpoint)) error {
	if r.err != nil {
		return r.err
	}
	r.config = cloneConfig(config)
	r.callbacks = append(r.callbacks, callback)
	return nil
}

func (r *refreshRuntime) ForceDrainBindings(addresses []netip.Addr) error {
	r.forced = append(r.forced, append([]netip.Addr(nil), addresses...))
	return r.err
}

type refreshFactory struct {
	runtimes map[string]*refreshRuntime
}

func (f *refreshFactory) Start(_ context.Context, config Config) (Runtime, error) {
	runtime := &refreshRuntime{}
	f.runtimes[config.ID] = runtime
	return runtime, nil
}

type perNodeResolver struct {
	configs map[string]Config
	errID   string
}

func (r *perNodeResolver) Resolve(config Config) (Config, error) {
	if config.ID == r.errID {
		return Config{}, errors.New("resource unavailable")
	}
	return cloneConfig(r.configs[config.ID]), nil
}

func TestManagerRefreshesRunningDeclarativeInboundBindings(t *testing.T) {
	factory := &refreshFactory{runtimes: make(map[string]*refreshRuntime)}
	manager, err := NewManager(factory, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	first := declarativeConfig("node-a", "pool-a")
	second := declarativeConfig("node-b", "pool-b")
	stopped := declarativeConfig("node-c", "pool-c")
	if _, err := manager.Create(context.Background(), first, false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(context.Background(), second, false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(context.Background(), stopped, false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Stop(context.Background(), stopped.ID); err != nil {
		t.Fatal(err)
	}
	resolvedA := cloneConfig(first)
	resolvedA.Inbound = []proxy.BindSpec{{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: netip.MustParseAddr("2001:4860:1::1")}}
	resolvedB := cloneConfig(second)
	resolvedB.Inbound = []proxy.BindSpec{{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: netip.MustParseAddr("2001:4860:1::2")}}
	resolver := &perNodeResolver{configs: map[string]Config{first.ID: resolvedA, second.ID: resolvedB}}
	type drainedCall struct {
		node, resource string
		endpoint       proxy.BindEndpoint
	}
	var drained []drainedCall
	err = manager.RefreshInboundBindings(context.Background(), resolver, func(nodeID, resource string, endpoint proxy.BindEndpoint) {
		drained = append(drained, drainedCall{node: nodeID, resource: resource, endpoint: endpoint})
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := factory.runtimes[first.ID].config.Inbound[0].Address; got != netip.MustParseAddr("2001:4860:1::1") {
		t.Fatalf("first runtime got %s", got)
	}
	if got := factory.runtimes[second.ID].config.Inbound[0].Address; got != netip.MustParseAddr("2001:4860:1::2") {
		t.Fatalf("second runtime got %s", got)
	}
	if len(factory.runtimes[stopped.ID].callbacks) != 0 {
		t.Fatal("stopped node was refreshed")
	}
	factory.runtimes[first.ID].callbacks[0](proxy.BindEndpoint{Address: netip.MustParseAddr("2001:4860:1::ff")})
	if len(drained) != 1 || drained[0].node != first.ID || drained[0].resource != "pool-a" {
		t.Fatalf("unexpected drained callback: %#v", drained)
	}
	stored, _ := manager.Get(first.ID)
	if len(stored.Config.Inbound) != 0 || stored.Config.InboundResource != "pool-a" {
		t.Fatalf("manager replaced declaration with resolved bindings: %#v", stored.Config)
	}
}

func TestManagerPreflightsEveryInboundBeforeRefreshingRuntimes(t *testing.T) {
	factory := &refreshFactory{runtimes: make(map[string]*refreshRuntime)}
	manager, err := NewManager(factory, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	first := declarativeConfig("node-a", "pool-a")
	second := declarativeConfig("node-b", "pool-b")
	if _, err := manager.Create(context.Background(), first, false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(context.Background(), second, false); err != nil {
		t.Fatal(err)
	}
	resolver := &perNodeResolver{configs: map[string]Config{first.ID: first}, errID: second.ID}
	if err := manager.RefreshInboundBindings(context.Background(), resolver, nil); err == nil {
		t.Fatal("expected preflight resolution error")
	}
	if len(factory.runtimes[first.ID].callbacks) != 0 || len(factory.runtimes[second.ID].callbacks) != 0 {
		t.Fatal("a runtime was refreshed before every declaration passed preflight")
	}
}

func TestManagerRefreshInboundBindingsValidatesResolver(t *testing.T) {
	manager, err := NewManager(&refreshFactory{runtimes: make(map[string]*refreshRuntime)}, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RefreshInboundBindings(context.Background(), nil, nil); err == nil {
		t.Fatal("expected nil resolver error")
	}
}

func TestManagerForceDrainsOnlyRunningNodesUsingInboundPool(t *testing.T) {
	factory := &refreshFactory{runtimes: make(map[string]*refreshRuntime)}
	manager, err := NewManager(factory, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	first := declarativeConfig("node-a", "pool-a")
	second := declarativeConfig("node-b", "pool-b")
	stopped := declarativeConfig("node-c", "pool-a")
	for _, config := range []Config{first, second, stopped} {
		if _, err := manager.Create(context.Background(), config, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := manager.Stop(context.Background(), stopped.ID); err != nil {
		t.Fatal(err)
	}
	addresses := []netip.Addr{netip.MustParseAddr("2001:4860:1::10")}
	if err := manager.ForceDrainInbound(context.Background(), "pool-a", addresses); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(factory.runtimes[first.ID].forced, [][]netip.Addr{addresses}) {
		t.Fatalf("first forced addresses = %#v", factory.runtimes[first.ID].forced)
	}
	if len(factory.runtimes[second.ID].forced) != 0 || len(factory.runtimes[stopped.ID].forced) != 0 {
		t.Fatal("force drain reached unrelated or stopped node")
	}
}

func declarativeConfig(id, resource string) Config {
	config := validConfig(id, id)
	config.Inbound = nil
	config.InboundMode = InboundIPv6
	config.InboundResource = resource
	return config
}
