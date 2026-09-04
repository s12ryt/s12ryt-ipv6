package node

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

var ErrTCPConnectionLimit = errors.New("node TCP connection limit reached")

type TrafficLifecycle string

const (
	TrafficTCPOpened   TrafficLifecycle = "tcp_opened"
	TrafficTCPClosed   TrafficLifecycle = "tcp_closed"
	TrafficTCPRejected TrafficLifecycle = "tcp_rejected"
	TrafficUDPOpened   TrafficLifecycle = "udp_opened"
	TrafficUDPClosed   TrafficLifecycle = "udp_closed"
)

type HandlerBuilder interface {
	Build(Config) (proxy.ConnectionProxy, error)
}

type NodeFirewall interface {
	OpenNode(context.Context, string, []proxy.BindEndpoint) error
	CloseNode(context.Context, string, []proxy.BindEndpoint) error
}

type TrafficEvent struct {
	Lifecycle TrafficLifecycle
	NodeID    string
	SourceIP  netip.Addr
	Traffic   proxy.ProxyTraffic
	Error     error
	Rejected  bool
}

type ListenerRuntimeOptions struct {
	Allocator *proxy.PortAllocator
	Handlers  HandlerBuilder
	Firewall  NodeFirewall
	Observe   func(TrafficEvent)
}

type ListenerRuntimeFactory struct {
	allocator *proxy.PortAllocator
	handlers  HandlerBuilder
	firewall  NodeFirewall
	observe   func(TrafficEvent)
}

func NewListenerRuntimeFactory(options ListenerRuntimeOptions) (*ListenerRuntimeFactory, error) {
	if options.Allocator == nil {
		return nil, errors.New("listener runtime port allocator is required")
	}
	if options.Handlers == nil {
		return nil, errors.New("listener runtime handler builder is required")
	}
	if options.Firewall == nil {
		return nil, errors.New("listener runtime firewall is required")
	}
	return &ListenerRuntimeFactory{
		allocator: options.Allocator, handlers: options.Handlers,
		firewall: options.Firewall, observe: options.Observe,
	}, nil
}

func (f *ListenerRuntimeFactory) Start(ctx context.Context, config Config) (Runtime, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	handler, err := f.handlers.Build(config)
	if err != nil {
		return nil, fmt.Errorf("build node handler: %w", err)
	}
	if handler == nil {
		return nil, errors.New("node handler builder returned nil")
	}
	reservation, err := f.allocator.Reserve(ctx, config.Port, config.Inbound)
	if err != nil {
		return nil, fmt.Errorf("reserve node listeners: %w", err)
	}
	bindings := reservation.Bindings()
	listeners := make([]net.Listener, 0, len(bindings))
	endpoints := make([]proxy.BindEndpoint, 0, len(bindings))
	for _, binding := range bindings {
		listener, ok := binding.Socket.(net.Listener)
		if !ok {
			_ = reservation.Close()
			return nil, errors.New("node TCP binding is not a listener")
		}
		listeners = append(listeners, listener)
		endpoints = append(endpoints, binding.Endpoint)
	}
	if err := f.firewall.OpenNode(ctx, config.ID, endpoints); err != nil {
		_ = reservation.Close()
		return nil, fmt.Errorf("open node firewall: %w", err)
	}
	runtimeCtx, cancel := context.WithCancel(context.Background())
	listenersByEndpoint := make(map[proxy.BindEndpoint]net.Listener, len(listeners))
	for index, endpoint := range endpoints {
		listenersByEndpoint[endpoint] = listeners[index]
	}
	runtime := &listenerRuntime{
		config: config, handler: handler, firewall: f.firewall, observe: f.observe,
		allocator: f.allocator, reservations: []*proxy.PortReservation{reservation}, listeners: listenersByEndpoint,
		endpoints: endpoints, ctx: runtimeCtx, cancel: cancel,
		active: make(map[net.Conn]proxy.BindEndpoint), retiring: make(map[proxy.BindEndpoint]func(proxy.BindEndpoint)),
		stopped: make(chan struct{}),
	}
	for index, listener := range listeners {
		runtime.wg.Add(1)
		go runtime.accept(listener, endpoints[index])
	}
	return runtime, nil
}

