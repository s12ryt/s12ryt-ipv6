package node

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type queueListener struct {
	endpoint proxy.BindEndpoint
	incoming chan net.Conn
	closed   chan struct{}
	once     sync.Once
}

func newQueueListener(endpoint proxy.BindEndpoint) *queueListener {
	return &queueListener{endpoint: endpoint, incoming: make(chan net.Conn, 8), closed: make(chan struct{})}
}

func (l *queueListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.incoming:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *queueListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *queueListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IP(l.endpoint.Address.AsSlice()), Port: int(l.endpoint.Port)}
}

type listenerBinder struct {
	mu        sync.Mutex
	listeners []*queueListener
	binds     []proxy.BindEndpoint
}

func (b *listenerBinder) Bind(_ context.Context, endpoint proxy.BindEndpoint) (io.Closer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	listener := newQueueListener(endpoint)
	b.binds = append(b.binds, endpoint)
	b.listeners = append(b.listeners, listener)
	return listener, nil
}

type fakeNodeFirewall struct {
	mu         sync.Mutex
	opened     map[string][]proxy.BindEndpoint
	openCalls  [][]proxy.BindEndpoint
	closed     []string
	openError  error
	closeError error
}

func (f *fakeNodeFirewall) OpenNode(_ context.Context, id string, endpoints []proxy.BindEndpoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.opened == nil {
		f.opened = make(map[string][]proxy.BindEndpoint)
	}
	f.opened[id] = append([]proxy.BindEndpoint(nil), endpoints...)
	f.openCalls = append(f.openCalls, append([]proxy.BindEndpoint(nil), endpoints...))
	return f.openError
}

func (f *fakeNodeFirewall) CloseNode(_ context.Context, id string, _ []proxy.BindEndpoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, id)
	return f.closeError
}

type readBlockingHandler struct {
	entered chan struct{}
	once    sync.Once
}

func (h *readBlockingHandler) ServeConn(_ context.Context, conn net.Conn) (proxy.ProxyTraffic, error) {
	h.once.Do(func() { close(h.entered) })
	_, err := conn.Read(make([]byte, 1))
	return proxy.ProxyTraffic{Protocol: "socks"}, err
}

type sequenceHandlerBuilder struct {
	mu       sync.Mutex
	handlers []proxy.ConnectionProxy
}

func (b *sequenceHandlerBuilder) Build(Config) (proxy.ConnectionProxy, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.handlers) == 0 {
		return nil, errors.New("no handler queued")
	}
	handler := b.handlers[0]
	b.handlers = b.handlers[1:]
	return handler, nil
}

type blockingHandler struct {
	entered chan struct{}
	once    sync.Once
}

type multiReadHandler struct {
	entered chan struct{}
}

func (h *multiReadHandler) ServeConn(_ context.Context, conn net.Conn) (proxy.ProxyTraffic, error) {
	h.entered <- struct{}{}
	_, err := conn.Read(make([]byte, 1))
	return proxy.ProxyTraffic{Protocol: "socks"}, err
}

func (h *blockingHandler) ServeConn(ctx context.Context, _ net.Conn) (proxy.ProxyTraffic, error) {
	h.once.Do(func() { close(h.entered) })
	<-ctx.Done()
	return proxy.ProxyTraffic{Protocol: "socks"}, ctx.Err()
}

type staticHandlerBuilder struct {
	handler proxy.ConnectionProxy
	err     error
	calls   int
}

func (b *staticHandlerBuilder) Build(Config) (proxy.ConnectionProxy, error) {
	b.calls++
	return b.handler, b.err
}

func runtimeConfig() Config {
	return Config{
		ID: "node-1", Name: "primary", Protocol: ProtocolSOCKS,
		Username: "alice", Password: "correct-horse-battery",
		Inbound: []proxy.BindSpec{{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: netip.MustParseAddr("2001:4860:1::10")}},
		MaxTCP:  1, MaxUDP: 1, DialTimeout: time.Second, HandshakeTimeout: time.Second,
		UDPIdleTimeout: time.Minute, ULAOverride: policy.ULAInherit, Outbound: "fixed-primary",
	}
}

