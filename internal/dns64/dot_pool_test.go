package dns64

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// fakeDoTConn wraps a net.Conn and records Close calls so tests can observe
// whether pooled connections are released correctly.
type fakeDoTConn struct {
	net.Conn
	closed atomic.Bool
}

func (c *fakeDoTConn) Close() error {
	c.closed.Store(true)
	return c.Conn.Close()
}

func readFramed(conn net.Conn) (*dns.Msg, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	payload := make([]byte, binary.BigEndian.Uint16(header))
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	msg := new(dns.Msg)
	if err := msg.Unpack(payload); err != nil {
		return nil, err
	}
	return msg, nil
}

func writeFramed(conn net.Conn, msg *dns.Msg) error {
	payload, err := msg.Pack()
	if err != nil {
		return err
	}
	frame := make([]byte, 2+len(payload))
	binary.BigEndian.PutUint16(frame, uint16(len(payload)))
	copy(frame[2:], payload)
	_, err = conn.Write(frame)
	return err
}

// servePipeDoT answers framed DNS messages on one end of a pipe until the
// responder returns an error, at which point the connection is dropped.
func servePipeDoT(serverEnd net.Conn, responder func(request *dns.Msg) (*dns.Msg, error)) {
	go func() {
		defer serverEnd.Close()
		for {
			request, err := readFramed(serverEnd)
			if err != nil {
				return
			}
			reply, err := responder(request)
			if err != nil {
				return
			}
			if err := writeFramed(serverEnd, reply); err != nil {
				return
			}
		}
	}()
}

type dotPoolHarness struct {
	queryer *DoTQueryer
	dialed  atomic.Int32
	mu      sync.Mutex
	conns   []*fakeDoTConn
}

func newDotPoolHarness(t *testing.T, responder func(request *dns.Msg) (*dns.Msg, error)) *dotPoolHarness {
	t.Helper()
	queryer, err := NewDoTQueryer(2*time.Second, netip.Addr{})
	if err != nil {
		t.Fatalf("NewDoTQueryer: %v", err)
	}
	harness := &dotPoolHarness{queryer: queryer}
	queryer.dial = func(context.Context, *dns.Client, string) (*dns.Conn, error) {
		serverEnd, clientEnd := net.Pipe()
		clientConn := &fakeDoTConn{Conn: clientEnd}
		harness.mu.Lock()
		harness.conns = append(harness.conns, clientConn)
		harness.mu.Unlock()
		harness.dialed.Add(1)
		servePipeDoT(serverEnd, responder)
		return &dns.Conn{Conn: clientConn}, nil
	}
	t.Cleanup(func() { _ = queryer.Close() })
	return harness
}

func echoResponder() func(request *dns.Msg) (*dns.Msg, error) {
	return func(request *dns.Msg) (*dns.Msg, error) {
		reply := new(dns.Msg)
		reply.SetReply(request)
		return reply, nil
	}
}

func dotPoolEndpoint() Endpoint {
	return Endpoint{Address: netip.MustParseAddr("2001:db8::53"), Port: 853, ServerName: "dns.example"}
}

func newDNSRequest(id uint16) *dns.Msg {
	request := new(dns.Msg)
	request.SetQuestion(dns.Fqdn("example.com."), dns.TypeAAAA)
	request.Id = id
	return request
}

func TestExchangeDoTReusesIdleConnection(t *testing.T) {
	harness := newDotPoolHarness(t, echoResponder())
	endpoint := dotPoolEndpoint()
	ctx := context.Background()

	first, err := harness.queryer.exchangeDoT(ctx, endpoint, newDNSRequest(1))
	if err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	if first.Id != 1 {
		t.Fatalf("first response id = %d, want 1", first.Id)
	}
	second, err := harness.queryer.exchangeDoT(ctx, endpoint, newDNSRequest(2))
	if err != nil {
		t.Fatalf("second exchange: %v", err)
	}
	if second.Id != 2 {
		t.Fatalf("second response id = %d, want 2", second.Id)
	}
	if got := harness.dialed.Load(); got != 1 {
		t.Fatalf("dialed %d connections, want 1 (idle connection must be reused)", got)
	}
}

