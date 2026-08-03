package app

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type recordingRuntimeFirewall struct {
	openNodeID         string
	openNodeEndpoints  []proxy.BindEndpoint
	closeNodeID        string
	closeNodeEndpoints []proxy.BindEndpoint
	openedRelay        proxy.BindEndpoint
	closedRelay        proxy.BindEndpoint
	err                error
}

func (f *recordingRuntimeFirewall) OpenNode(_ context.Context, id string, endpoints []proxy.BindEndpoint) error {
	f.openNodeID = id
	f.openNodeEndpoints = append([]proxy.BindEndpoint(nil), endpoints...)
	return f.err
}

func (f *recordingRuntimeFirewall) CloseNode(_ context.Context, id string, endpoints []proxy.BindEndpoint) error {
	f.closeNodeID = id
	f.closeNodeEndpoints = append([]proxy.BindEndpoint(nil), endpoints...)
	return f.err
}

func (f *recordingRuntimeFirewall) Open(_ context.Context, endpoint proxy.BindEndpoint) error {
	f.openedRelay = endpoint
	return f.err
}

func (f *recordingRuntimeFirewall) Close(_ context.Context, endpoint proxy.BindEndpoint) error {
	f.closedRelay = endpoint
	return f.err
}

func TestDeferredRuntimeFirewallRejectsUseBeforeConfiguration(t *testing.T) {
	firewall := NewDeferredRuntimeFirewall()
	endpoint := proxy.BindEndpoint{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: netip.MustParseAddr("2001:4860::1"), Port: 443}
	if err := firewall.OpenNode(context.Background(), "node-a", []proxy.BindEndpoint{endpoint}); err == nil {
		t.Fatal("OpenNode() error = nil")
	}
	if err := firewall.CloseNode(context.Background(), "node-a", []proxy.BindEndpoint{endpoint}); err == nil {
		t.Fatal("CloseNode() error = nil")
	}
	if err := firewall.Open(context.Background(), endpoint); err == nil {
		t.Fatal("Open() error = nil")
	}
	if err := firewall.Close(context.Background(), endpoint); err == nil {
		t.Fatal("Close() error = nil")
	}
}

func TestDeferredRuntimeFirewallBindsOnceAndDelegates(t *testing.T) {
	firewall := NewDeferredRuntimeFirewall()
	if err := firewall.Set(nil); err == nil {
		t.Fatal("Set(nil) error = nil")
	}
	wantErr := errors.New("backend failed")
	target := &recordingRuntimeFirewall{err: wantErr}
	if err := firewall.Set(target); err != nil {
		t.Fatal(err)
	}
	if err := firewall.Set(&recordingRuntimeFirewall{}); err == nil {
		t.Fatal("second Set() error = nil")
	}

	nodeEndpoint := proxy.BindEndpoint{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Address: netip.MustParseAddr("2001:4860::2"), Port: 1080}
	relayEndpoint := proxy.BindEndpoint{Protocol: proxy.BindUDP, Family: proxy.BindIPv6, Address: nodeEndpoint.Address, Port: 52000}
	if err := firewall.OpenNode(context.Background(), "node-a", []proxy.BindEndpoint{nodeEndpoint}); !errors.Is(err, wantErr) {
		t.Fatalf("OpenNode() error = %v", err)
	}
	if err := firewall.CloseNode(context.Background(), "node-a", []proxy.BindEndpoint{nodeEndpoint}); !errors.Is(err, wantErr) {
		t.Fatalf("CloseNode() error = %v", err)
	}
	if err := firewall.Open(context.Background(), relayEndpoint); !errors.Is(err, wantErr) {
		t.Fatalf("Open() error = %v", err)
	}
	if err := firewall.Close(context.Background(), relayEndpoint); !errors.Is(err, wantErr) {
		t.Fatalf("Close() error = %v", err)
	}

	if target.openNodeID != "node-a" || len(target.openNodeEndpoints) != 1 || target.openNodeEndpoints[0] != nodeEndpoint {
		t.Fatalf("OpenNode() call = %q, %v", target.openNodeID, target.openNodeEndpoints)
	}
	if target.closeNodeID != "node-a" || len(target.closeNodeEndpoints) != 1 || target.closeNodeEndpoints[0] != nodeEndpoint {
		t.Fatalf("CloseNode() call = %q, %v", target.closeNodeID, target.closeNodeEndpoints)
	}
	if target.openedRelay != relayEndpoint || target.closedRelay != relayEndpoint {
		t.Fatalf("relay calls = open %v, close %v", target.openedRelay, target.closedRelay)
	}
}
