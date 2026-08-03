package dns64

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/miekg/dns"
)

type dnsExchange func(context.Context, Endpoint, *dns.Msg) (*dns.Msg, error)

type DoTQueryer struct {
	timeout  time.Duration
	source   netip.Addr
	exchange dnsExchange
}

func NewDoTQueryer(timeout time.Duration, source netip.Addr) (*DoTQueryer, error) {
	if timeout <= 0 {
		return nil, errors.New("DoT timeout must be positive")
	}
	if source.IsValid() && (!source.Is6() || source.Is4In6()) {
		return nil, errors.New("DoT source must be an IPv6 address")
	}
	queryer := &DoTQueryer{timeout: timeout, source: source}
	queryer.exchange = queryer.exchangeDoT
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
	response, _, err := client.ExchangeContext(ctx, request, address)
	return response, err
}
