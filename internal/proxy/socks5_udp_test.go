package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/things-go/go-socks5/statute"
)

type packetFrame struct {
	payload []byte
	addr    net.Addr
}

type fakePacketConn struct {
	local    net.Addr
	incoming chan packetFrame
	outgoing chan packetFrame
	closed   chan struct{}
	once     sync.Once
}

func newFakePacketConn(local net.Addr) *fakePacketConn {
	return &fakePacketConn{
		local: local, incoming: make(chan packetFrame, 4), outgoing: make(chan packetFrame, 4), closed: make(chan struct{}),
	}
}

func (c *fakePacketConn) ReadFrom(payload []byte) (int, net.Addr, error) {
	select {
	case frame := <-c.incoming:
		return copy(payload, frame.payload), frame.addr, nil
	case <-c.closed:
		return 0, nil, net.ErrClosed
	}
}

func (c *fakePacketConn) WriteTo(payload []byte, addr net.Addr) (int, error) {
	copyPayload := append([]byte(nil), payload...)
	select {
	case c.outgoing <- packetFrame{payload: copyPayload, addr: addr}:
		return len(payload), nil
	case <-c.closed:
		return 0, net.ErrClosed
	}
}

func (c *fakePacketConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *fakePacketConn) LocalAddr() net.Addr              { return c.local }
func (c *fakePacketConn) SetDeadline(time.Time) error      { return nil }
func (c *fakePacketConn) SetReadDeadline(time.Time) error  { return nil }
func (c *fakePacketConn) SetWriteDeadline(time.Time) error { return nil }

type relaySocketBinder struct {
	packet   *fakePacketConn
	endpoint BindEndpoint
}

func (b *relaySocketBinder) Bind(_ context.Context, endpoint BindEndpoint) (io.Closer, error) {
	b.endpoint = endpoint
	b.packet = newFakePacketConn(&net.UDPAddr{IP: net.IP(endpoint.Address.AsSlice()), Port: int(endpoint.Port)})
	return b.packet, nil
}

type fakeRelayFirewall struct {
	opened    []BindEndpoint
	closed    []BindEndpoint
	openError error
}

func (f *fakeRelayFirewall) Open(_ context.Context, endpoint BindEndpoint) error {
	f.opened = append(f.opened, endpoint)
	return f.openError
}

func (f *fakeRelayFirewall) Close(_ context.Context, endpoint BindEndpoint) error {
	f.closed = append(f.closed, endpoint)
	return nil
}

type addressedConn struct {
	net.Conn
	local  net.Addr
	remote net.Addr
}

func (c *addressedConn) LocalAddr() net.Addr  { return c.local }
func (c *addressedConn) RemoteAddr() net.Addr { return c.remote }

