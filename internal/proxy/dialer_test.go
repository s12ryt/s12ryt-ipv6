package proxy

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/dns64"
	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
)

type fakeDestinationResolver struct {
	resolution dns64.Resolution
	err        error
	host       string
	policy     policy.DestinationPolicy
	override   policy.ULAOverride
	prefix     netip.Prefix
}

func (f *fakeDestinationResolver) Resolve(_ context.Context, host string, destinationPolicy policy.DestinationPolicy, override policy.ULAOverride, prefix netip.Prefix) (dns64.Resolution, error) {
	f.host = host
	f.policy = destinationPolicy
	f.override = override
	f.prefix = prefix
	return f.resolution, f.err
}

type connectorCall struct {
	network     string
	destination netip.AddrPort
	source      netip.Addr
	timeout     time.Duration
}

type fakeConnector struct {
	calls   []connectorCall
	failFor map[netip.Addr]error
	conn    net.Conn
}

type blockingConnector struct {
	called  chan struct{}
	release chan struct{}
	conn    net.Conn
}

func (c *blockingConnector) DialContext(context.Context, string, netip.AddrPort, netip.Addr, time.Duration) (net.Conn, error) {
	close(c.called)
	<-c.release
	return c.conn, nil
}

func (f *fakeConnector) DialContext(_ context.Context, network string, destination netip.AddrPort, source netip.Addr, timeout time.Duration) (net.Conn, error) {
	f.calls = append(f.calls, connectorCall{network: network, destination: destination, source: source, timeout: timeout})
	if err := f.failFor[destination.Addr()]; err != nil {
		return nil, err
	}
	return f.conn, nil
}

type stubConn struct {
	closed int
}

func (*stubConn) Read([]byte) (int, error)         { return 0, errors.New("not implemented") }
func (*stubConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *stubConn) Close() error                   { c.closed++; return nil }
func (*stubConn) LocalAddr() net.Addr              { return stubAddr("local") }
func (*stubConn) RemoteAddr() net.Addr             { return stubAddr("remote") }
func (*stubConn) SetDeadline(time.Time) error      { return nil }
func (*stubConn) SetReadDeadline(time.Time) error  { return nil }
func (*stubConn) SetWriteDeadline(time.Time) error { return nil }

type stubAddr string

func (a stubAddr) Network() string { return "stub" }
func (a stubAddr) String() string  { return string(a) }

