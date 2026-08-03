package node

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type inertProxyDialer struct{}

func (inertProxyDialer) Dial(context.Context, string, string, uint16) (net.Conn, proxy.DialMetadata, error) {
	return nil, proxy.DialMetadata{}, errors.New("not used")
}

type recordingNodeDialerFactory struct {
	configs []Config
	dialer  proxy.ProxyDialer
	err     error
}

func (f *recordingNodeDialerFactory) BuildDialer(config Config) (proxy.ProxyDialer, error) {
	f.configs = append(f.configs, cloneConfig(config))
	return f.dialer, f.err
}

type recordingUDPRelayFactory struct {
	configs []Config
	dialers []proxy.ProxyDialer
	relay   *proxy.UDPRelayManager
	err     error
}

func (f *recordingUDPRelayFactory) BuildUDPRelay(config Config, dialer proxy.ProxyDialer) (*proxy.UDPRelayManager, error) {
	f.configs = append(f.configs, cloneConfig(config))
	f.dialers = append(f.dialers, dialer)
	return f.relay, f.err
}

func TestProtocolHandlerBuilderBuildsConfiguredProtocol(t *testing.T) {
	tests := []struct {
		protocol   Protocol
		wantType   any
		wantRelays int
	}{
		{protocol: ProtocolHTTP, wantType: (*proxy.HTTPProxy)(nil)},
		{protocol: ProtocolSOCKS, wantType: (*proxy.SOCKS5Proxy)(nil), wantRelays: 1},
		{protocol: ProtocolMixed, wantType: (*proxy.MixedProxy)(nil), wantRelays: 1},
	}
	for _, test := range tests {
		t.Run(string(test.protocol), func(t *testing.T) {
			dialer := inertProxyDialer{}
			dialers := &recordingNodeDialerFactory{dialer: dialer}
			relays := &recordingUDPRelayFactory{relay: new(proxy.UDPRelayManager)}
			builder, err := NewProtocolHandlerBuilder(dialers, relays)
			if err != nil {
				t.Fatal(err)
			}
			config := validConfig("node-1", "primary")
			config.Protocol = test.protocol
			handler, err := builder.Build(config)
			if err != nil {
				t.Fatal(err)
			}
			if reflect.TypeOf(handler) != reflect.TypeOf(test.wantType) {
				t.Fatalf("handler type = %T, want %T", handler, test.wantType)
			}
			if len(dialers.configs) != 1 || !reflect.DeepEqual(dialers.configs[0], config) {
				t.Fatalf("dialer configs = %#v", dialers.configs)
			}
			if len(relays.configs) != test.wantRelays {
				t.Fatalf("relay builds = %d, want %d", len(relays.configs), test.wantRelays)
			}
			if test.wantRelays == 1 && relays.dialers[0] != dialer {
				t.Fatal("UDP relay did not receive the node dialer")
			}
		})
	}
}

func TestProtocolHandlerBuilderPropagatesFactoryFailures(t *testing.T) {
	dialFailure := errors.New("source pool unavailable")
	builder, err := NewProtocolHandlerBuilder(
		&recordingNodeDialerFactory{err: dialFailure},
		&recordingUDPRelayFactory{relay: new(proxy.UDPRelayManager)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Build(validConfig("node-1", "primary")); !errors.Is(err, dialFailure) {
		t.Fatalf("Build() error = %v, want dial failure", err)
	}

	relayFailure := errors.New("UDP relay unavailable")
	builder, _ = NewProtocolHandlerBuilder(
		&recordingNodeDialerFactory{dialer: inertProxyDialer{}},
		&recordingUDPRelayFactory{err: relayFailure},
	)
	if _, err := builder.Build(validConfig("node-1", "primary")); !errors.Is(err, relayFailure) {
		t.Fatalf("Build() error = %v, want relay failure", err)
	}
}

func TestProtocolHandlerBuilderValidatesDependenciesAndNodeTimeouts(t *testing.T) {
	dialers := &recordingNodeDialerFactory{dialer: inertProxyDialer{}}
	relays := &recordingUDPRelayFactory{relay: new(proxy.UDPRelayManager)}
	if _, err := NewProtocolHandlerBuilder(nil, relays); err == nil {
		t.Fatal("NewProtocolHandlerBuilder(nil dialers) error = nil")
	}
	if _, err := NewProtocolHandlerBuilder(dialers, nil); err == nil {
		t.Fatal("NewProtocolHandlerBuilder(nil relays) error = nil")
	}

	invalid := []Config{
		func() Config { c := validConfig("node-1", "primary"); c.DialTimeout = 0; return c }(),
		func() Config { c := validConfig("node-1", "primary"); c.TunnelIdleTimeout = -1; return c }(),
		func() Config { c := validConfig("node-1", "primary"); c.UDPIdleTimeout = 0; return c }(),
		func() Config { c := validConfig("node-1", "primary"); c.Outbound = ""; return c }(),
		func() Config { c := validConfig("node-1", "primary"); c.ULAOverride = "sometimes"; return c }(),
	}
	builder, _ := NewProtocolHandlerBuilder(dialers, relays)
	for _, config := range invalid {
		if _, err := builder.Build(config); err == nil {
			t.Fatalf("Build(%#v) error = nil", config)
		}
	}
}
