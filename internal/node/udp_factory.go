package node

import (
	"errors"
	"fmt"
	"net/netip"

	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type UDPRelayFactoryOptions struct {
	Allocator *proxy.PortAllocator
	Firewall  proxy.UDPRelayFirewall
	Observe   func(TrafficEvent)
}

type UDPRelayFactory struct {
	allocator *proxy.PortAllocator
	firewall  proxy.UDPRelayFirewall
	observe   func(TrafficEvent)
}

func NewUDPRelayFactory(options UDPRelayFactoryOptions) (*UDPRelayFactory, error) {
	if options.Allocator == nil {
		return nil, errors.New("UDP relay port allocator is required")
	}
	if options.Firewall == nil {
		return nil, errors.New("UDP relay firewall is required")
	}
	return &UDPRelayFactory{allocator: options.Allocator, firewall: options.Firewall, observe: options.Observe}, nil
}

func (f *UDPRelayFactory) BuildUDPRelay(config Config, dialer proxy.ProxyDialer) (*proxy.UDPRelayManager, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if dialer == nil {
		return nil, errors.New("UDP relay dialer is required")
	}
	return proxy.NewUDPRelayManager(proxy.UDPRelayOptions{
		Allocator: f.allocator, Dialer: dialer, Firewall: f.firewall,
		IdleTimeout: config.UDPIdleTimeout, MaxAssociations: config.MaxUDP,
		BindSelector: func(address netip.Addr, family proxy.BindFamily) (string, bool, error) {
			return selectUDPRelayBind(config, address, family)
		},
		Observe: func(event proxy.UDPAssociationEvent) {
			f.observeAssociation(config.ID, event)
		},
	})
}

func (f *UDPRelayFactory) observeAssociation(nodeID string, event proxy.UDPAssociationEvent) {
	if f.observe == nil {
		return
	}
	lifecycle := TrafficUDPOpened
	if event.Lifecycle == proxy.UDPAssociationClosed {
		lifecycle = TrafficUDPClosed
	}
	f.observe(TrafficEvent{
		Lifecycle: lifecycle, NodeID: nodeID, SourceIP: event.SourceIP,
		Traffic: event.Traffic, Error: event.Error,
	})
}

func selectUDPRelayBind(config Config, address netip.Addr, family proxy.BindFamily) (string, bool, error) {
	address = address.Unmap()
	if !address.IsValid() || (family == proxy.BindIPv4 && !address.Is4()) ||
		(family == proxy.BindIPv6 && (!address.Is6() || address.Is4In6())) {
		return "", false, errors.New("UDP relay local address does not match its family")
	}
	var wildcard *proxy.BindSpec
	for index := range config.Inbound {
		candidate := &config.Inbound[index]
		if candidate.Protocol != proxy.BindTCP || candidate.Family != family {
			continue
		}
		if candidate.Address.IsValid() && candidate.Address.Unmap() == address {
			return candidate.Interface, candidate.Freebind, nil
		}
		if !candidate.Address.IsValid() && wildcard == nil {
			wildcard = candidate
		}
	}
	if wildcard != nil {
		return wildcard.Interface, wildcard.Freebind, nil
	}
	return "", false, fmt.Errorf("no node inbound binding covers local address %s", address)
}

var _ NodeUDPRelayFactory = (*UDPRelayFactory)(nil)
