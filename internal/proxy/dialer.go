package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/dns64"
	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
)

type DestinationResolver interface {
	Resolve(context.Context, string, policy.DestinationPolicy, policy.ULAOverride, netip.Prefix) (dns64.Resolution, error)
}

type Connector interface {
	DialContext(context.Context, string, netip.AddrPort, netip.Addr, time.Duration) (net.Conn, error)
}

type DialerOptions struct {
	Resolver    DestinationResolver
	Sources     *SourcePool
	Connector   Connector
	Policy      func() policy.DestinationPolicy
	NAT64Prefix func() netip.Prefix
	ULAOverride policy.ULAOverride
	Timeout     time.Duration
}

type DialMetadata struct {
	Source      netip.Addr
	Destination netip.AddrPort
	Resolver    string
	Synthesized bool
}

type Dialer struct {
	resolver    DestinationResolver
	sources     *SourcePool
	connector   Connector
	policy      func() policy.DestinationPolicy
	nat64Prefix func() netip.Prefix
	ulaOverride policy.ULAOverride
	timeout     time.Duration
}

func NewDialer(options DialerOptions) (*Dialer, error) {
	if options.Resolver == nil {
		return nil, errors.New("destination resolver is required")
	}
	if options.Sources == nil {
		return nil, errors.New("source address pool is required")
	}
	if options.Connector == nil {
		return nil, errors.New("IPv6 connector is required")
	}
	if options.Policy == nil {
		return nil, errors.New("destination policy provider is required")
	}
	if options.NAT64Prefix == nil {
		return nil, errors.New("NAT64 prefix provider is required")
	}
	if options.Timeout <= 0 {
		return nil, errors.New("dial timeout must be positive")
	}
	switch options.ULAOverride {
	case "", policy.ULAInherit:
		options.ULAOverride = policy.ULAInherit
	case policy.ULAAllow, policy.ULADeny:
	default:
		return nil, fmt.Errorf("invalid ULA override %q", options.ULAOverride)
	}
	return &Dialer{
		resolver: options.Resolver, sources: options.Sources, connector: options.Connector,
		policy: options.Policy, nat64Prefix: options.NAT64Prefix,
		ulaOverride: options.ULAOverride, timeout: options.Timeout,
	}, nil
}

func (d *Dialer) Dial(ctx context.Context, network, host string, port uint16) (net.Conn, DialMetadata, error) {
	ipv6Network, err := ipv6Network(network)
	if err != nil {
		return nil, DialMetadata{}, err
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, DialMetadata{}, errors.New("destination host is required")
	}
	if port == 0 {
		return nil, DialMetadata{}, errors.New("destination port must be non-zero")
	}
	resolution, err := d.resolver.Resolve(ctx, host, d.policy(), d.ulaOverride, d.nat64Prefix())
	if err != nil {
		return nil, DialMetadata{}, fmt.Errorf("resolve destination %q: %w", host, err)
	}
	if len(resolution.Addresses) == 0 {
		return nil, DialMetadata{}, errors.New("resolver returned no destination addresses")
	}
	for _, address := range resolution.Addresses {
		if !address.IsValid() || !address.Is6() || address.Is4In6() {
			return nil, DialMetadata{}, fmt.Errorf("resolver returned non-IPv6 destination %s", address)
		}
	}

	lease, err := d.sources.Acquire()
	if err != nil {
		return nil, DialMetadata{}, fmt.Errorf("acquire source IPv6 address: %w", err)
	}
	var failures []error
	for _, address := range resolution.Addresses {
		destination := netip.AddrPortFrom(address, port)
		conn, dialErr := d.connector.DialContext(ctx, ipv6Network, destination, lease.Address(), d.timeout)
		if dialErr != nil {
			failures = append(failures, fmt.Errorf("dial %s: %w", destination, dialErr))
			continue
		}
		if conn == nil {
			failures = append(failures, fmt.Errorf("dial %s: connector returned a nil connection", destination))
			continue
		}
		leased := &leasedConn{Conn: conn, lease: lease}
		if err := lease.Attach(leased); err != nil {
			return nil, DialMetadata{}, fmt.Errorf("attach source IPv6 lease: %w", err)
		}
		return leased, DialMetadata{
			Source: lease.Address(), Destination: destination,
			Resolver: resolution.Source, Synthesized: resolution.Synthesized,
		}, nil
	}
	lease.Release()
	return nil, DialMetadata{}, fmt.Errorf("all IPv6 destination attempts failed: %w", errors.Join(failures...))
}

func ipv6Network(network string) (string, error) {
	switch network {
	case "tcp", "tcp6":
		return "tcp6", nil
	case "udp", "udp6":
		return "udp6", nil
	default:
		return "", fmt.Errorf("unsupported proxy network %q", network)
	}
}

type leasedConn struct {
	net.Conn
	lease *SourceLease
	once  sync.Once
	err   error
}

func (c *leasedConn) Close() error {
	c.once.Do(func() {
		c.err = c.Conn.Close()
		c.lease.Release()
	})
	return c.err
}
