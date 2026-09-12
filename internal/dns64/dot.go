package dns64

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/miekg/dns"
)

type dnsExchange func(context.Context, Endpoint, *dns.Msg) (*dns.Msg, error)

const maxIdleDoTConnsPerEndpoint = 2

type DoTQueryer struct {
	timeout  time.Duration
	source   netip.Addr
	exchange dnsExchange
	dial     func(context.Context, *dns.Client, string) (*dns.Conn, error)

	mu     sync.Mutex
	idle   map[string][]*dns.Conn
	closed bool
}

func NewDoTQueryer(timeout time.Duration, source netip.Addr) (*DoTQueryer, error) {
	if timeout <= 0 {
		return nil, errors.New("DoT timeout must be positive")
	}
	if source.IsValid() && (!source.Is6() || source.Is4In6()) {
		return nil, errors.New("DoT source must be an IPv6 address")
	}
	queryer := &DoTQueryer{timeout: timeout, source: source, idle: make(map[string][]*dns.Conn)}
	queryer.exchange = queryer.exchangeDoT
	queryer.dial = queryer.dialDoTConn
	return queryer, nil
}

func (q *DoTQueryer) Query(ctx context.Context, endpoint Endpoint, name string, record RecordType) (QueryResult, error) {
	if err := validateEndpoint(endpoint); err != nil {
		return QueryResult{}, err
	}
	name = normalizeName(name)
	if _, ok := dns.IsDomainName(name); !ok {
		return QueryResult{}, fmt.Errorf("invalid DNS name %q", name)
	}
	var queryType uint16
	switch record {
	case TypeA:
		queryType = dns.TypeA
	case TypeAAAA:
		queryType = dns.TypeAAAA
	default:
		return QueryResult{}, fmt.Errorf("unsupported DNS record type %d", record)
	}
	request := new(dns.Msg)
	request.SetQuestion(name, queryType)
	request.RecursionDesired = true
	response, err := q.exchange(ctx, endpoint, request)
	if err != nil {
		return QueryResult{}, fmt.Errorf("DoT exchange with %s: %w", endpoint.Name, err)
	}
	if response == nil {
		return QueryResult{}, errors.New("DoT response is nil")
	}
	if response.Rcode != dns.RcodeSuccess {
		return QueryResult{}, fmt.Errorf("DoT response code %s", dns.RcodeToString[response.Rcode])
	}

	result := QueryResult{}
	for _, answer := range response.Answer {
		var address netip.Addr
		var ttl uint32
		switch record := answer.(type) {
		case *dns.A:
			if queryType != dns.TypeA {
				continue
			}
			parsed, ok := netip.AddrFromSlice(record.A)
			if !ok || !parsed.Is4() {
				continue
			}
			address, ttl = parsed.Unmap(), record.Hdr.Ttl
		case *dns.AAAA:
			if queryType != dns.TypeAAAA {
				continue
			}
			parsed, ok := netip.AddrFromSlice(record.AAAA)
			if !ok || !parsed.Is6() || parsed.Is4In6() {
				continue
			}
			address, ttl = parsed, record.Hdr.Ttl
		default:
			continue
		}
		result.Addresses = append(result.Addresses, address)
		duration := time.Duration(ttl) * time.Second
		if result.TTL == 0 || duration < result.TTL {
			result.TTL = duration
		}
	}
	return result, nil
}

func (q *DoTQueryer) clientFor(endpoint Endpoint) *dns.Client {
	dialer := &net.Dialer{Timeout: q.timeout}
	if q.source.IsValid() {
		dialer.LocalAddr = &net.TCPAddr{IP: net.IP(q.source.AsSlice())}
	}
	return &dns.Client{
		Net:     "tcp6-tls",
		Timeout: q.timeout,
		Dialer:  dialer,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: endpoint.ServerName,
		},
	}
}

func (q *DoTQueryer) exchangeDoT(ctx context.Context, endpoint Endpoint, request *dns.Msg) (*dns.Msg, error) {
	client := q.clientFor(endpoint)
	address := net.JoinHostPort(endpoint.Address.String(), strconv.Itoa(int(endpoint.Port)))
	if conn := q.takeIdle(address); conn != nil {
		response, err := q.roundTrip(ctx, conn, request)
		if err == nil && response != nil && response.Id == request.Id {
			q.putIdle(address, conn)
			return response, nil
		}
		conn.Close()
	}
	conn, err := q.dial(ctx, client, address)
	if err != nil {
		return nil, err
	}
	response, err := q.roundTrip(ctx, conn, request)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if response == nil || response.Id != request.Id {
		conn.Close()
		return nil, fmt.Errorf("DoT response id mismatch from %s", address)
	}
	q.putIdle(address, conn)
	return response, nil
}

func (q *DoTQueryer) roundTrip(ctx context.Context, conn *dns.Conn, request *dns.Msg) (*dns.Msg, error) {
	deadline := time.Now().Add(q.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return nil, err
	}
	if err := conn.WriteMsg(request); err != nil {
		return nil, err
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return nil, err
	}
	return conn.ReadMsg()
}

func (q *DoTQueryer) dialDoTConn(ctx context.Context, client *dns.Client, address string) (*dns.Conn, error) {
	raw, err := client.Dialer.DialContext(ctx, "tcp6", address)
	if err != nil {
		return nil, err
	}
	tlsConn := tls.Client(raw, client.TLSConfig)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	return &dns.Conn{Conn: tlsConn}, nil
}

func (q *DoTQueryer) takeIdle(address string) *dns.Conn {
	q.mu.Lock()
	defer q.mu.Unlock()
	conns := q.idle[address]
	if len(conns) == 0 {
		return nil
	}
	conn := conns[len(conns)-1]
	conns = conns[:len(conns)-1]
	if len(conns) == 0 {
		delete(q.idle, address)
	} else {
		q.idle[address] = conns
	}
	return conn
}

func (q *DoTQueryer) putIdle(address string, conn *dns.Conn) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || len(q.idle[address]) >= maxIdleDoTConnsPerEndpoint {
		conn.Close()
		return
	}
	q.idle[address] = append(q.idle[address], conn)
}

func (q *DoTQueryer) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return nil
	}
	q.closed = true
	for address, conns := range q.idle {
		for _, conn := range conns {
			conn.Close()
		}
		delete(q.idle, address)
	}
	return nil
}
