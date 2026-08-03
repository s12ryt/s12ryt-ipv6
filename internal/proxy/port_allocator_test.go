package proxy

import (
	"context"
	"errors"
	"io"
	"net/netip"
	"testing"
)

type fakeBoundSocket struct {
	closed int
	err    error
}

func (s *fakeBoundSocket) Close() error {
	s.closed++
	return s.err
}

type fakeSocketBinder struct {
	calls   []BindEndpoint
	fail    map[BindEndpoint]error
	sockets map[BindEndpoint]*fakeBoundSocket
}

func (b *fakeSocketBinder) Bind(_ context.Context, endpoint BindEndpoint) (io.Closer, error) {
	b.calls = append(b.calls, endpoint)
	if err := b.fail[endpoint]; err != nil {
		return nil, err
	}
	socket := &fakeBoundSocket{}
	if b.sockets == nil {
		b.sockets = make(map[BindEndpoint]*fakeBoundSocket)
	}
	b.sockets[endpoint] = socket
	return socket, nil
}

func TestPortAllocatorAutoBindsAllTransportsAndSkipsFailedCandidate(t *testing.T) {
	address := netip.MustParseAddr("2001:4860:1::1")
	failedTCP := BindEndpoint{Protocol: BindTCP, Family: BindIPv6, Address: address, Port: 50000}
	binder := &fakeSocketBinder{fail: map[BindEndpoint]error{failedTCP: errors.New("in use")}}
	allocator, err := NewPortAllocator(50000, 50002, binder)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := allocator.Reserve(context.Background(), 0, []BindSpec{
		{Protocol: BindTCP, Family: BindIPv6, Address: address},
		{Protocol: BindUDP, Family: BindIPv6, Address: address},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Port() != 50001 {
		t.Fatalf("allocated port = %d, want 50001", reservation.Port())
	}
	bindings := reservation.Bindings()
	if len(bindings) != 2 || bindings[0].Endpoint.Port != 50001 || bindings[1].Endpoint.Port != 50001 {
		t.Fatalf("bindings = %#v", bindings)
	}
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	for _, binding := range bindings {
		if binder.sockets[binding.Endpoint].closed != 1 {
			t.Fatalf("socket %v close count = %d", binding.Endpoint, binder.sockets[binding.Endpoint].closed)
		}
	}
}

func TestPortAllocatorRollsBackPartiallyBoundCandidate(t *testing.T) {
	address := netip.MustParseAddr("2001:4860:1::1")
	tcp := BindEndpoint{Protocol: BindTCP, Family: BindIPv6, Address: address, Port: 50000}
	udp := BindEndpoint{Protocol: BindUDP, Family: BindIPv6, Address: address, Port: 50000}
	binder := &fakeSocketBinder{fail: map[BindEndpoint]error{udp: errors.New("UDP unavailable")}}
	allocator, err := NewPortAllocator(50000, 50000, binder)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := allocator.Reserve(context.Background(), 0, []BindSpec{
		{Protocol: BindTCP, Family: BindIPv6, Address: address},
		{Protocol: BindUDP, Family: BindIPv6, Address: address},
	}); !errors.Is(err, ErrNoAvailablePort) {
		t.Fatalf("Reserve() error = %v, want ErrNoAvailablePort", err)
	}
	if binder.sockets[tcp] == nil || binder.sockets[tcp].closed != 1 {
		t.Fatalf("partially bound TCP socket was not rolled back: %#v", binder.sockets[tcp])
	}
}

func TestPortAllocatorWildcardConflictsButDifferentSpecificAddressesCanReusePort(t *testing.T) {
	a := netip.MustParseAddr("2001:4860:1::1")
	b := netip.MustParseAddr("2001:4860:1::2")
	binder := &fakeSocketBinder{}
	allocator, err := NewPortAllocator(50000, 50002, binder)
	if err != nil {
		t.Fatal(err)
	}
	first, err := allocator.Reserve(context.Background(), 50000, []BindSpec{{Protocol: BindTCP, Family: BindIPv6, Address: a}})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := allocator.Reserve(context.Background(), 50000, []BindSpec{{Protocol: BindTCP, Family: BindIPv6, Address: b}})
	if err != nil {
		t.Fatalf("different specific address cannot reuse port: %v", err)
	}
	defer second.Close()
	callCount := len(binder.calls)
	if _, err := allocator.Reserve(context.Background(), 50000, []BindSpec{{Protocol: BindTCP, Family: BindIPv6}}); !errors.Is(err, ErrPortConflict) {
		t.Fatalf("wildcard Reserve() error = %v, want ErrPortConflict", err)
	}
	if len(binder.calls) != callCount {
		t.Fatal("known wildcard conflict reached socket binder")
	}

	v4Wildcard, err := allocator.Reserve(context.Background(), 50000, []BindSpec{{Protocol: BindTCP, Family: BindIPv4}})
	if err != nil {
		t.Fatalf("IPv4 wildcard incorrectly conflicts with IPv6: %v", err)
	}
	defer v4Wildcard.Close()
}

func TestPortAllocatorReleaseMakesEndpointReusable(t *testing.T) {
	address := netip.MustParseAddr("2001:4860:1::1")
	binder := &fakeSocketBinder{}
	allocator, err := NewPortAllocator(50000, 50000, binder)
	if err != nil {
		t.Fatal(err)
	}
	specs := []BindSpec{{Protocol: BindTCP, Family: BindIPv6, Address: address}}
	first, err := allocator.Reserve(context.Background(), 50000, specs)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := allocator.Reserve(context.Background(), 50000, specs)
	if err != nil {
		t.Fatalf("released endpoint was not reusable: %v", err)
	}
	defer second.Close()
}

func TestPortAllocatorValidatesConfigurationAndSpecs(t *testing.T) {
	binder := &fakeSocketBinder{}
	if _, err := NewPortAllocator(0, 10, binder); err == nil {
		t.Fatal("NewPortAllocator accepted zero start")
	}
	if _, err := NewPortAllocator(20, 10, binder); err == nil {
		t.Fatal("NewPortAllocator accepted reversed range")
	}
	if _, err := NewPortAllocator(10, 20, nil); err == nil {
		t.Fatal("NewPortAllocator accepted nil binder")
	}
	allocator, err := NewPortAllocator(50000, 50001, binder)
	if err != nil {
		t.Fatal(err)
	}
	invalid := [][]BindSpec{
		nil,
		{{Protocol: "icmp", Family: BindIPv6}},
		{{Protocol: BindTCP, Family: "inet"}},
		{{Protocol: BindTCP, Family: BindIPv4, Address: netip.MustParseAddr("2001:4860::1")}},
		{{Protocol: BindTCP, Family: BindIPv6, Address: netip.MustParseAddr("192.0.2.1")}},
		{{Protocol: BindTCP, Family: BindIPv6}, {Protocol: BindTCP, Family: BindIPv6}},
	}
	for _, specs := range invalid {
		if _, err := allocator.Reserve(context.Background(), 0, specs); err == nil {
			t.Fatalf("Reserve(%#v) error = nil", specs)
		}
	}
	manual, err := allocator.Reserve(context.Background(), 49999, []BindSpec{{Protocol: BindTCP, Family: BindIPv6}})
	if err != nil {
		t.Fatalf("manual port outside automatic range was rejected: %v", err)
	}
	defer manual.Close()
}

func TestPortAllocatorRejectsOverlappingSpecsBeforeBinding(t *testing.T) {
	binder := &fakeSocketBinder{}
	allocator, err := NewPortAllocator(50000, 50000, binder)
	if err != nil {
		t.Fatal(err)
	}
	specific := netip.MustParseAddr("2001:4860:1::1")
	_, err = allocator.Reserve(context.Background(), 50000, []BindSpec{
		{Protocol: BindTCP, Family: BindIPv6},
		{Protocol: BindTCP, Family: BindIPv6, Address: specific},
	})
	if !errors.Is(err, ErrPortConflict) {
		t.Fatalf("Reserve() error = %v, want ErrPortConflict", err)
	}
	if len(binder.calls) != 0 {
		t.Fatalf("overlapping specifications reached binder: %#v", binder.calls)
	}
}

func TestPortReservationReleasesSelectedEndpointsWithoutClosingOthers(t *testing.T) {
	binder := &fakeSocketBinder{}
	allocator, err := NewPortAllocator(50000, 50000, binder)
	if err != nil {
		t.Fatal(err)
	}
	first := netip.MustParseAddr("2001:4860:1::10")
	second := netip.MustParseAddr("2001:4860:1::11")
	reservation, err := allocator.Reserve(context.Background(), 50000, []BindSpec{
		{Protocol: BindTCP, Family: BindIPv6, Address: first},
		{Protocol: BindTCP, Family: BindIPv6, Address: second},
	})
	if err != nil {
		t.Fatal(err)
	}
	bindings := reservation.Bindings()
	if err := reservation.ReleaseEndpoints([]BindEndpoint{bindings[0].Endpoint}); err != nil {
		t.Fatal(err)
	}
	if binder.sockets[bindings[0].Endpoint].closed != 1 || binder.sockets[bindings[1].Endpoint].closed != 0 {
		t.Fatalf("socket close counts = %d/%d", binder.sockets[bindings[0].Endpoint].closed, binder.sockets[bindings[1].Endpoint].closed)
	}
	if got := reservation.Bindings(); len(got) != 1 || got[0].Endpoint != bindings[1].Endpoint {
		t.Fatalf("remaining bindings = %#v", got)
	}
	reused, err := allocator.Reserve(context.Background(), 50000, []BindSpec{{
		Protocol: BindTCP, Family: BindIPv6, Address: first,
	}})
	if err != nil {
		t.Fatalf("released endpoint could not be reused: %v", err)
	}
	if err := reservation.ReleaseEndpoints([]BindEndpoint{bindings[0].Endpoint}); err != nil {
		t.Fatalf("second release should be idempotent: %v", err)
	}
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	if binder.sockets[bindings[1].Endpoint].closed != 1 {
		t.Fatal("Close did not release remaining endpoint")
	}
	_ = reused.Close()
}
