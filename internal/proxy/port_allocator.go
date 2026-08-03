package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"slices"
	"sync"
)

var (
	ErrNoAvailablePort = errors.New("no available port in automatic range")
	ErrPortConflict    = errors.New("proxy bind endpoint conflicts with an active endpoint")
)

type BindProtocol string

const (
	BindTCP BindProtocol = "tcp"
	BindUDP BindProtocol = "udp"
)

type BindFamily string

const (
	BindIPv4 BindFamily = "ipv4"
	BindIPv6 BindFamily = "ipv6"
)

type BindSpec struct {
	Protocol  BindProtocol
	Family    BindFamily
	Address   netip.Addr
	Interface string
	Freebind  bool
}

type BindEndpoint struct {
	Protocol  BindProtocol
	Family    BindFamily
	Address   netip.Addr
	Port      uint16
	Interface string
	Freebind  bool
}

type SocketBinder interface {
	Bind(context.Context, BindEndpoint) (io.Closer, error)
}

type Binding struct {
	Endpoint BindEndpoint
	Socket   io.Closer
}

type PortAllocator struct {
	mu       sync.Mutex
	start    uint16
	end      uint16
	next     uint16
	binder   SocketBinder
	reserved map[BindEndpoint]struct{}
}

type PortReservation struct {
	allocator *PortAllocator
	port      uint16
	mu        sync.Mutex
	bindings  []Binding
	closed    bool
	closeErr  error
}

func NewPortAllocator(start, end uint16, binder SocketBinder) (*PortAllocator, error) {
	if start == 0 || end == 0 {
		return nil, errors.New("automatic port range must be non-zero")
	}
	if start > end {
		return nil, errors.New("automatic port range start exceeds end")
	}
	if binder == nil {
		return nil, errors.New("socket binder is required")
	}
	return &PortAllocator{
		start: start, end: end, next: start, binder: binder,
		reserved: make(map[BindEndpoint]struct{}),
	}, nil
}

func (a *PortAllocator) Reserve(ctx context.Context, requested uint16, specs []BindSpec) (*PortReservation, error) {
	normalized, err := normalizeBindSpecs(specs)
	if err != nil {
		return nil, err
	}
	if requested != 0 {
		a.mu.Lock()
		defer a.mu.Unlock()
		reservation, err := a.reserveCandidate(ctx, requested, normalized)
		if err != nil {
			return nil, fmt.Errorf("reserve manual port %d: %w", requested, err)
		}
		return reservation, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	candidate := a.next
	count := int(a.end-a.start) + 1
	var failures []error
	for range count {
		reservation, reserveErr := a.reserveCandidate(ctx, candidate, normalized)
		if reserveErr == nil {
			a.next = a.after(candidate)
			return reservation, nil
		}
		failures = append(failures, fmt.Errorf("port %d: %w", candidate, reserveErr))
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		candidate = a.after(candidate)
	}
	return nil, fmt.Errorf("%w: %w", ErrNoAvailablePort, errors.Join(failures...))
}

func (a *PortAllocator) reserveCandidate(ctx context.Context, port uint16, specs []BindSpec) (*PortReservation, error) {
	endpoints := make([]BindEndpoint, 0, len(specs))
	for _, spec := range specs {
		endpoint := BindEndpoint{
			Protocol: spec.Protocol, Family: spec.Family, Address: spec.Address, Port: port,
			Interface: spec.Interface, Freebind: spec.Freebind,
		}
		for active := range a.reserved {
			if endpointsConflict(endpoint, active) {
				return nil, fmt.Errorf("%w: %v conflicts with %v", ErrPortConflict, endpoint, active)
			}
		}
		endpoints = append(endpoints, endpoint)
	}

	bindings := make([]Binding, 0, len(endpoints))
	for _, endpoint := range endpoints {
		socket, err := a.binder.Bind(ctx, endpoint)
		if err != nil {
			var closeErrors []error
			for i := len(bindings) - 1; i >= 0; i-- {
				if closeErr := bindings[i].Socket.Close(); closeErr != nil {
					closeErrors = append(closeErrors, closeErr)
				}
			}
			return nil, errors.Join(fmt.Errorf("bind %v: %w", endpoint, err), errors.Join(closeErrors...))
		}
		if socket == nil {
			for i := len(bindings) - 1; i >= 0; i-- {
				_ = bindings[i].Socket.Close()
			}
			return nil, fmt.Errorf("bind %v: binder returned a nil socket", endpoint)
		}
		bindings = append(bindings, Binding{Endpoint: endpoint, Socket: socket})
	}
	for _, endpoint := range endpoints {
		a.reserved[endpoint] = struct{}{}
	}
	return &PortReservation{allocator: a, port: port, bindings: bindings}, nil
}

func (a *PortAllocator) after(port uint16) uint16 {
	if port >= a.end || port < a.start {
		return a.start
	}
	return port + 1
}

func (r *PortReservation) Port() uint16 {
	if r == nil {
		return 0
	}
	return r.port
}

func (r *PortReservation) Bindings() []Binding {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.bindings)
}

