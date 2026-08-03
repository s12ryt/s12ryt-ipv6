package proxy

import (
	"bufio"
	"context"
	"errors"
	"net"
	"time"
)

var ErrMixedProtocol = errors.New("mixed proxy protocol is not recognized")

type ConnectionProxy interface {
	ServeConn(context.Context, net.Conn) (ProxyTraffic, error)
}

type MixedProxyOptions struct {
	SOCKS5           ConnectionProxy
	HTTP             ConnectionProxy
	HandshakeTimeout time.Duration
}

type MixedProxy struct {
	socks5           ConnectionProxy
	http             ConnectionProxy
	handshakeTimeout time.Duration
}

func NewMixedProxy(options MixedProxyOptions) (*MixedProxy, error) {
	if options.SOCKS5 == nil {
		return nil, errors.New("mixed proxy SOCKS5 handler is required")
	}
	if options.HTTP == nil {
		return nil, errors.New("mixed proxy HTTP handler is required")
	}
	if options.HandshakeTimeout <= 0 {
		return nil, errors.New("mixed proxy handshake timeout must be positive")
	}
	return &MixedProxy{
		socks5: options.SOCKS5, http: options.HTTP, handshakeTimeout: options.HandshakeTimeout,
	}, nil
}

func (p *MixedProxy) ServeConn(ctx context.Context, client net.Conn) (ProxyTraffic, error) {
	if client == nil {
		return ProxyTraffic{}, errors.New("mixed proxy client connection is required")
	}
	defer client.Close()
	if err := client.SetReadDeadline(time.Now().Add(p.handshakeTimeout)); err != nil {
		return ProxyTraffic{}, errors.New("set mixed proxy handshake deadline failed")
	}
	reader := bufio.NewReader(client)
	first, err := reader.Peek(1)
	if err != nil {
		return ProxyTraffic{}, errors.New("read mixed proxy protocol failed")
	}
	if err := client.SetReadDeadline(time.Time{}); err != nil {
		return ProxyTraffic{}, errors.New("clear mixed proxy handshake deadline failed")
	}
	buffered := &bufferedConn{Conn: client, reader: reader}
	if first[0] == 0x05 {
		traffic, serveErr := p.socks5.ServeConn(ctx, buffered)
		traffic.Protocol = "socks"
		return traffic, serveErr
	}
	if first[0] >= 'A' && first[0] <= 'Z' {
		traffic, serveErr := p.http.ServeConn(ctx, buffered)
		traffic.Protocol = "http"
		return traffic, serveErr
	}
	return ProxyTraffic{}, ErrMixedProtocol
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(payload []byte) (int, error) {
	return c.reader.Read(payload)
}