func TestExchangeDoTRetriesWithFreshConnectionAfterReuseFailure(t *testing.T) {
	var requests atomic.Int32
	responder := func(request *dns.Msg) (*dns.Msg, error) {
		if requests.Add(1) == 1 {
			reply := new(dns.Msg)
			reply.SetReply(request)
			return reply, nil
		}
		if requests.Load() == 2 {
			// Drop the connection after the second request so the pooled
			// read fails; the exchange must retry on a fresh connection.
			return nil, errors.New("drop")
		}
		reply := new(dns.Msg)
		reply.SetReply(request)
		return reply, nil
	}
	harness := newDotPoolHarness(t, responder)
	endpoint := dotPoolEndpoint()
	ctx := context.Background()

	if _, err := harness.queryer.exchangeDoT(ctx, endpoint, newDNSRequest(1)); err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	second, err := harness.queryer.exchangeDoT(ctx, endpoint, newDNSRequest(2))
	if err != nil {
		t.Fatalf("second exchange must succeed via fresh connection: %v", err)
	}
	if second.Id != 2 {
		t.Fatalf("second response id = %d, want 2", second.Id)
	}
	if got := harness.dialed.Load(); got != 2 {
		t.Fatalf("dialed %d connections, want 2 (dead pooled conn must be replaced)", got)
	}
}

func TestExchangeDoTRejectsMismatchedResponseId(t *testing.T) {
	responder := func(request *dns.Msg) (*dns.Msg, error) {
		reply := new(dns.Msg)
		reply.SetReply(request)
		reply.Id = 4242
		return reply, nil
	}
	harness := newDotPoolHarness(t, responder)
	endpoint := dotPoolEndpoint()
	ctx := context.Background()

	if _, err := harness.queryer.exchangeDoT(ctx, endpoint, newDNSRequest(1)); err == nil {
		t.Fatal("mismatched response id must be an error")
	}
	harness.mu.Lock()
	firstConn := harness.conns[0]
	harness.mu.Unlock()
	if !firstConn.closed.Load() {
		t.Fatal("connection with mismatched id must be closed, not pooled")
	}
}

func TestDoTQueryerCloseClosesIdleConnections(t *testing.T) {
	harness := newDotPoolHarness(t, echoResponder())
	endpoint := dotPoolEndpoint()
	ctx := context.Background()

	if _, err := harness.queryer.exchangeDoT(ctx, endpoint, newDNSRequest(1)); err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	if err := harness.queryer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	harness.mu.Lock()
	firstConn := harness.conns[0]
	harness.mu.Unlock()
	if !firstConn.closed.Load() {
		t.Fatal("Close must close pooled idle connections")
	}
	if _, err := harness.queryer.exchangeDoT(ctx, endpoint, newDNSRequest(2)); err != nil {
		t.Fatalf("query after Close must still work with a fresh connection: %v", err)
	}
	if got := harness.dialed.Load(); got != 2 {
		t.Fatalf("dialed %d connections, want 2 (no reuse after Close)", got)
	}
}

func TestPutIdleEnforcesPerEndpointCap(t *testing.T) {
	queryer, err := NewDoTQueryer(time.Second, netip.Addr{})
	if err != nil {
		t.Fatalf("NewDoTQueryer: %v", err)
	}
	t.Cleanup(func() { _ = queryer.Close() })

	newConn := func() *fakeDoTConn {
		serverEnd, clientEnd := net.Pipe()
		conn := &fakeDoTConn{Conn: clientEnd}
		go func() {
			defer serverEnd.Close()
			buffer := make([]byte, 512)
			for {
				if _, err := serverEnd.Read(buffer); err != nil {
					return
				}
			}
		}()
		return conn
	}
	address := "2001:db8::53:853"
	first := newConn()
	second := newConn()
	third := newConn()
	queryer.putIdle(address, &dns.Conn{Conn: first})
	queryer.putIdle(address, &dns.Conn{Conn: second})
	queryer.putIdle(address, &dns.Conn{Conn: third})

	queryer.mu.Lock()
	pooled := len(queryer.idle[address])
	queryer.mu.Unlock()
	if pooled != maxIdleDoTConnsPerEndpoint {
		t.Fatalf("pooled %d connections, want cap %d", pooled, maxIdleDoTConnsPerEndpoint)
	}
	if !third.closed.Load() {
		t.Fatal("connection beyond the idle cap must be closed immediately")
	}
}

func TestExchangeDoTConcurrentQueriesAllSucceed(t *testing.T) {
	harness := newDotPoolHarness(t, echoResponder())
	endpoint := dotPoolEndpoint()
	ctx := context.Background()

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id uint16) {
			defer wg.Done()
			response, err := harness.queryer.exchangeDoT(ctx, endpoint, newDNSRequest(id))
			if err != nil {
				errs <- err
				return
			}
			if response.Id != id {
				errs <- errors.New("response id mismatch")
			}
		}(uint16(i + 1))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent exchange: %v", err)
	}
}