func (f *ListenerRuntimeFactory) Replace(ctx context.Context, current Runtime, config Config) (Runtime, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	runtime, ok := current.(*listenerRuntime)
	if !ok || !sameBindings(runtime.endpoints, config.Port, config.Inbound) {
		return f.Start(ctx, config)
	}
	port := runtime.Port()
	config.Port = port
	runtime.mu.Lock()
	if runtime.stopping {
		runtime.mu.Unlock()
		return nil, errors.New("node runtime is stopping")
	}
	if sameRuntimeConfigExceptMetadata(runtime.config, config) {
		runtime.config = config
		runtime.mu.Unlock()
		return runtime, nil
	}
	runtime.mu.Unlock()

	handler, err := f.handlers.Build(config)
	if err != nil {
		return nil, fmt.Errorf("build replacement node handler: %w", err)
	}
	if handler == nil {
		return nil, errors.New("node handler builder returned nil")
	}

	runtime.mu.Lock()
	if runtime.stopping {
		runtime.mu.Unlock()
		return nil, errors.New("node runtime is stopping")
	}
	runtime.config = config
	runtime.handler = handler
	active := make([]net.Conn, 0, len(runtime.active))
	for conn := range runtime.active {
		active = append(active, conn)
		delete(runtime.active, conn)
	}
	runtime.mu.Unlock()

	for _, conn := range active {
		_ = conn.Close()
	}
	return runtime, nil
}

func sameRuntimeConfigExceptMetadata(left, right Config) bool {
	return left.ID == right.ID &&
		left.Protocol == right.Protocol &&
		left.Username == right.Username &&
		left.Password == right.Password &&
		left.MaxTCP == right.MaxTCP &&
		left.MaxUDP == right.MaxUDP &&
		left.DialTimeout == right.DialTimeout &&
		left.HandshakeTimeout == right.HandshakeTimeout &&
		left.TunnelIdleTimeout == right.TunnelIdleTimeout &&
		left.UDPIdleTimeout == right.UDPIdleTimeout &&
		left.ULAOverride == right.ULAOverride &&
		left.Outbound == right.Outbound &&
		left.DedicatedPool == right.DedicatedPool &&
		left.InboundMode == right.InboundMode &&
		left.InboundResource == right.InboundResource &&
		slices.Equal(left.Inbound, right.Inbound)
}

type listenerRuntime struct {
	config       Config
	handler      proxy.ConnectionProxy
	firewall     NodeFirewall
	observe      func(TrafficEvent)
	allocator    *proxy.PortAllocator
	reservations []*proxy.PortReservation
	listeners    map[proxy.BindEndpoint]net.Listener
	endpoints    []proxy.BindEndpoint
	ctx          context.Context
	cancel       context.CancelFunc

	refreshMu sync.Mutex
	mu        sync.Mutex
	active    map[net.Conn]proxy.BindEndpoint
	retiring  map[proxy.BindEndpoint]func(proxy.BindEndpoint)
	stopping  bool
	wg        sync.WaitGroup
	stopOnce  sync.Once
	stopErr   error
	stopped   chan struct{}
}

func (r *listenerRuntime) Port() uint16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.reservations) == 0 {
		return 0
	}
	return r.reservations[0].Port()
}

func (r *listenerRuntime) Stop(ctx context.Context) error {
	r.stopOnce.Do(func() {
		r.refreshMu.Lock()
		r.mu.Lock()
		r.stopping = true
		active := make([]net.Conn, 0, len(r.active))
		for conn := range r.active {
			active = append(active, conn)
		}
		r.mu.Unlock()

		firewallErr := r.firewall.CloseNode(ctx, r.config.ID, append([]proxy.BindEndpoint(nil), r.endpoints...))
		var reservationErrors []error
		for _, reservation := range r.reservations {
			reservationErrors = append(reservationErrors, reservation.Close())
		}
		r.cancel()
		for _, conn := range active {
			_ = conn.Close()
		}
		r.refreshMu.Unlock()
		r.wg.Wait()
		r.stopErr = errors.Join(firewallErr, errors.Join(reservationErrors...))
		close(r.stopped)
	})
	<-r.stopped
	return r.stopErr
}

func (r *listenerRuntime) accept(listener net.Listener, endpoint proxy.BindEndpoint) {
	defer r.wg.Done()
	var tempDelay time.Duration
	for {
		conn, err := listener.Accept()
		if err != nil {
			var netErr net.Error
			if !errors.As(err, &netErr) || !netErr.Temporary() {
				return
			}
			if tempDelay == 0 {
				tempDelay = 5 * time.Millisecond
			} else {
				tempDelay *= 2
			}
			if tempDelay > time.Second {
				tempDelay = time.Second
			}
			timer := time.NewTimer(tempDelay)
			select {
			case <-timer.C:
			case <-r.ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			}
			continue
		}
		tempDelay = 0
		r.mu.Lock()
		if r.stopping {
			r.mu.Unlock()
			_ = conn.Close()
			continue
		}
		nodeID := r.config.ID
		if len(r.active) >= r.config.MaxTCP {
			r.mu.Unlock()
			source := remoteIP(conn.RemoteAddr())
			_ = conn.Close()
			r.emit(TrafficEvent{Lifecycle: TrafficTCPRejected, NodeID: nodeID, SourceIP: source, Error: ErrTCPConnectionLimit, Rejected: true})
			continue
		}
		handler := r.handler
		r.active[conn] = endpoint
		r.wg.Add(1)
		r.mu.Unlock()
		r.emit(TrafficEvent{Lifecycle: TrafficTCPOpened, NodeID: nodeID, SourceIP: remoteIP(conn.RemoteAddr())})
		go r.serve(conn, handler, nodeID)
	}
}

