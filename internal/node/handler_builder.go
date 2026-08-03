package node

import (
	"errors"
	"fmt"

	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type NodeDialerFactory interface {
	BuildDialer(Config) (proxy.ProxyDialer, error)
}

type NodeUDPRelayFactory interface {
	BuildUDPRelay(Config, proxy.ProxyDialer) (*proxy.UDPRelayManager, error)
}

type ProtocolHandlerBuilder struct {
	dialers NodeDialerFactory
	relays  NodeUDPRelayFactory
}

func NewProtocolHandlerBuilder(dialers NodeDialerFactory, relays NodeUDPRelayFactory) (*ProtocolHandlerBuilder, error) {
	if dialers == nil {
		return nil, errors.New("node dialer factory is required")
	}
	if relays == nil {
		return nil, errors.New("node UDP relay factory is required")
	}
	return &ProtocolHandlerBuilder{dialers: dialers, relays: relays}, nil
}

func (b *ProtocolHandlerBuilder) Build(config Config) (proxy.ConnectionProxy, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	dialer, err := b.dialers.BuildDialer(config)
	if err != nil {
		return nil, fmt.Errorf("build node dialer: %w", err)
	}
	if dialer == nil {
		return nil, errors.New("node dialer factory returned nil")
	}

	httpOptions := proxy.HTTPProxyOptions{
		Dialer: dialer, Username: config.Username, Password: config.Password,
		HandshakeTimeout: config.HandshakeTimeout, TunnelIdleTimeout: config.TunnelIdleTimeout,
	}
	if config.Protocol == ProtocolHTTP {
		return proxy.NewHTTPProxy(httpOptions)
	}

	relay, err := b.relays.BuildUDPRelay(config, dialer)
	if err != nil {
		return nil, fmt.Errorf("build node UDP relay: %w", err)
	}
	if relay == nil {
		return nil, errors.New("node UDP relay factory returned nil")
	}
	socksOptions := proxy.SOCKS5Options{
		Dialer: dialer, UDPRelays: relay, Username: config.Username, Password: config.Password,
		HandshakeTimeout: config.HandshakeTimeout, TunnelIdleTimeout: config.TunnelIdleTimeout,
	}
	socksHandler, err := proxy.NewSOCKS5Proxy(socksOptions)
	if err != nil {
		return nil, err
	}
	if config.Protocol == ProtocolSOCKS {
		return socksHandler, nil
	}
	httpHandler, err := proxy.NewHTTPProxy(httpOptions)
	if err != nil {
		return nil, err
	}
	return proxy.NewMixedProxy(proxy.MixedProxyOptions{
		SOCKS5: socksHandler, HTTP: httpHandler, HandshakeTimeout: config.HandshakeTimeout,
	})
}

var _ HandlerBuilder = (*ProtocolHandlerBuilder)(nil)