func TestSOCKS5UDPAssociateRelaysPerDestinationAndCleansUp(t *testing.T) {
	binder := &relaySocketBinder{}
	allocator, err := NewPortAllocator(51000, 51002, binder)
	if err != nil {
		t.Fatal(err)
	}
	upstream, origin := net.Pipe()
	dialer := &fakeProxyDialer{
		conn: upstream,
		metadata: DialMetadata{
			Source:      netip.MustParseAddr("2001:4860:1::10"),
			Destination: netip.MustParseAddrPort("[2001:4860:4860::8888]:53"),
		},
	}
	firewall := &fakeRelayFirewall{}
	relays, err := NewUDPRelayManager(UDPRelayOptions{
		Allocator: allocator, Dialer: dialer, Firewall: firewall,
		IdleTimeout: time.Minute, MaxAssociations: 1, Interface: "eth0",
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NewSOCKS5Proxy(SOCKS5Options{
		Dialer: dialer, UDPRelays: relays, HandshakeTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	clientPipe, serverPipe := net.Pipe()
	client := &addressedConn{Conn: clientPipe, local: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 53000}, remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1080}}
	server := &addressedConn{Conn: serverPipe, local: &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1080}, remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 53000}}
	done := make(chan struct {
		traffic ProxyTraffic
		err     error
	}, 1)
	go func() {
		traffic, serveErr := proxy.ServeConn(context.Background(), server)
		done <- struct {
			traffic ProxyTraffic
			err     error
		}{traffic, serveErr}
	}()

	_, _ = client.Write(statute.NewMethodRequest(statute.VersionSocks5, []byte{statute.MethodNoAuth}).Bytes())
	if _, err := statute.ParseMethodReply(client); err != nil {
		t.Fatal(err)
	}
	request := statute.Request{
		Version: statute.VersionSocks5, Command: statute.CommandAssociate,
		DstAddr: statute.AddrSpec{AddrType: statute.ATYPIPv4, IP: net.IPv4zero, Port: 0},
	}
	_, _ = client.Write(request.Bytes())
	reply, err := statute.ParseReply(client)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Response != statute.RepSuccess || reply.BndAddr.Port != 51000 {
		t.Fatalf("associate reply = %#v", reply)
	}
	if len(firewall.opened) != 1 || firewall.opened[0] != binder.endpoint {
		t.Fatalf("firewall opened = %#v, endpoint = %#v", firewall.opened, binder.endpoint)
	}

	datagram, err := statute.NewDatagram("example.com:53", []byte("query"))
	if err != nil {
		t.Fatal(err)
	}
	udpClient := &net.UDPAddr{IP: net.ParseIP("192.0.2.10"), Port: 54000}
	spoofed, err := statute.NewDatagram("example.com:53", []byte("spoof"))
	if err != nil {
		t.Fatal(err)
	}
	binder.packet.incoming <- packetFrame{
		payload: spoofed.Bytes(),
		addr:    &net.UDPAddr{IP: net.ParseIP("192.0.2.11"), Port: 54000},
	}
	fragmented := datagram
	fragmented.Frag = 1
	binder.packet.incoming <- packetFrame{payload: fragmented.Bytes(), addr: udpClient}
	binder.packet.incoming <- packetFrame{payload: datagram.Bytes(), addr: udpClient}
	originPayload := make([]byte, len("query"))
	if _, err := io.ReadFull(origin, originPayload); err != nil {
		t.Fatal(err)
	}
	if string(originPayload) != "query" {
		t.Fatalf("origin payload = %q", originPayload)
	}
	go func() { _, _ = origin.Write([]byte("answer")) }()
	select {
	case frame := <-binder.packet.outgoing:
		if frame.addr.String() != udpClient.String() {
			t.Fatalf("UDP response destination = %s", frame.addr)
		}
		response, err := statute.ParseDatagram(frame.payload)
		if err != nil {
			t.Fatal(err)
		}
		if response.DstAddr.FQDN != "example.com" || response.DstAddr.Port != 53 || string(response.Data) != "answer" {
			t.Fatalf("UDP response = %#v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for UDP response")
	}

	_ = client.Close()
	_ = origin.Close()
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.traffic.Protocol != "socks" || result.traffic.UpBytes != int64(len("query")) || result.traffic.DownBytes != int64(len("answer")) {
		t.Fatalf("traffic = %#v", result.traffic)
	}
	if calls := dialer.Calls(); len(calls) != 1 || calls[0] != (proxyDialCall{network: "udp", host: "example.com", port: 53}) {
		t.Fatalf("dial calls = %#v", calls)
	}
	if len(firewall.closed) != 1 || firewall.closed[0] != binder.endpoint {
		t.Fatalf("firewall closed = %#v", firewall.closed)
	}
	select {
	case <-binder.packet.closed:
	default:
		t.Fatal("UDP relay socket was not closed")
	}
}

func TestSOCKS5UDPAssociateSelectsBindPropertiesFromControlAddress(t *testing.T) {
	binder := &relaySocketBinder{}
	allocator, err := NewPortAllocator(51000, 51000, binder)
	if err != nil {
		t.Fatal(err)
	}
	selectedAddress := netip.Addr{}
	selectedFamily := BindFamily("")
	relays, err := NewUDPRelayManager(UDPRelayOptions{
		Allocator: allocator, Dialer: &fakeProxyDialer{}, Firewall: &fakeRelayFirewall{},
		IdleTimeout: time.Minute, MaxAssociations: 1,
		BindSelector: func(address netip.Addr, family BindFamily) (string, bool, error) {
			selectedAddress, selectedFamily = address, family
			return "eth9", true, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	socksProxy, err := NewSOCKS5Proxy(SOCKS5Options{
		Dialer: &fakeProxyDialer{}, UDPRelays: relays, HandshakeTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	clientPipe, serverPipe := net.Pipe()
	localIP := net.ParseIP("2001:4860:1::10")
	clientIP := net.ParseIP("2001:4860:1::20")
	client := &addressedConn{
		Conn:  clientPipe,
		local: &net.TCPAddr{IP: clientIP, Port: 53000}, remote: &net.TCPAddr{IP: localIP, Port: 1080},
	}
	server := &addressedConn{
		Conn:  serverPipe,
		local: &net.TCPAddr{IP: localIP, Port: 1080}, remote: &net.TCPAddr{IP: clientIP, Port: 53000},
	}
	done := make(chan error, 1)
	go func() {
		_, serveErr := socksProxy.ServeConn(context.Background(), server)
		done <- serveErr
	}()
	_, _ = client.Write(statute.NewMethodRequest(statute.VersionSocks5, []byte{statute.MethodNoAuth}).Bytes())
	if _, err := statute.ParseMethodReply(client); err != nil {
		t.Fatal(err)
	}
	request := statute.Request{
		Version: statute.VersionSocks5, Command: statute.CommandAssociate,
		DstAddr: statute.AddrSpec{AddrType: statute.ATYPIPv6, IP: net.IPv6unspecified},
	}
	_, _ = client.Write(request.Bytes())
	if reply, err := statute.ParseReply(client); err != nil || reply.Response != statute.RepSuccess {
		t.Fatalf("associate reply = %#v, %v", reply, err)
	}
	if selectedAddress != netip.MustParseAddr("2001:4860:1::10") || selectedFamily != BindIPv6 {
		t.Fatalf("selector received %s/%s", selectedAddress, selectedFamily)
	}
	if binder.endpoint.Interface != "eth9" || !binder.endpoint.Freebind {
		t.Fatalf("relay endpoint = %#v", binder.endpoint)
	}
	_ = client.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSOCKS5UDPMappingRemoteTrafficRefreshesIdleTimeout(t *testing.T) {
	binder := &relaySocketBinder{}
	allocator, err := NewPortAllocator(51000, 51000, binder)
	if err != nil {
		t.Fatal(err)
	}
	upstream, origin := net.Pipe()
	dialer := &fakeProxyDialer{conn: upstream, metadata: DialMetadata{
		Source:      netip.MustParseAddr("2001:4860:1::10"),
		Destination: netip.MustParseAddrPort("[2001:4860:4860::8888]:53"),
	}}
	relays, err := NewUDPRelayManager(UDPRelayOptions{
		Allocator: allocator, Dialer: dialer, Firewall: &fakeRelayFirewall{},
		IdleTimeout: 150 * time.Millisecond, MaxAssociations: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NewSOCKS5Proxy(SOCKS5Options{Dialer: dialer, UDPRelays: relays, HandshakeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	clientPipe, serverPipe := net.Pipe()
	client := &addressedConn{
		Conn:   clientPipe,
		local:  &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 53000},
		remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1080},
	}
	server := &addressedConn{
		Conn:   serverPipe,
		local:  &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1080},
		remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 53000},
	}
	done := make(chan error, 1)
	go func() {
		_, serveErr := proxy.ServeConn(context.Background(), server)
		done <- serveErr
	}()

	_, _ = client.Write(statute.NewMethodRequest(statute.VersionSocks5, []byte{statute.MethodNoAuth}).Bytes())
	if _, err := statute.ParseMethodReply(client); err != nil {
		t.Fatal(err)
	}
	request := statute.Request{
		Version: statute.VersionSocks5, Command: statute.CommandAssociate,
		DstAddr: statute.AddrSpec{AddrType: statute.ATYPIPv4, IP: net.IPv4zero},
	}
	_, _ = client.Write(request.Bytes())
	if reply, err := statute.ParseReply(client); err != nil || reply.Response != statute.RepSuccess {
		t.Fatalf("associate reply = %#v, %v", reply, err)
	}

	datagram, err := statute.NewDatagram("example.com:53", []byte("query"))
	if err != nil {
		t.Fatal(err)
	}
	udpClient := &net.UDPAddr{IP: net.ParseIP("192.0.2.10"), Port: 54000}
	binder.packet.incoming <- packetFrame{payload: datagram.Bytes(), addr: udpClient}
	query := make([]byte, len("query"))
	if _, err := io.ReadFull(origin, query); err != nil {
		t.Fatal(err)
	}

	for _, response := range []string{"first", "second"} {
		time.Sleep(100 * time.Millisecond)
		if _, err := origin.Write([]byte(response)); err != nil {
			t.Fatalf("remote traffic did not refresh mapping idle timeout before %q: %v", response, err)
		}
		select {
		case frame := <-binder.packet.outgoing:
			packet, err := statute.ParseDatagram(frame.payload)
			if err != nil {
				t.Fatal(err)
			}
			if string(packet.Data) != response {
				t.Fatalf("response payload = %q, want %q", packet.Data, response)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %q response", response)
		}
	}

	_ = client.Close()
	_ = origin.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSOCKS5UDPAssociateEnforcesConcurrentLimitAndReleasesSlot(t *testing.T) {
	binder := &relaySocketBinder{}
	allocator, err := NewPortAllocator(51000, 51001, binder)
	if err != nil {
		t.Fatal(err)
	}
	dialer := &fakeProxyDialer{}
	relays, err := NewUDPRelayManager(UDPRelayOptions{
		Allocator: allocator, Dialer: dialer, Firewall: &fakeRelayFirewall{},
		IdleTimeout: time.Minute, MaxAssociations: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NewSOCKS5Proxy(SOCKS5Options{Dialer: dialer, UDPRelays: relays, HandshakeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}

	firstClient, firstDone := startUDPAssociation(t, proxy, "192.0.2.10", 53000)
	secondClient, secondDone := startUDPAssociationExpecting(t, proxy, "192.0.2.11", 53001, statute.RepRuleFailure)
	_ = secondClient.Close()
	if serveErr := <-secondDone; !errors.Is(serveErr, ErrUDPAssociationLimit) {
		t.Fatalf("second association error = %v, want ErrUDPAssociationLimit", serveErr)
	}

	_ = firstClient.Close()
	if serveErr := <-firstDone; serveErr != nil {
		t.Fatal(serveErr)
	}
	thirdClient, thirdDone := startUDPAssociation(t, proxy, "192.0.2.12", 53002)
	_ = thirdClient.Close()
	if serveErr := <-thirdDone; serveErr != nil {
		t.Fatalf("association slot was not reusable: %v", serveErr)
	}
}

func TestSOCKS5UDPAssociateReportsOpenAndCloseLifecycle(t *testing.T) {
	binder := &relaySocketBinder{}
	allocator, err := NewPortAllocator(51000, 51000, binder)
	if err != nil {
		t.Fatal(err)
	}
	dialer := &fakeProxyDialer{}
	events := make(chan UDPAssociationEvent, 2)
	manager, err := NewUDPRelayManager(UDPRelayOptions{
		Allocator: allocator, Dialer: dialer, Firewall: &fakeRelayFirewall{},
		IdleTimeout: time.Minute, MaxAssociations: 1,
		Observe: func(event UDPAssociationEvent) { events <- event },
	})
	if err != nil {
		t.Fatal(err)
	}
	proxyServer, err := NewSOCKS5Proxy(SOCKS5Options{
		Dialer: dialer, UDPRelays: manager, HandshakeTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, done := startUDPAssociation(t, proxyServer, "192.0.2.10", 53000)
	select {
	case event := <-events:
		if event.Lifecycle != UDPAssociationOpened {
			t.Fatalf("open event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for UDP open event")
	}
	_ = client.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Lifecycle != UDPAssociationClosed {
			t.Fatalf("close event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for UDP close event")
	}
}

func TestSOCKS5UDPAssociateRollsBackWhenFirewallOpenFails(t *testing.T) {
	binder := &relaySocketBinder{}
	allocator, err := NewPortAllocator(51000, 51000, binder)
	if err != nil {
		t.Fatal(err)
	}
	relays, err := NewUDPRelayManager(UDPRelayOptions{
		Allocator: allocator, Dialer: &fakeProxyDialer{},
		Firewall:    &fakeRelayFirewall{openError: errors.New("nft failed")},
		IdleTimeout: time.Minute, MaxAssociations: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NewSOCKS5Proxy(SOCKS5Options{Dialer: &fakeProxyDialer{}, UDPRelays: relays, HandshakeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	clientPipe, serverPipe := net.Pipe()
	client := &addressedConn{Conn: clientPipe, local: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 53000}}
	server := &addressedConn{Conn: serverPipe, local: &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1080}, remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 53000}}
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		_, serveErr := proxy.ServeConn(context.Background(), server)
		done <- serveErr
	}()
	_, _ = client.Write(statute.NewMethodRequest(statute.VersionSocks5, []byte{statute.MethodNoAuth}).Bytes())
	_, _ = statute.ParseMethodReply(client)
	request := statute.Request{
		Version: statute.VersionSocks5, Command: statute.CommandAssociate,
		DstAddr: statute.AddrSpec{AddrType: statute.ATYPIPv4, IP: net.IPv4zero},
	}
	_, _ = client.Write(request.Bytes())
	reply, err := statute.ParseReply(client)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Response != statute.RepServerFailure {
		t.Fatalf("reply response = %d", reply.Response)
	}
	if serveErr := <-done; serveErr == nil {
		t.Fatal("ServeConn() error = nil")
	}
	select {
	case <-binder.packet.closed:
	default:
		t.Fatal("failed firewall transaction leaked UDP socket")
	}
}

func startUDPAssociation(t *testing.T, proxy *SOCKS5Proxy, clientIP string, port int) (net.Conn, <-chan error) {
	t.Helper()
	return startUDPAssociationExpecting(t, proxy, clientIP, port, statute.RepSuccess)
}

func startUDPAssociationExpecting(t *testing.T, proxy *SOCKS5Proxy, clientIP string, port int, response byte) (net.Conn, <-chan error) {
	t.Helper()
	clientPipe, serverPipe := net.Pipe()
	client := &addressedConn{
		Conn:   clientPipe,
		local:  &net.TCPAddr{IP: net.ParseIP(clientIP), Port: port},
		remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1080},
	}
	server := &addressedConn{
		Conn:   serverPipe,
		local:  &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1080},
		remote: &net.TCPAddr{IP: net.ParseIP(clientIP), Port: port},
	}
	done := make(chan error, 1)
	go func() {
		_, serveErr := proxy.ServeConn(context.Background(), server)
		done <- serveErr
	}()
	_, _ = client.Write(statute.NewMethodRequest(statute.VersionSocks5, []byte{statute.MethodNoAuth}).Bytes())
	if _, err := statute.ParseMethodReply(client); err != nil {
		t.Fatal(err)
	}
	request := statute.Request{
		Version: statute.VersionSocks5, Command: statute.CommandAssociate,
		DstAddr: statute.AddrSpec{AddrType: statute.ATYPIPv4, IP: net.IPv4zero},
	}
	_, _ = client.Write(request.Bytes())
	reply, err := statute.ParseReply(client)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Response != response {
		t.Fatalf("association response = %d, want %d", reply.Response, response)
	}
	return client, done
}