func (r *listenerRuntime) serve(conn net.Conn, handler proxy.ConnectionProxy, nodeID string) {
	defer r.wg.Done()
	defer func() {
		_ = conn.Close()
		r.mu.Lock()
		endpoint := r.active[conn]
		delete(r.active, conn)
		callback := r.drainedCallbackLocked(endpoint)
		r.mu.Unlock()
		if callback != nil {
			callback(endpoint)
		}
	}()
	traffic, err := r.dispatch(handler, conn)
	r.emit(TrafficEvent{Lifecycle: TrafficTCPClosed, NodeID: nodeID, SourceIP: remoteIP(conn.RemoteAddr()), Traffic: traffic, Error: err})
}

// dispatch isolates protocol handler panics so a single malformed peer
// connection can never take down the whole process; the panic is converted
// into a per-connection error and the runtime keeps serving.
func (r *listenerRuntime) dispatch(handler proxy.ConnectionProxy, conn net.Conn) (traffic proxy.ProxyTraffic, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			traffic = proxy.ProxyTraffic{}
			err = fmt.Errorf("proxy handler panicked: %v", recovered)
		}
	}()
	return handler.ServeConn(r.ctx, conn)
}

func (r *listenerRuntime) RefreshBindings(ctx context.Context, config Config, onDrained func(proxy.BindEndpoint)) error {
	if err := config.Validate(); err != nil {
		return err
	}
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()

	port := r.Port()
	if config.Port != 0 && config.Port != port {
		return errors.New("binding refresh cannot change the node port")
	}
	config.Port = port
	desired, err := bindEndpoints(config.Inbound, port)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if r.stopping {
		r.mu.Unlock()
		return errors.New("node runtime is stopping")
	}
	current := append([]proxy.BindEndpoint(nil), r.endpoints...)
	r.mu.Unlock()

	currentSet := make(map[proxy.BindEndpoint]struct{}, len(current))
	for _, endpoint := range current {
		currentSet[endpoint] = struct{}{}
	}
	desiredSet := make(map[proxy.BindEndpoint]struct{}, len(desired))
	addedSpecs := make([]proxy.BindSpec, 0)
	for _, endpoint := range desired {
		desiredSet[endpoint] = struct{}{}
		if _, exists := currentSet[endpoint]; !exists {
			addedSpecs = append(addedSpecs, proxy.BindSpec{
				Protocol: endpoint.Protocol, Family: endpoint.Family, Address: endpoint.Address,
				Interface: endpoint.Interface, Freebind: endpoint.Freebind,
			})
		}
	}
	removed := make([]proxy.BindEndpoint, 0)
	for _, endpoint := range current {
		if _, exists := desiredSet[endpoint]; !exists {
			removed = append(removed, endpoint)
		}
	}

	var addedReservation *proxy.PortReservation
	addedListeners := make(map[proxy.BindEndpoint]net.Listener)
	if len(addedSpecs) > 0 {
		addedReservation, err = r.allocator.Reserve(ctx, port, addedSpecs)
		if err != nil {
			return fmt.Errorf("reserve refreshed node listeners: %w", err)
		}
		for _, binding := range addedReservation.Bindings() {
			listener, ok := binding.Socket.(net.Listener)
			if !ok {
				_ = addedReservation.Close()
				return errors.New("refreshed node TCP binding is not a listener")
			}
			addedListeners[binding.Endpoint] = listener
		}
	}
	if err := r.firewall.OpenNode(ctx, config.ID, desired); err != nil {
		if addedReservation != nil {
			_ = addedReservation.Close()
		}
		return fmt.Errorf("open refreshed node firewall: %w", err)
	}

	r.mu.Lock()
	if r.stopping {
		r.mu.Unlock()
		if addedReservation != nil {
			_ = addedReservation.Close()
		}
		return errors.New("node runtime is stopping")
	}
	if addedReservation != nil {
		r.reservations = append(r.reservations, addedReservation)
	}
	for endpoint, listener := range addedListeners {
		r.listeners[endpoint] = listener
	}
	for _, endpoint := range removed {
		delete(r.listeners, endpoint)
		r.retiring[endpoint] = onDrained
	}
	r.endpoints = append([]proxy.BindEndpoint(nil), desired...)
	r.config = config
	r.mu.Unlock()

	for endpoint, listener := range addedListeners {
		r.wg.Add(1)
		go r.accept(listener, endpoint)
	}
	var releaseErrors []error
	for _, endpoint := range removed {
		for _, reservation := range r.reservations {
			if !reservationContains(reservation, endpoint) {
				continue
			}
			if err := reservation.ReleaseEndpoints([]proxy.BindEndpoint{endpoint}); err != nil {
				releaseErrors = append(releaseErrors, err)
			}
			break
		}
		r.mu.Lock()
		callback := r.drainedCallbackLocked(endpoint)
		r.mu.Unlock()
		if callback != nil {
			callback(endpoint)
		}
	}
	return errors.Join(releaseErrors...)
}