func TestDialerRetriesDestinationsWithOneSourceLeaseAndReleasesOnClose(t *testing.T) {
	sourceA := netip.MustParseAddr("2001:4860:1::1")
	sourceB := netip.MustParseAddr("2001:4860:1::2")
	destinationA := netip.MustParseAddr("2606:4700:4700::1111")
	destinationB := netip.MustParseAddr("2001:4860:4860::8888")
	drained := make([]netip.Addr, 0, 1)
	sources, err := NewSourcePool([]netip.Addr{sourceA, sourceB}, func(address netip.Addr) {
		drained = append(drained, address)
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver := &fakeDestinationResolver{resolution: dns64.Resolution{
		Addresses: []netip.Addr{destinationA, destinationB}, Source: "dot-primary",
	}}
	underlying := &stubConn{}
	connector := &fakeConnector{failFor: map[netip.Addr]error{destinationA: errors.New("unreachable")}, conn: underlying}
	nat64Prefix := netip.MustParsePrefix("64:ff9b::/96")
	destinationPolicy := policy.DestinationPolicy{AllowULA: true}
	dialer, err := NewDialer(DialerOptions{
		Resolver:    resolver,
		Sources:     sources,
		Connector:   connector,
		Policy:      func() policy.DestinationPolicy { return destinationPolicy },
		NAT64Prefix: func() netip.Prefix { return nat64Prefix },
		ULAOverride: policy.ULADeny,
		Timeout:     7 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	conn, metadata, err := dialer.Dial(context.Background(), "tcp", "example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.host != "example.com" || resolver.policy.AllowULA != destinationPolicy.AllowULA || resolver.override != policy.ULADeny || resolver.prefix != nat64Prefix {
		t.Fatalf("resolver arguments = host %q policy %#v override %q prefix %s", resolver.host, resolver.policy, resolver.override, resolver.prefix)
	}
	if len(connector.calls) != 2 {
		t.Fatalf("connector calls = %d, want 2", len(connector.calls))
	}
	for _, call := range connector.calls {
		if call.network != "tcp6" || call.source != sourceA || call.timeout != 7*time.Second || call.destination.Port() != 443 {
			t.Fatalf("connector call = %#v", call)
		}
	}
	if connector.calls[0].destination.Addr() != destinationA || connector.calls[1].destination.Addr() != destinationB {
		t.Fatalf("destination retry order = %#v", connector.calls)
	}
	if metadata.Source != sourceA || metadata.Destination != netip.AddrPortFrom(destinationB, 443) || metadata.Resolver != "dot-primary" {
		t.Fatalf("metadata = %#v", metadata)
	}

	if err := sources.Replace([]netip.Addr{sourceB}); err != nil {
		t.Fatal(err)
	}
	if len(drained) != 0 {
		t.Fatalf("source drained before connection close: %v", drained)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if underlying.closed != 1 {
		t.Fatalf("underlying Close calls = %d, want 1", underlying.closed)
	}
	if len(drained) != 1 || drained[0] != sourceA {
		t.Fatalf("drained after close = %v, want [%s]", drained, sourceA)
	}
}

func TestDialerUsesUDP6AndReleasesLeaseAfterAllDestinationsFail(t *testing.T) {
	source := netip.MustParseAddr("2001:4860:1::1")
	destination := netip.MustParseAddr("2606:4700:4700::1111")
	drained := make([]netip.Addr, 0, 1)
	sources, err := NewSourcePool([]netip.Addr{source}, func(address netip.Addr) { drained = append(drained, address) })
	if err != nil {
		t.Fatal(err)
	}
	resolver := &fakeDestinationResolver{resolution: dns64.Resolution{Addresses: []netip.Addr{destination}}}
	connector := &fakeConnector{failFor: map[netip.Addr]error{destination: errors.New("unreachable")}}
	dialer, err := NewDialer(DialerOptions{
		Resolver: resolver, Sources: sources, Connector: connector,
		Policy:      func() policy.DestinationPolicy { return policy.DestinationPolicy{} },
		NAT64Prefix: func() netip.Prefix { return netip.Prefix{} },
		ULAOverride: policy.ULAInherit, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := dialer.Dial(context.Background(), "udp", "example.com", 53); err == nil {
		t.Fatal("Dial() error = nil, want connector failure")
	}
	if len(connector.calls) != 1 || connector.calls[0].network != "udp6" || connector.calls[0].source != source {
		t.Fatalf("connector calls = %#v", connector.calls)
	}
	if err := sources.Replace([]netip.Addr{netip.MustParseAddr("2001:4860:1::2")}); err != nil {
		t.Fatal(err)
	}
	if len(drained) != 1 || drained[0] != source {
		t.Fatalf("failed dial leaked source lease: drained=%v", drained)
	}
}

func TestDialerRejectsInvalidConstructionAndRequest(t *testing.T) {
	source := netip.MustParseAddr("2001:4860:1::1")
	sources, err := NewSourcePool([]netip.Addr{source}, nil)
	if err != nil {
		t.Fatal(err)
	}
	valid := DialerOptions{
		Resolver: &fakeDestinationResolver{}, Sources: sources, Connector: &fakeConnector{},
		Policy:      func() policy.DestinationPolicy { return policy.DestinationPolicy{} },
		NAT64Prefix: func() netip.Prefix { return netip.Prefix{} }, Timeout: time.Second,
	}
	invalid := []DialerOptions{
		{},
		func() DialerOptions { candidate := valid; candidate.Resolver = nil; return candidate }(),
		func() DialerOptions { candidate := valid; candidate.Sources = nil; return candidate }(),
		func() DialerOptions { candidate := valid; candidate.Connector = nil; return candidate }(),
		func() DialerOptions { candidate := valid; candidate.Policy = nil; return candidate }(),
		func() DialerOptions { candidate := valid; candidate.NAT64Prefix = nil; return candidate }(),
		func() DialerOptions { candidate := valid; candidate.Timeout = 0; return candidate }(),
	}
	for _, candidate := range invalid {
		if _, err := NewDialer(candidate); err == nil {
			t.Fatalf("NewDialer(%#v) error = nil", candidate)
		}
	}
	dialer, err := NewDialer(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := dialer.Dial(context.Background(), "icmp", "example.com", 1); err == nil {
		t.Fatal("Dial() accepted unsupported network")
	}
	if _, _, err := dialer.Dial(context.Background(), "tcp", "", 1); err == nil {
		t.Fatal("Dial() accepted empty host")
	}
	if _, _, err := dialer.Dial(context.Background(), "tcp", "example.com", 0); err == nil {
		t.Fatal("Dial() accepted port zero")
	}
}

func TestDialerClosesConnectionForcedWhileDialWasInFlight(t *testing.T) {
	sourceA := netip.MustParseAddr("2001:4860:1::1")
	sourceB := netip.MustParseAddr("2001:4860:1::2")
	destination := netip.MustParseAddr("2606:4700:4700::1111")
	drained := make(chan netip.Addr, 1)
	sources, err := NewSourcePool([]netip.Addr{sourceA}, func(address netip.Addr) { drained <- address })
	if err != nil {
		t.Fatal(err)
	}
	underlying := &stubConn{}
	connector := &blockingConnector{called: make(chan struct{}), release: make(chan struct{}), conn: underlying}
	dialer, err := NewDialer(DialerOptions{
		Resolver: &fakeDestinationResolver{resolution: dns64.Resolution{Addresses: []netip.Addr{destination}}},
		Sources:  sources, Connector: connector,
		Policy:      func() policy.DestinationPolicy { return policy.DestinationPolicy{} },
		NAT64Prefix: func() netip.Prefix { return netip.Prefix{} },
		Timeout:     time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		_, _, dialErr := dialer.Dial(context.Background(), "tcp", "example.com", 443)
		result <- dialErr
	}()
	<-connector.called
	if err := sources.Replace([]netip.Addr{sourceB}); err != nil {
		t.Fatal(err)
	}
	if err := sources.ForceDrain(sourceA); err != nil {
		t.Fatal(err)
	}
	close(connector.release)

	if err := <-result; !errors.Is(err, ErrSourceDrainForced) {
		t.Fatalf("Dial() error = %v, want ErrSourceDrainForced", err)
	}
	if underlying.closed != 1 {
		t.Fatalf("underlying Close calls = %d, want 1", underlying.closed)
	}
	select {
	case address := <-drained:
		if address != sourceA {
			t.Fatalf("drained address = %s, want %s", address, sourceA)
		}
	default:
		t.Fatal("forced in-flight lease did not complete draining")
	}
}
