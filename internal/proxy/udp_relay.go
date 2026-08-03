package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	socks5 "github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/statute"
)

type UDPRelayFirewall interface {
	Open(context.Context, BindEndpoint) error
	Close(context.Context, BindEndpoint) error
}

type UDPRelayBindSelector func(netip.Addr, BindFamily) (string, bool, error)

type UDPAssociationLifecycle string

const (
	UDPAssociationOpened UDPAssociationLifecycle = "udp_opened"
	UDPAssociationClosed UDPAssociationLifecycle = "udp_closed"
)

type UDPAssociationEvent struct {
	Lifecycle UDPAssociationLifecycle
	SourceIP  netip.Addr
	Traffic   ProxyTraffic
	Error     error
}

type UDPRelayOptions struct {
	Allocator       *PortAllocator
	Dialer          ProxyDialer
	Firewall        UDPRelayFirewall
	IdleTimeout     time.Duration
	MaxAssociations int
	Interface       string
	Freebind        bool
	BindSelector    UDPRelayBindSelector
	Observe         func(UDPAssociationEvent)
}

var ErrUDPAssociationLimit = errors.New("SOCKS5 UDP association limit reached")

type UDPRelayManager struct {
	allocator    *PortAllocator
	dialer       ProxyDialer
	firewall     UDPRelayFirewall
	idleTimeout  time.Duration
	interface_   string
	freebind     bool
	bindSelector UDPRelayBindSelector
	observe      func(UDPAssociationEvent)
	associations chan struct{}
}

func NewUDPRelayManager(options UDPRelayOptions) (*UDPRelayManager, error) {
	if options.Allocator == nil {
		return nil, errors.New("UDP relay port allocator is required")
	}
	if options.Dialer == nil {
		return nil, errors.New("UDP relay dialer is required")
	}
	if options.Firewall == nil {
		return nil, errors.New("UDP relay firewall is required")
	}
	if options.IdleTimeout <= 0 {
		return nil, errors.New("UDP relay idle timeout must be positive")
	}
	if options.MaxAssociations <= 0 {
		return nil, errors.New("UDP relay association limit must be positive")
	}
	return &UDPRelayManager{
		allocator: options.Allocator, dialer: options.Dialer, firewall: options.Firewall,
		idleTimeout: options.IdleTimeout, interface_: options.Interface, freebind: options.Freebind,
		bindSelector: options.BindSelector, observe: options.Observe,
		associations: make(chan struct{}, options.MaxAssociations),
	}, nil
}