func (r *PortReservation) ReleaseEndpoints(endpoints []BindEndpoint) error {
	if r == nil || r.allocator == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || len(endpoints) == 0 {
		return r.closeErr
	}
	requested := make(map[BindEndpoint]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		requested[endpoint] = struct{}{}
	}
	remaining := make([]Binding, 0, len(r.bindings))
	var released []BindEndpoint
	var failures []error
	for index := len(r.bindings) - 1; index >= 0; index-- {
		binding := r.bindings[index]
		if _, selected := requested[binding.Endpoint]; !selected {
			continue
		}
		if err := binding.Socket.Close(); err != nil {
			failures = append(failures, fmt.Errorf("close %v: %w", binding.Endpoint, err))
		}
		released = append(released, binding.Endpoint)
	}
	for _, binding := range r.bindings {
		if _, selected := requested[binding.Endpoint]; !selected {
			remaining = append(remaining, binding)
		}
	}
	r.bindings = remaining
	r.allocator.mu.Lock()
	for _, endpoint := range released {
		delete(r.allocator.reserved, endpoint)
	}
	r.allocator.mu.Unlock()
	r.closeErr = errors.Join(r.closeErr, errors.Join(failures...))
	return errors.Join(failures...)
}

func (r *PortReservation) Close() error {
	if r == nil || r.allocator == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.closeErr
	}
	r.closed = true
	var failures []error
	for i := len(r.bindings) - 1; i >= 0; i-- {
		if err := r.bindings[i].Socket.Close(); err != nil {
			failures = append(failures, fmt.Errorf("close %v: %w", r.bindings[i].Endpoint, err))
		}
	}
	r.allocator.mu.Lock()
	for _, binding := range r.bindings {
		delete(r.allocator.reserved, binding.Endpoint)
	}
	r.allocator.mu.Unlock()
	r.bindings = nil
	r.closeErr = errors.Join(r.closeErr, errors.Join(failures...))
	return r.closeErr
}

func normalizeBindSpecs(specs []BindSpec) ([]BindSpec, error) {
	if len(specs) == 0 {
		return nil, errors.New("at least one bind specification is required")
	}
	normalized := make([]BindSpec, 0, len(specs))
	seen := make(map[BindSpec]struct{}, len(specs))
	for _, spec := range specs {
		if spec.Protocol != BindTCP && spec.Protocol != BindUDP {
			return nil, fmt.Errorf("unsupported bind protocol %q", spec.Protocol)
		}
		if spec.Family != BindIPv4 && spec.Family != BindIPv6 {
			return nil, fmt.Errorf("unsupported bind family %q", spec.Family)
		}
		if spec.Address.IsValid() {
			spec.Address = spec.Address.Unmap()
			if spec.Family == BindIPv4 && !spec.Address.Is4() {
				return nil, fmt.Errorf("address %s does not match IPv4 bind", spec.Address)
			}
			if spec.Family == BindIPv6 && (!spec.Address.Is6() || spec.Address.Is4In6()) {
				return nil, fmt.Errorf("address %s does not match IPv6 bind", spec.Address)
			}
		}
		if _, exists := seen[spec]; exists {
			return nil, fmt.Errorf("duplicate bind specification %v", spec)
		}
		candidate := BindEndpoint{Protocol: spec.Protocol, Family: spec.Family, Address: spec.Address, Port: 1}
		for _, existing := range normalized {
			active := BindEndpoint{Protocol: existing.Protocol, Family: existing.Family, Address: existing.Address, Port: 1}
			if endpointsConflict(candidate, active) {
				return nil, fmt.Errorf("%w: overlapping bind specifications %v and %v", ErrPortConflict, existing, spec)
			}
		}
		seen[spec] = struct{}{}
		normalized = append(normalized, spec)
	}
	return normalized, nil
}

func endpointsConflict(left, right BindEndpoint) bool {
	if left.Protocol != right.Protocol || left.Family != right.Family || left.Port != right.Port {
		return false
	}
	return !left.Address.IsValid() || !right.Address.IsValid() || left.Address == right.Address
}
