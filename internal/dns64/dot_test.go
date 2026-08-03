package dns64

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestDoTQueryerBuildsQuestionAndParsesMatchingRecords(t *testing.T) {
	var capturedName string
	var capturedType uint16
	queryer := &DoTQueryer{exchange: func(_ context.Context, _ Endpoint, request *dns.Msg) (*dns.Msg, error) {
		capturedName = request.Question[0].Name
		capturedType = request.Question[0].Qtype
		response := new(dns.Msg)
		response.SetReply(request)
		response.Answer = []dns.RR{
			&dns.A{Hdr: dns.RR_Header{Name: capturedName, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120}, A: []byte{8, 8, 8, 8}},
			&dns.A{Hdr: dns.RR_Header{Name: capturedName, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 45}, A: []byte{1, 1, 1, 1}},
			&dns.AAAA{Hdr: dns.RR_Header{Name: capturedName, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 1}, AAAA: netip.MustParseAddr("2606:4700:4700::1111").AsSlice()},
		}
		return response, nil
	}}

	result, err := queryer.Query(context.Background(), testEndpoints()[0], "example.com", TypeA)
	if err != nil {
		t.Fatal(err)
	}
	if capturedName != "example.com." || capturedType != dns.TypeA {
		t.Fatalf("question = %s type %d", capturedName, capturedType)
	}
	if len(result.Addresses) != 2 || result.TTL != 45*time.Second {
		t.Fatalf("result = %#v", result)
	}
}

func TestDoTQueryerRejectsDNSFailureResponse(t *testing.T) {
	queryer := &DoTQueryer{exchange: func(_ context.Context, _ Endpoint, request *dns.Msg) (*dns.Msg, error) {
		response := new(dns.Msg)
		response.SetRcode(request, dns.RcodeNameError)
		return response, nil
	}}
	if _, err := queryer.Query(context.Background(), testEndpoints()[0], "missing.example", TypeAAAA); err == nil {
		t.Fatal("Query() error = nil for NXDOMAIN")
	}
}

func TestDoTQueryerPropagatesTransportFailure(t *testing.T) {
	want := errors.New("TLS failed")
	queryer := &DoTQueryer{exchange: func(context.Context, Endpoint, *dns.Msg) (*dns.Msg, error) {
		return nil, want
	}}
	if _, err := queryer.Query(context.Background(), testEndpoints()[0], "example.com", TypeAAAA); !errors.Is(err, want) {
		t.Fatalf("Query() error = %v, want wrapped transport error", err)
	}
}

func TestNewDoTQueryerPinsIPv6NetworkTLSNameAndOptionalSource(t *testing.T) {
	source := netip.MustParseAddr("2001:db8::10")
	queryer, err := NewDoTQueryer(10*time.Second, source)
	if err != nil {
		t.Fatal(err)
	}
	client := queryer.clientFor(testEndpoints()[0])
	if client.Net != "tcp6-tls" {
		t.Fatalf("network = %q, want tcp6-tls", client.Net)
	}
	if client.TLSConfig == nil || client.TLSConfig.ServerName != "cloudflare-dns.com" {
		t.Fatalf("TLS config = %#v", client.TLSConfig)
	}
	if client.Dialer == nil || client.Dialer.LocalAddr == nil || client.Dialer.LocalAddr.String() != "[2001:db8::10]:0" {
		t.Fatalf("local address = %#v", client.Dialer)
	}
}

func TestNewDoTQueryerRejectsIPv4SourceAndInvalidTimeout(t *testing.T) {
	if _, err := NewDoTQueryer(time.Second, netip.MustParseAddr("192.0.2.1")); err == nil {
		t.Fatal("IPv4 source accepted")
	}
	if _, err := NewDoTQueryer(0, netip.Addr{}); err == nil {
		t.Fatal("zero timeout accepted")
	}
}