func (m *UDPRelayManager) Associate(ctx context.Context, control net.Conn, reader io.Reader, request *socks5.Request) (traffic ProxyTraffic, resultErr error) {
	select {
	case m.associations <- struct{}{}:
		defer func() { <-m.associations }()
	default:
		_ = socks5.SendReply(control, statute.RepRuleFailure, nil)
		return ProxyTraffic{}, ErrUDPAssociationLimit
	}
	local, err := netAddrPort(control.LocalAddr())
	if err != nil {
		_ = socks5.SendReply(control, statute.RepServerFailure, nil)
		return ProxyTraffic{}, errors.New("SOCKS5 UDP control local address is invalid")
	}
	client, err := netAddrPort(control.RemoteAddr())
	if err != nil || !udpClientAllowed(client, request.RawDestAddr) {
		_ = socks5.SendReply(control, statute.RepRuleFailure, nil)
		return ProxyTraffic{}, errors.New("SOCKS5 UDP client address is not allowed")
	}
	family := BindIPv6
	if local.Addr().Is4() {
		family = BindIPv4
	}
	interfaceName, freebind := m.interface_, m.freebind && family == BindIPv6
	if m.bindSelector != nil {
		interfaceName, freebind, err = m.bindSelector(local.Addr(), family)
		if err != nil {
			_ = socks5.SendReply(control, statute.RepServerFailure, nil)
			return ProxyTraffic{}, fmt.Errorf("select SOCKS5 UDP relay binding: %w", err)
		}
		if family == BindIPv4 && freebind {
			_ = socks5.SendReply(control, statute.RepServerFailure, nil)
			return ProxyTraffic{}, errors.New("select SOCKS5 UDP relay binding: IPv4 cannot use freebind")
		}
	}
	reservation, err := m.allocator.Reserve(ctx, 0, []BindSpec{{
		Protocol: BindUDP, Family: family, Address: local.Addr(),
		Interface: interfaceName, Freebind: freebind,
	}})
	if err != nil {
		_ = socks5.SendReply(control, statute.RepServerFailure, nil)
		return ProxyTraffic{}, fmt.Errorf("reserve SOCKS5 UDP relay: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, reservation.Close())
	}()
	bindings := reservation.Bindings()
	if len(bindings) != 1 {
		_ = socks5.SendReply(control, statute.RepServerFailure, nil)
		return ProxyTraffic{}, errors.New("UDP relay reservation returned an unexpected binding count")
	}
	packet, ok := bindings[0].Socket.(net.PacketConn)
	if !ok {
		_ = socks5.SendReply(control, statute.RepServerFailure, nil)
		return ProxyTraffic{}, errors.New("UDP relay binding is not a packet connection")
	}
	endpoint := bindings[0].Endpoint
	if err := m.firewall.Open(ctx, endpoint); err != nil {
		_ = socks5.SendReply(control, statute.RepServerFailure, nil)
		return ProxyTraffic{}, fmt.Errorf("open SOCKS5 UDP relay firewall: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, m.firewall.Close(context.Background(), endpoint))
	}()
	if err := socks5.SendReply(control, statute.RepSuccess, packet.LocalAddr()); err != nil {
		return ProxyTraffic{}, errors.New("write SOCKS5 UDP ASSOCIATE response failed")
	}
	m.emit(UDPAssociationEvent{Lifecycle: UDPAssociationOpened, SourceIP: client.Addr()})
	defer func() {
		traffic.Protocol = "socks"
		m.emit(UDPAssociationEvent{
			Lifecycle: UDPAssociationClosed, SourceIP: client.Addr(), Traffic: traffic, Error: resultErr,
		})
	}()

	association := udpAssociation{
		packet: packet, dialer: m.dialer, idleTimeout: m.idleTimeout,
		clientIP: client.Addr(), requestedClient: request.RawDestAddr,
		mappings: make(map[string]*udpMapping),
	}
	controlClosed := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, reader)
		close(controlClosed)
		_ = reservation.Close()
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = reservation.Close()
		case <-controlClosed:
		}
	}()
	traffic, err = association.run(ctx)
	if err != nil && !isExpectedClose(err) {
		return traffic, err
	}
	return traffic, nil
}

func (m *UDPRelayManager) emit(event UDPAssociationEvent) {
	if m.observe != nil {
		m.observe(event)
	}
}

type udpAssociation struct {
	packet          net.PacketConn
	dialer          ProxyDialer
	idleTimeout     time.Duration
	clientIP        netip.Addr
	requestedClient *statute.AddrSpec

	mu       sync.Mutex
	mappings map[string]*udpMapping
	traffic  ProxyTraffic
	wg       sync.WaitGroup
}

type udpMapping struct {
	conn        net.Conn
	destination statute.AddrSpec
	client      net.Addr
	metadata    DialMetadata
	writeMu     sync.Mutex
}

func (a *udpAssociation) run(ctx context.Context) (ProxyTraffic, error) {
	defer a.closeMappings()
	buffer := make([]byte, 64*1024)
	for {
		_ = a.packet.SetReadDeadline(time.Now().Add(a.idleTimeout))
		read, source, err := a.packet.ReadFrom(buffer)
		if err != nil {
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				return a.snapshot(), nil
			}
			return a.snapshot(), err
		}
		if !a.allowedSource(source) {
			continue
		}
		datagram, err := statute.ParseDatagram(buffer[:read])
		if err != nil || datagram.Frag != 0 {
			continue
		}
		host, port, err := socksDestination(&datagram.DstAddr)
		if err != nil {
			continue
		}
		key := source.String() + "\x00" + host + "\x00" + strconv.Itoa(int(port))
		mapping, err := a.mapping(ctx, key, source, datagram.DstAddr, host, port)
		if err != nil {
			continue
		}
		mapping.writeMu.Lock()
		_ = mapping.conn.SetReadDeadline(time.Now().Add(a.idleTimeout))
		written, writeErr := mapping.conn.Write(datagram.Data)
		mapping.writeMu.Unlock()
		a.addUp(int64(written))
		if writeErr != nil {
			a.removeMapping(key, mapping)
		}
	}
}

