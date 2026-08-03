package proxy

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	socks5 "github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/statute"
)

var ErrSOCKSCommandUnsupported = errors.New("SOCKS5 command is not supported")

type SOCKS5Options struct {
	Dialer            ProxyDialer
	UDPRelays         *UDPRelayManager
	Username          string
	Password          string
	HandshakeTimeout  time.Duration
	TunnelIdleTimeout time.Duration
}

type SOCKS5Proxy struct {
	dialer            ProxyDialer
	udpRelays         *UDPRelayManager
	authenticator     socks5.Authenticator
	handshakeTimeout  time.Duration
	tunnelIdleTimeout time.Duration
}

func NewSOCKS5Proxy(options SOCKS5Options) (*SOCKS5Proxy, error) {
	if options.Dialer == nil {
		return nil, errors.New("SOCKS5 proxy dialer is required")
	}
	if options.HandshakeTimeout <= 0 {
		return nil, errors.New("SOCKS5 handshake timeout must be positive")
	}
	if options.TunnelIdleTimeout < 0 {
		return nil, errors.New("SOCKS5 tunnel idle timeout must not be negative")
	}
	if (options.Username == "") != (options.Password == "") {
		return nil, errors.New("SOCKS5 username and password must both be set or both be empty")
	}
	var authenticator socks5.Authenticator = socks5.NoAuthAuthenticator{}
	if options.Username != "" {
		authenticator = socks5.UserPassAuthenticator{Credentials: fixedCredential{
			username: options.Username,
			password: options.Password,
		}}
	}
	return &SOCKS5Proxy{
		dialer: options.Dialer, udpRelays: options.UDPRelays, authenticator: authenticator,
		handshakeTimeout: options.HandshakeTimeout, tunnelIdleTimeout: options.TunnelIdleTimeout,
	}, nil
}

func (p *SOCKS5Proxy) ServeConn(ctx context.Context, client net.Conn) (ProxyTraffic, error) {
	if client == nil {
		return ProxyTraffic{}, errors.New("SOCKS5 client connection is required")
	}
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(p.handshakeTimeout)); err != nil {
		return ProxyTraffic{}, errors.New("set SOCKS5 handshake deadline failed")
	}
	reader := bufio.NewReader(client)
	methodRequest, err := statute.ParseMethodRequest(reader)
	if err != nil || methodRequest.Ver != statute.VersionSocks5 {
		return ProxyTraffic{}, statute.ErrNotSupportVersion
	}
	if !containsMethod(methodRequest.Methods, p.authenticator.GetCode()) {
		_, _ = client.Write([]byte{statute.VersionSocks5, statute.MethodNoAcceptable})
		return ProxyTraffic{}, statute.ErrNoSupportedAuth
	}
	remote := ""
	if client.RemoteAddr() != nil {
		remote = client.RemoteAddr().String()
	}
	if _, err := p.authenticator.Authenticate(reader, client, remote); err != nil {
		return ProxyTraffic{}, err
	}
	request, err := socks5.ParseRequest(reader)
	if err != nil {
		if errors.Is(err, statute.ErrUnrecognizedAddrType) {
			_ = socks5.SendReply(client, statute.RepAddrTypeNotSupported, nil)
		}
		return ProxyTraffic{}, errors.New("parse SOCKS5 request failed")
	}
	request.LocalAddr = client.LocalAddr()
	request.RemoteAddr = client.RemoteAddr()
	if err := client.SetDeadline(time.Time{}); err != nil {
		return ProxyTraffic{}, errors.New("clear SOCKS5 handshake deadline failed")
	}

	switch request.Command {
	case statute.CommandConnect:
		return p.handleConnect(ctx, client, request)
	case statute.CommandBind:
		_ = socks5.SendReply(client, statute.RepCommandNotSupported, nil)
		return ProxyTraffic{}, ErrSOCKSCommandUnsupported
	case statute.CommandAssociate:
		if p.udpRelays == nil {
			_ = socks5.SendReply(client, statute.RepCommandNotSupported, nil)
			return ProxyTraffic{}, fmt.Errorf("%w: UDP ASSOCIATE is not configured", ErrSOCKSCommandUnsupported)
		}
		traffic, associateErr := p.udpRelays.Associate(ctx, client, request.Reader, request)
		traffic.Protocol = "socks"
		return traffic, associateErr
	default:
		_ = socks5.SendReply(client, statute.RepCommandNotSupported, nil)
		return ProxyTraffic{}, ErrSOCKSCommandUnsupported
	}
}

func (p *SOCKS5Proxy) handleConnect(ctx context.Context, client net.Conn, request *socks5.Request) (ProxyTraffic, error) {
	host, port, err := socksDestination(request.RawDestAddr)
	if err != nil {
		_ = socks5.SendReply(client, statute.RepAddrTypeNotSupported, nil)
		return ProxyTraffic{}, err
	}
	upstream, metadata, err := p.dialer.Dial(ctx, "tcp", host, port)
	if err != nil {
		_ = socks5.SendReply(client, statute.RepHostUnreachable, nil)
		return ProxyTraffic{}, fmt.Errorf("SOCKS5 destination dial failed: %w", err)
	}
	if upstream == nil {
		_ = socks5.SendReply(client, statute.RepServerFailure, nil)
		return ProxyTraffic{}, errors.New("SOCKS5 proxy dialer returned a nil connection")
	}
	defer upstream.Close()
	bindAddress := &net.TCPAddr{IP: net.IP(metadata.Source.AsSlice())}
	if err := socks5.SendReply(client, statute.RepSuccess, bindAddress); err != nil {
		return ProxyTraffic{Protocol: "socks", Metadata: metadata}, errors.New("write SOCKS5 CONNECT response failed")
	}
	up, down, relayErr := relayConnections(ctx, client, request.Reader, upstream, p.tunnelIdleTimeout)
	return ProxyTraffic{Protocol: "socks", Metadata: metadata, UpBytes: up, DownBytes: down}, relayErr
}

func socksDestination(address *statute.AddrSpec) (string, uint16, error) {
	if address == nil || address.Port <= 0 || address.Port > 65535 {
		return "", 0, errors.New("invalid SOCKS5 destination")
	}
	host := strings.TrimSpace(address.FQDN)
	if host == "" && len(address.IP) != 0 {
		host = address.IP.String()
	}
	if host == "" {
		return "", 0, errors.New("invalid SOCKS5 destination")
	}
	if strings.Contains(host, "%") {
		return "", 0, errors.New("scoped SOCKS5 destination is not supported")
	}
	return host, uint16(address.Port), nil
}

func containsMethod(methods []byte, wanted byte) bool {
	for _, method := range methods {
		if method == wanted {
			return true
		}
	}
	return false
}

type fixedCredential struct {
	username string
	password string
}

func (c fixedCredential) Valid(username, password, _ string) bool {
	want := c.username + "\x00" + c.password
	got := username + "\x00" + password
	return len(want) == len(got) && subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}
