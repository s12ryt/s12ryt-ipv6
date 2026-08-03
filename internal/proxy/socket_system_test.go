package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"syscall"
	"testing"
	"time"
)

type listenCall struct {
	network    string
	address    string
	controlSet bool
}

type dialSocketCall struct {
	network    string
	address    string
	localAddr  net.Addr
	timeout    time.Duration
	controlSet bool
}

func TestSystemSocketBinderUsesExplicitFamilyAndSocketOptions(t *testing.T) {
	var calls []listenCall
	binder := &SystemSocketBinder{listen: func(_ context.Context, network, address string, control func(string, string, syscall.RawConn) error) (io.Closer, error) {
		calls = append(calls, listenCall{network: network, address: address, controlSet: control != nil})
		return &fakeBoundSocket{}, nil
	}}

	closer, err := binder.Bind(context.Background(), BindEndpoint{
		Protocol: BindTCP,
		Family:   BindIPv4,
		Port:     41000,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	closer, err = binder.Bind(context.Background(), BindEndpoint{
		Protocol:  BindUDP,
		Family:    BindIPv6,
		Address:   netip.MustParseAddr("2001:4860:1::10"),
		Port:      41001,
		Interface: "eth0",
		Freebind:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()

	want := []listenCall{
		{network: "tcp4", address: "0.0.0.0:41000"},
		{network: "udp6", address: "[2001:4860:1::10]:41001", controlSet: true},
	}
	if len(calls) != len(want) {
		t.Fatalf("listen calls = %#v", calls)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("listen call %d = %#v, want %#v", index, calls[index], want[index])
		}
	}
}

func TestSystemSocketBinderRejectsInvalidEndpointBeforeListen(t *testing.T) {
	called := false
	binder := &SystemSocketBinder{listen: func(context.Context, string, string, func(string, string, syscall.RawConn) error) (io.Closer, error) {
		called = true
		return &fakeBoundSocket{}, nil
	}}
	invalid := []BindEndpoint{
		{},
		{Protocol: BindTCP, Family: BindIPv6},
		{Protocol: BindTCP, Family: BindIPv4, Address: netip.MustParseAddr("2001:4860::1"), Port: 1},
		{Protocol: BindTCP, Family: BindIPv6, Address: netip.MustParseAddr("192.0.2.1"), Port: 1},
		{Protocol: BindTCP, Family: BindIPv4, Port: 1, Freebind: true},
	}
	for _, endpoint := range invalid {
		if _, err := binder.Bind(context.Background(), endpoint); err == nil {
			t.Fatalf("Bind(%#v) error = nil", endpoint)
		}
	}
	if called {
		t.Fatal("invalid endpoint reached listen")
	}
}

func TestSystemConnectorBindsIPv6SourceAndUsesIPv6Network(t *testing.T) {
	var call dialSocketCall
	underlying := &stubConn{}
	connector := &SystemConnector{
		interfaceName: "eth0",
		freebind:      true,
		dial: func(_ context.Context, network, address string, dialer *net.Dialer) (net.Conn, error) {
			call = dialSocketCall{
				network: network, address: address, localAddr: dialer.LocalAddr,
				timeout: dialer.Timeout, controlSet: dialer.Control != nil,
			}
			return underlying, nil
		},
	}
	destination := netip.MustParseAddrPort("[2606:4700:4700::1111]:443")
	source := netip.MustParseAddr("2001:4860:1::10")
	conn, err := connector.DialContext(context.Background(), "tcp6", destination, source, 7*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if conn != underlying {
		t.Fatal("DialContext returned a different connection")
	}
	local, ok := call.localAddr.(*net.TCPAddr)
	if !ok || local.IP.String() != source.String() || local.Port != 0 {
		t.Fatalf("local address = %#v", call.localAddr)
	}
	if call.network != "tcp6" || call.address != destination.String() || call.timeout != 7*time.Second || !call.controlSet {
		t.Fatalf("dial call = %#v", call)
	}
}

func TestSystemConnectorValidatesIPv6OnlyArguments(t *testing.T) {
	called := false
	connector := &SystemConnector{dial: func(context.Context, string, string, *net.Dialer) (net.Conn, error) {
		called = true
		return nil, errors.New("unexpected")
	}}
	validDestination := netip.MustParseAddrPort("[2606:4700:4700::1111]:443")
	validSource := netip.MustParseAddr("2001:4860:1::10")
	tests := []struct {
		network     string
		destination netip.AddrPort
		source      netip.Addr
		timeout     time.Duration
	}{
		{"tcp", validDestination, validSource, time.Second},
		{"udp4", validDestination, validSource, time.Second},
		{"tcp6", netip.MustParseAddrPort("192.0.2.1:443"), validSource, time.Second},
		{"tcp6", validDestination, netip.MustParseAddr("192.0.2.1"), time.Second},
		{"tcp6", validDestination, validSource, 0},
	}
	for _, test := range tests {
		if _, err := connector.DialContext(context.Background(), test.network, test.destination, test.source, test.timeout); err == nil {
			t.Fatalf("DialContext(%q, %s, %s, %s) error = nil", test.network, test.destination, test.source, test.timeout)
		}
	}
	if called {
		t.Fatal("invalid dial reached network dialer")
	}
}