func (r *listenerRuntime) ForceDrainBindings(addresses []netip.Addr) error {
	wanted := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !address.IsValid() || !address.Is6() || address.Is4In6() {
			return errors.New("draining inbound address must be native IPv6")
		}
		wanted[address] = struct{}{}
	}
	if len(wanted) == 0 {
		return errors.New("at least one draining inbound address is required")
	}

	r.mu.Lock()
	connections := make([]net.Conn, 0)
	for connection, endpoint := range r.active {
		if _, retiring := r.retiring[endpoint]; !retiring {
			continue
		}
		if _, selected := wanted[endpoint.Address.Unmap()]; selected {
			connections = append(connections, connection)
		}
	}
	r.mu.Unlock()
	var failures []error
	for _, connection := range connections {
		if err := connection.Close(); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (r *listenerRuntime) drainedCallbackLocked(endpoint proxy.BindEndpoint) func(proxy.BindEndpoint) {
	callback, retiring := r.retiring[endpoint]
	if !retiring || r.stopping {
		return nil
	}
	for _, activeEndpoint := range r.active {
		if activeEndpoint == endpoint {
			return nil
		}
	}
	delete(r.retiring, endpoint)
	return callback
}

func reservationContains(reservation *proxy.PortReservation, endpoint proxy.BindEndpoint) bool {
	for _, binding := range reservation.Bindings() {
		if binding.Endpoint == endpoint {
			return true
		}
	}
	return false
}

func bindEndpoints(specs []proxy.BindSpec, port uint16) ([]proxy.BindEndpoint, error) {
	if len(specs) == 0 {
		return nil, errors.New("at least one bind specification is required")
	}
	result := make([]proxy.BindEndpoint, 0, len(specs))
	for _, spec := range specs {
		if spec.Protocol != proxy.BindTCP {
			return nil, errors.New("node inbound listeners must use TCP")
		}
		if spec.Family != proxy.BindIPv4 && spec.Family != proxy.BindIPv6 {
			return nil, fmt.Errorf("unsupported bind family %q", spec.Family)
		}
		address := spec.Address
		if address.IsValid() {
			address = address.Unmap()
			if (spec.Family == proxy.BindIPv4 && !address.Is4()) ||
				(spec.Family == proxy.BindIPv6 && (!address.Is6() || address.Is4In6())) {
				return nil, fmt.Errorf("address %s does not match bind family %q", address, spec.Family)
			}
		}
		candidate := proxy.BindEndpoint{
			Protocol: spec.Protocol, Family: spec.Family, Address: address, Port: port,
			Interface: spec.Interface, Freebind: spec.Freebind,
		}
		for _, existing := range result {
			if candidate.Protocol == existing.Protocol && candidate.Family == existing.Family &&
				(!candidate.Address.IsValid() || !existing.Address.IsValid() || candidate.Address == existing.Address) {
				return nil, errors.New("overlapping bind specifications")
			}
		}
		result = append(result, candidate)
	}
	return result, nil
}

func sameBindings(current []proxy.BindEndpoint, port uint16, specs []proxy.BindSpec) bool {
	if port == 0 || len(current) != len(specs) {
		return false
	}
	want := make(map[proxy.BindEndpoint]int, len(specs))
	for _, spec := range specs {
		address := spec.Address
		if address.IsValid() {
			address = address.Unmap()
		}
		want[proxy.BindEndpoint{
			Protocol: spec.Protocol, Family: spec.Family, Address: address, Port: port,
			Interface: spec.Interface, Freebind: spec.Freebind,
		}]++
	}
	for _, endpoint := range current {
		if want[endpoint] == 0 {
			return false
		}
		want[endpoint]--
	}
	return true
}

func (r *listenerRuntime) emit(event TrafficEvent) {
	if r.observe != nil {
		r.observe(event)
	}
}

func remoteIP(address net.Addr) netip.Addr {
	switch value := address.(type) {
	case *net.TCPAddr:
		if addr, ok := netip.AddrFromSlice(value.IP); ok {
			return addr.Unmap()
		}
	case *net.UDPAddr:
		if addr, ok := netip.AddrFromSlice(value.IP); ok {
			return addr.Unmap()
		}
	}
	return netip.Addr{}
}
