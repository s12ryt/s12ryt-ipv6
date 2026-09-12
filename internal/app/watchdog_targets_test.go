package app

import (
	"net/netip"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/node"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type inboundResolverFunc func(node.Config) (node.Config, error)

func (f inboundResolverFunc) Resolve(config node.Config) (node.Config, error) {
	return f(config)
}

func TestBuildWatchdogTargetsResolvesModernInboundDeclaration(t *testing.T) {
	declaration := node.Config{
		ID:              "node-1",
		Name:            "proxy-1",
		Protocol:        node.ProtocolSOCKS,
		Username:        "user",
		Password:        "pass",
		Port:            1080,
		InboundMode:     node.InboundIPv6,
		InboundResource: "pool-in",
	}
	resolver := inboundResolverFunc(func(config node.Config) (node.Config, error) {
		config.Inbound = []proxy.BindSpec{{
			Protocol: proxy.BindTCP,
			Family:   proxy.BindIPv6,
			Address:  netip.MustParseAddr("2001:db8::10"),
		}}
		return config, nil
	})

	targets := buildWatchdogTargets([]node.Node{{
		Config: declaration,
		Status: node.StatusRunning,
	}}, resolver)
	if len(targets) != 1 {
		t.Fatalf("buildWatchdogTargets() returned %d targets, want 1", len(targets))
	}
	want := ProbeTarget{
		Node: "proxy-1", Address: "[2001:db8::10]:1080", Protocol: "socks", Username: "user", Password: "pass",
	}
	if targets[0] != want {
		t.Fatalf("buildWatchdogTargets()[0] = %+v, want %+v", targets[0], want)
	}
}