func (a *udpAssociation) mapping(ctx context.Context, key string, client net.Addr, destination statute.AddrSpec, host string, port uint16) (*udpMapping, error) {
	a.mu.Lock()
	if mapping := a.mappings[key]; mapping != nil {
		a.mu.Unlock()
		return mapping, nil
	}
	a.mu.Unlock()
	conn, metadata, err := a.dialer.Dial(ctx, "udp", host, port)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, errors.New("UDP relay dialer returned a nil connection")
	}
	mapping := &udpMapping{conn: conn, destination: destination, client: client, metadata: metadata}
	a.mu.Lock()
	if existing := a.mappings[key]; existing != nil {
		a.mu.Unlock()
		_ = conn.Close()
		return existing, nil
	}
	a.mappings[key] = mapping
	if !a.traffic.Metadata.Source.IsValid() {
		a.traffic.Metadata = metadata
	}
	a.wg.Add(1)
	a.mu.Unlock()
	go a.readMapping(key, mapping)
	return mapping, nil
}

func (a *udpAssociation) readMapping(key string, mapping *udpMapping) {
	defer a.wg.Done()
	buffer := make([]byte, 64*1024)
	for {
		if err := mapping.conn.SetReadDeadline(time.Now().Add(a.idleTimeout)); err != nil {
			a.removeMapping(key, mapping)
			return
		}
		read, err := mapping.conn.Read(buffer)
		if err != nil {
			a.removeMapping(key, mapping)
			return
		}
		response := statute.Datagram{DstAddr: mapping.destination, Data: append([]byte(nil), buffer[:read]...)}
		written, writeErr := a.packet.WriteTo(response.Bytes(), mapping.client)
		if writeErr != nil {
			return
		}
		headerLength := len(response.Header())
		if written > headerLength {
			a.addDown(int64(written - headerLength))
		}
	}
}

func (a *udpAssociation) allowedSource(source net.Addr) bool {
	address, err := netAddrPort(source)
	if err != nil || address.Addr() != a.clientIP {
		return false
	}
	if a.requestedClient == nil || a.requestedClient.Port == 0 {
		return true
	}
	return address.Port() == uint16(a.requestedClient.Port)
}

func (a *udpAssociation) removeMapping(key string, mapping *udpMapping) {
	a.mu.Lock()
	if a.mappings[key] == mapping {
		delete(a.mappings, key)
	}
	a.mu.Unlock()
	_ = mapping.conn.Close()
}

func (a *udpAssociation) closeMappings() {
	a.mu.Lock()
	for key, mapping := range a.mappings {
		delete(a.mappings, key)
		_ = mapping.conn.Close()
	}
	a.mu.Unlock()
	a.wg.Wait()
}

func (a *udpAssociation) addUp(count int64) {
	a.mu.Lock()
	a.traffic.UpBytes += count
	a.mu.Unlock()
}

func (a *udpAssociation) addDown(count int64) {
	a.mu.Lock()
	a.traffic.DownBytes += count
	a.mu.Unlock()
}

func (a *udpAssociation) snapshot() ProxyTraffic {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.traffic
}

func netAddrPort(address net.Addr) (netip.AddrPort, error) {
	switch value := address.(type) {
	case *net.TCPAddr:
		ip, ok := netip.AddrFromSlice(value.IP)
		if !ok || value.Port < 0 || value.Port > 65535 {
			return netip.AddrPort{}, errors.New("invalid TCP address")
		}
		return netip.AddrPortFrom(ip.Unmap(), uint16(value.Port)), nil
	case *net.UDPAddr:
		ip, ok := netip.AddrFromSlice(value.IP)
		if !ok || value.Port < 0 || value.Port > 65535 {
			return netip.AddrPort{}, errors.New("invalid UDP address")
		}
		return netip.AddrPortFrom(ip.Unmap(), uint16(value.Port)), nil
	default:
		return netip.AddrPort{}, fmt.Errorf("unsupported network address %T", address)
	}
}

func udpClientAllowed(client netip.AddrPort, requested *statute.AddrSpec) bool {
	if requested == nil || len(requested.IP) == 0 || requested.IP.IsUnspecified() {
		return true
	}
	requestedIP, ok := netip.AddrFromSlice(requested.IP)
	if !ok || requestedIP.Unmap() != client.Addr() {
		return false
	}
	return requested.Port == 0 || requested.Port == int(client.Port())
}