func TestListenerRuntimeStartsListenersEnforcesLimitAndStopsConnections(t *testing.T) {
	binder := &listenerBinder{}
	allocator, err := proxy.NewPortAllocator(52000, 52000, binder)
	if err != nil {
		t.Fatal(err)
	}
	handler := &blockingHandler{entered: make(chan struct{})}
	builder := &staticHandlerBuilder{handler: handler}
	firewall := &fakeNodeFirewall{}
	events := make(chan TrafficEvent, 4)
	factory, err := NewListenerRuntimeFactory(ListenerRuntimeOptions{
		Allocator: allocator, Handlers: builder, Firewall: firewall,
		Observe: func(event TrafficEvent) { events <- event },
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := factory.Start(context.Background(), runtimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Port() != 52000 {
		t.Fatalf("runtime port = %d", runtime.Port())
	}
	if len(binder.listeners) != 1 || !reflect.DeepEqual(firewall.opened["node-1"], binder.binds) {
		t.Fatalf("listeners/openings = %d/%#v, binds %#v", len(binder.listeners), firewall.opened, binder.binds)
	}

	firstClient, firstServer := net.Pipe()
	binder.listeners[0].incoming <- firstServer
	select {
	case <-handler.entered:
	case <-time.After(time.Second):
		t.Fatal("first connection was not handled")
	}
	select {
	case event := <-events:
		if event.Lifecycle != TrafficTCPOpened || event.NodeID != "node-1" {
			t.Fatalf("open event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for TCP open event")
	}
	secondClient, secondServer := net.Pipe()
	binder.listeners[0].incoming <- secondServer
	select {
	case event := <-events:
		if event.Lifecycle != TrafficTCPRejected || !errors.Is(event.Error, ErrTCPConnectionLimit) || !event.Rejected {
			t.Fatalf("limit event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for limit event")
	}
	_ = secondClient.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := secondClient.Read(make([]byte, 1)); err == nil {
		t.Fatal("rejected connection remained open")
	}

	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Lifecycle != TrafficTCPClosed || event.NodeID != "node-1" || event.Traffic.Protocol != "socks" {
			t.Fatalf("close event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for TCP close event")
	}
	if !reflect.DeepEqual(firewall.closed, []string{"node-1"}) {
		t.Fatalf("closed firewall nodes = %#v", firewall.closed)
	}
	select {
	case <-binder.listeners[0].closed:
	default:
		t.Fatal("listener was not closed")
	}
	_ = firstClient.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := firstClient.Read(make([]byte, 1)); err == nil {
		t.Fatal("active connection remained open")
	}
	_ = firstClient.Close()
	_ = secondClient.Close()
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop() = %v", err)
	}
}

func TestListenerRuntimeRollsBackBindingsWhenFirewallOpenFails(t *testing.T) {
	binder := &listenerBinder{}
	allocator, _ := proxy.NewPortAllocator(52000, 52000, binder)
	factory, err := NewListenerRuntimeFactory(ListenerRuntimeOptions{
		Allocator: allocator,
		Handlers:  &staticHandlerBuilder{handler: &blockingHandler{entered: make(chan struct{})}},
		Firewall:  &fakeNodeFirewall{openError: errors.New("nft failed")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factory.Start(context.Background(), runtimeConfig()); err == nil {
		t.Fatal("Start() error = nil")
	}
	if len(binder.listeners) != 1 {
		t.Fatalf("listener count = %d", len(binder.listeners))
	}
	select {
	case <-binder.listeners[0].closed:
	default:
		t.Fatal("failed firewall transaction leaked listener")
	}
}

func TestListenerRuntimeBuildsHandlerBeforeBinding(t *testing.T) {
	binder := &listenerBinder{}
	allocator, _ := proxy.NewPortAllocator(52000, 52000, binder)
	factory, err := NewListenerRuntimeFactory(ListenerRuntimeOptions{
		Allocator: allocator, Handlers: &staticHandlerBuilder{err: errors.New("invalid handler")}, Firewall: &fakeNodeFirewall{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factory.Start(context.Background(), runtimeConfig()); err == nil {
		t.Fatal("Start() error = nil")
	}
	if len(binder.binds) != 0 {
		t.Fatalf("binds = %#v", binder.binds)
	}
	if _, err := NewListenerRuntimeFactory(ListenerRuntimeOptions{}); err == nil {
		t.Fatal("NewListenerRuntimeFactory(empty) error = nil")
	}
}

func TestListenerRuntimeReplacesHandlerOnSameBindingsWithoutRebinding(t *testing.T) {
	binder := &listenerBinder{}
	allocator, _ := proxy.NewPortAllocator(52000, 52000, binder)
	firstHandler := &readBlockingHandler{entered: make(chan struct{})}
	secondHandler := &readBlockingHandler{entered: make(chan struct{})}
	factory, err := NewListenerRuntimeFactory(ListenerRuntimeOptions{
		Allocator: allocator,
		Handlers:  &sequenceHandlerBuilder{handlers: []proxy.ConnectionProxy{firstHandler, secondHandler}},
		Firewall:  &fakeNodeFirewall{},
	})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(factory, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	created, err := manager.Create(context.Background(), runtimeConfig(), false)
	if err != nil {
		t.Fatal(err)
	}
	firstClient, firstServer := net.Pipe()
	binder.listeners[0].incoming <- firstServer
	select {
	case <-firstHandler.entered:
	case <-time.After(time.Second):
		t.Fatal("first handler did not start")
	}

	replacement := created.Config
	replacement.Name = "replacement"
	updated, err := manager.Update(context.Background(), replacement.ID, replacement, false)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Config.Name != "replacement" || updated.Config.Port != 52000 {
		t.Fatalf("updated node = %#v", updated)
	}
	if len(binder.binds) != 1 {
		t.Fatalf("same endpoint update rebound %d sockets", len(binder.binds))
	}
	_ = firstClient.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := firstClient.Read(make([]byte, 1)); err == nil {
		t.Fatal("old connection remained open after handler replacement")
	}

	secondClient, secondServer := net.Pipe()
	binder.listeners[0].incoming <- secondServer
	select {
	case <-secondHandler.entered:
	case <-time.After(time.Second):
		t.Fatal("replacement handler did not receive new connection")
	}
	if _, err := manager.Stop(context.Background(), replacement.ID); err != nil {
		t.Fatal(err)
	}
	_ = firstClient.Close()
	_ = secondClient.Close()
}

func TestListenerRuntimeRefreshesBindingsAndDrainsRemovedConnections(t *testing.T) {
	binder := &listenerBinder{}
	allocator, _ := proxy.NewPortAllocator(52000, 52000, binder)
	handler := &multiReadHandler{entered: make(chan struct{}, 4)}
	firewall := &fakeNodeFirewall{}
	factory, err := NewListenerRuntimeFactory(ListenerRuntimeOptions{
		Allocator: allocator, Handlers: &staticHandlerBuilder{handler: handler}, Firewall: firewall,
	})
	if err != nil {
		t.Fatal(err)
	}
	first := netip.MustParseAddr("2001:4860:1::10")
	kept := netip.MustParseAddr("2001:4860:1::11")
	added := netip.MustParseAddr("2001:4860:1::12")
	config := runtimeConfig()
	config.MaxTCP = 4
	config.Inbound = []proxy.BindSpec{
		{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: first},
		{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: kept},
	}
	runtime, err := factory.Start(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	binder.listeners[0].incoming <- server
	select {
	case <-handler.entered:
	case <-time.After(time.Second):
		t.Fatal("removed endpoint connection did not start")
	}

	refresher, ok := runtime.(BindingRefreshRuntime)
	if !ok {
		t.Fatal("listener runtime does not support binding refresh")
	}
	refreshed := config
	refreshed.Port = 52000
	refreshed.Inbound = []proxy.BindSpec{
		{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: kept},
		{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: added},
	}
	drained := make(chan proxy.BindEndpoint, 1)
	if err := refresher.RefreshBindings(context.Background(), refreshed, func(endpoint proxy.BindEndpoint) {
		drained <- endpoint
	}); err != nil {
		t.Fatal(err)
	}
	if len(binder.binds) != 3 {
		t.Fatalf("bind count = %d, want only one added listener", len(binder.binds))
	}
	select {
	case <-binder.listeners[0].closed:
	default:
		t.Fatal("removed listener still accepts connections")
	}
	select {
	case <-binder.listeners[1].closed:
		t.Fatal("retained listener was closed")
	default:
	}
	select {
	case endpoint := <-drained:
		t.Fatalf("endpoint %v drained before its active connection closed", endpoint)
	default:
	}

	addedClient, addedServer := net.Pipe()
	binder.listeners[2].incoming <- addedServer
	select {
	case <-handler.entered:
	case <-time.After(time.Second):
		t.Fatal("new listener did not accept connections")
	}
	_ = client.Close()
	select {
	case endpoint := <-drained:
		if endpoint.Address != first {
			t.Fatalf("drained endpoint = %v", endpoint)
		}
	case <-time.After(time.Second):
		t.Fatal("removed endpoint was not reported drained")
	}
	if len(firewall.openCalls) != 2 || len(firewall.openCalls[1]) != 2 {
		t.Fatalf("firewall generations = %#v", firewall.openCalls)
	}
	_ = addedClient.Close()
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestListenerRuntimeBindingRefreshRollsBackNewListenerWhenFirewallFails(t *testing.T) {
	binder := &listenerBinder{}
	allocator, _ := proxy.NewPortAllocator(52000, 52000, binder)
	firewall := &fakeNodeFirewall{}
	factory, _ := NewListenerRuntimeFactory(ListenerRuntimeOptions{
		Allocator: allocator,
		Handlers:  &staticHandlerBuilder{handler: &blockingHandler{entered: make(chan struct{})}},
		Firewall:  firewall,
	})
	config := runtimeConfig()
	runtime, err := factory.Start(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	firewall.openError = errors.New("replace failed")
	refreshed := config
	refreshed.Port = 52000
	refreshed.Inbound = append(refreshed.Inbound, proxy.BindSpec{
		Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: netip.MustParseAddr("2001:4860:1::11"),
	})
	if err := runtime.(BindingRefreshRuntime).RefreshBindings(context.Background(), refreshed, nil); err == nil {
		t.Fatal("RefreshBindings() error = nil")
	}
	if len(binder.listeners) != 2 {
		t.Fatalf("listener count = %d", len(binder.listeners))
	}
	select {
	case <-binder.listeners[1].closed:
	default:
		t.Fatal("failed refresh leaked added listener")
	}
	select {
	case <-binder.listeners[0].closed:
		t.Fatal("failed refresh closed existing listener")
	default:
	}
	firewall.openError = nil
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestListenerRuntimeForceClosesConnectionsOnRetiringAddresses(t *testing.T) {
	binder := &listenerBinder{}
	allocator, _ := proxy.NewPortAllocator(52000, 52000, binder)
	handler := &multiReadHandler{entered: make(chan struct{}, 2)}
	factory, err := NewListenerRuntimeFactory(ListenerRuntimeOptions{
		Allocator: allocator, Handlers: &staticHandlerBuilder{handler: handler}, Firewall: &fakeNodeFirewall{},
	})
	if err != nil {
		t.Fatal(err)
	}
	retiring := netip.MustParseAddr("2001:4860:1::10")
	current := netip.MustParseAddr("2001:4860:1::11")
	config := runtimeConfig()
	config.MaxTCP = 2
	config.Inbound = []proxy.BindSpec{{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: retiring}}
	runtime, err := factory.Start(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	oldClient, oldServer := net.Pipe()
	binder.listeners[0].incoming <- oldServer
	select {
	case <-handler.entered:
	case <-time.After(time.Second):
		t.Fatal("retiring connection did not start")
	}
	refreshed := config
	refreshed.Port = runtime.Port()
	refreshed.Inbound = []proxy.BindSpec{{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: current}}
	drained := make(chan proxy.BindEndpoint, 1)
	if err := runtime.(BindingRefreshRuntime).RefreshBindings(context.Background(), refreshed, func(endpoint proxy.BindEndpoint) {
		drained <- endpoint
	}); err != nil {
		t.Fatal(err)
	}
	newClient, newServer := net.Pipe()
	binder.listeners[1].incoming <- newServer
	select {
	case <-handler.entered:
	case <-time.After(time.Second):
		t.Fatal("current connection did not start")
	}

	drainer, ok := runtime.(BindingDrainRuntime)
	if !ok {
		t.Fatal("listener runtime does not support forced binding drain")
	}
	if err := drainer.ForceDrainBindings([]netip.Addr{retiring}); err != nil {
		t.Fatal(err)
	}
	_ = oldClient.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := oldClient.Read(make([]byte, 1)); err == nil {
		t.Fatal("forced retiring connection remained open")
	}
	select {
	case endpoint := <-drained:
		if endpoint.Address != retiring {
			t.Fatalf("drained endpoint = %v", endpoint)
		}
	case <-time.After(time.Second):
		t.Fatal("forced retiring endpoint was not reported drained")
	}
	_ = newClient.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := newClient.Write([]byte{1}); err != nil {
		t.Fatalf("current connection was closed: %v", err)
	}
	if err := drainer.ForceDrainBindings([]netip.Addr{retiring}); err != nil {
		t.Fatalf("second ForceDrainBindings() = %v", err)
	}
	_ = newClient.Close()
	_ = oldClient.Close()
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
