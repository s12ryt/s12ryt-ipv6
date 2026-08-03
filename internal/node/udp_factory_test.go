package node

import (
	"context"
	"errors"
	"io"
	"net/netip"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type discardSocketBinder struct{}

func (discardSocketBinder) Bind(context.Context, proxy.BindEndpoint) (io.Closer, error) {
	return nil, errors.New("not used")
}

type inertUDPFirewall struct{}

func (inertUDPFirewall) Open(context.Context, proxy.BindEndpoint) error  { return nil }
func (inertUDPFirewall) Close(context.Context, proxy.BindEndpoint) error { return nil }

func TestUDPRelayFactorySelectsExactInboundBeforeWildcard(t *testing.T) {
	config := validConfig("node-1", "primary")
	exact := netip.MustParseAddr("2001:4860:1::10")
	config.Inbound = []proxy.BindSpec{
		{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Interface: "wildcard"},
		{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: exact, Interface: "eth9", Freebind: true},
		{Protocol: proxy.BindTCP, Family: proxy.BindIPv4},
	}

	interfaceName, freebind, err := selectUDPRelayBind(config, exact, proxy.BindIPv6)
	if err != nil {
		t.Fatal(err)
	}
	if interfaceName != "eth9" || !freebind {
		t.Fatalf("exact bind = %q/%t", interfaceName, freebind)
	}
	interfaceName, freebind, err = selectUDPRelayBind(config, netip.MustParseAddr("2001:4860:1::20"), proxy.BindIPv6)
	if err != nil {
		t.Fatal(err)
	}
	if interfaceName != "wildcard" || freebind {
		t.Fatalf("wildcard bind = %q/%t", interfaceName, freebind)
	}
	if _, _, err := selectUDPRelayBind(config, netip.MustParseAddr("192.0.2.1"), proxy.BindIPv6); err == nil {
		t.Fatal("family mismatch error = nil")
	}
}

func TestUDPRelayFactoryBuildsManagerAndValidatesDependencies(t *testing.T) {
	allocator, err := proxy.NewPortAllocator(51000, 51000, discardSocketBinder{})
	if err != nil {
		t.Fatal(err)
	}
	factory, err := NewUDPRelayFactory(UDPRelayFactoryOptions{Allocator: allocator, Firewall: inertUDPFirewall{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factory.BuildUDPRelay(validConfig("node-1", "primary"), nil); err == nil {
		t.Fatal("BuildUDPRelay(nil dialer) error = nil")
	}
	if relay, err := factory.BuildUDPRelay(validConfig("node-1", "primary"), inertProxyDialer{}); err != nil || relay == nil {
		t.Fatalf("BuildUDPRelay() = %T, %v", relay, err)
	}
	if _, err := NewUDPRelayFactory(UDPRelayFactoryOptions{}); err == nil {
		t.Fatal("NewUDPRelayFactory(empty) error = nil")
	}
}

func TestUDPRelayFactoryTranslatesAssociationLifecycle(t *testing.T) {
	allocator, _ := proxy.NewPortAllocator(51000, 51000, discardSocketBinder{})
	events := make(chan TrafficEvent, 2)
	factory, err := NewUDPRelayFactory(UDPRelayFactoryOptions{
		Allocator: allocator, Firewall: inertUDPFirewall{},
		Observe: func(event TrafficEvent) { events <- event },
	})
	if err != nil {
		t.Fatal(err)
	}
	source := netip.MustParseAddr("2001:4860:1::20")
	factory.observeAssociation("node-1", proxy.UDPAssociationEvent{
		Lifecycle: proxy.UDPAssociationOpened, SourceIP: source,
	})
	factory.observeAssociation("node-1", proxy.UDPAssociationEvent{
		Lifecycle: proxy.UDPAssociationClosed, SourceIP: source,
		Traffic: proxy.ProxyTraffic{Protocol: "socks", UpBytes: 12, DownBytes: 34},
		Error:   errors.New("relay failed"),
	})
	opened := <-events
	closed := <-events
	if opened.Lifecycle != TrafficUDPOpened || opened.NodeID != "node-1" || opened.SourceIP != source {
		t.Fatalf("opened event = %#v", opened)
	}
	if closed.Lifecycle != TrafficUDPClosed || closed.Traffic.UpBytes != 12 || closed.Error == nil {
		t.Fatalf("closed event = %#v", closed)
	}
}
