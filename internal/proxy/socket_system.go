package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"syscall"
	"time"
)

type socketListenFunc func(context.Context, string, string, func(string, string, syscall.RawConn) error) (io.Closer, error)
type socketDialFunc func(context.Context, string, string, *net.Dialer) (net.Conn, error)

type SystemSocketBinder struct {
	listen socketListenFunc
}

func NewSystemSocketBinder() *SystemSocketBinder {
	return &SystemSocketBinder{listen: listenSocket}
}

func (b *SystemSocketBinder) Bind(ctx context.Context, endpoint BindEndpoint) (io.Closer, error) {
	if b == nil || b.listen == nil {
		return nil, errors.New("system socket listener is unavailable")
	}
	network, address, err := bindEndpointAddress(endpoint)
	if err != nil {
		return nil, err
	}
	control := makeSocketControl(endpoint.Interface, endpoint.Freebind)
	closer, err := b.listen(ctx, network, address, control)
	if err != nil {
		return nil, fmt.Errorf("listen %s %s: %w", network, address, err)
	}
	if closer == nil {
		return nil, errors.New("system listener returned a nil socket")
	}
	return closer, nil
}

func bindEndpointAddress(endpoint BindEndpoint) (string, string, error) {
	if endpoint.Port == 0 {
		return "", "", errors.New("bind port must be non-zero")
	}
	var network string
	switch endpoint.Protocol {
	case BindTCP:
		network = "tcp"
	case BindUDP:
		network = "udp"
	default:
		return "", "", fmt.Errorf("unsupported bind protocol %q", endpoint.Protocol)
	}
	var address netip.Addr
	switch endpoint.Family {
	case BindIPv4:
		network += "4"
		address = netip.IPv4Unspecified()
		if endpoint.Address.IsValid() {
			address = endpoint.Address.Unmap()
			if !address.Is4() {
				return "", "", fmt.Errorf("address %s does not match IPv4 bind", address)
			}
		}
		if endpoint.Freebind {
			return "", "", errors.New("IPv6 freebind cannot be used with IPv4")
		}
	case BindIPv6:
		network += "6"
		address = netip.IPv6Unspecified()
		if endpoint.Address.IsValid() {
			address = endpoint.Address.Unmap()
			if !address.Is6() || address.Is4In6() {
				return "", "", fmt.Errorf("address %s does not match IPv6 bind", address)
			}
		}
		if endpoint.Freebind && !endpoint.Address.IsValid() {
			return "", "", errors.New("IPv6 freebind requires a specific address")
		}
	default:
		return "", "", fmt.Errorf("unsupported bind family %q", endpoint.Family)
	}
	return network, netip.AddrPortFrom(address, endpoint.Port).String(), nil
}

func listenSocket(ctx context.Context, network, address string, control func(string, string, syscall.RawConn) error) (io.Closer, error) {
	config := net.ListenConfig{Control: control}
	if strings.HasPrefix(network, "tcp") {
		return config.Listen(ctx, network, address)
	}
	return config.ListenPacket(ctx, network, address)
}

type SystemConnector struct {
	interfaceName string
	freebind      bool
	dial          socketDialFunc
}

func NewSystemConnector(interfaceName string, freebind bool) *SystemConnector {
	return &SystemConnector{interfaceName: interfaceName, freebind: freebind, dial: dialSocket}
}

func (c *SystemConnector) DialContext(ctx context.Context, network string, destination netip.AddrPort, source netip.Addr, timeout time.Duration) (net.Conn, error) {
	if c == nil || c.dial == nil {
		return nil, errors.New("system socket dialer is unavailable")
	}
	if network != "tcp6" && network != "udp6" {
		return nil, fmt.Errorf("system connector requires tcp6 or udp6, got %q", network)
	}
	if !destination.IsValid() || destination.Port() == 0 || !isNativeIPv6(destination.Addr()) {
		return nil, fmt.Errorf("destination %s is not a valid IPv6 endpoint", destination)
	}
	if !isNativeIPv6(source) {
		return nil, fmt.Errorf("source %s is not a valid IPv6 address", source)
	}
	if timeout <= 0 {
		return nil, errors.New("dial timeout must be positive")
	}

	var local net.Addr
	if network == "tcp6" {
		local = &net.TCPAddr{IP: net.IP(source.AsSlice())}
	} else {
		local = &net.UDPAddr{IP: net.IP(source.AsSlice())}
	}
	dialer := &net.Dialer{
		Timeout:   timeout,
		LocalAddr: local,
		Control:   makeSocketControl(c.interfaceName, c.freebind),
	}
	conn, err := c.dial(ctx, network, destination.String(), dialer)
	if err != nil {
		return nil, fmt.Errorf("dial %s %s from %s: %w", network, destination, source, err)
	}
	if conn == nil {
		return nil, errors.New("system dialer returned a nil connection")
	}
	return conn, nil
}

func dialSocket(ctx context.Context, network, address string, dialer *net.Dialer) (net.Conn, error) {
	return dialer.DialContext(ctx, network, address)
}

func isNativeIPv6(address netip.Addr) bool {
	return address.IsValid() && address.Is6() && !address.Is4In6()
}
