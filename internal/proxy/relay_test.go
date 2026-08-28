package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

type deadlineRecordingConn struct {
	net.Conn
	mu            sync.Mutex
	deadlineCalls int
}

func (c *deadlineRecordingConn) SetDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.deadlineCalls++
	c.mu.Unlock()
	return c.Conn.SetDeadline(deadline)
}

func (c *deadlineRecordingConn) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deadlineCalls
}

func TestRelayConnectionsClosesIdleTunnel(t *testing.T) {
	client, proxyClient := net.Pipe()
	proxyUpstream, upstream := net.Pipe()
	defer client.Close()
	defer upstream.Close()
	result := make(chan error, 1)
	go func() {
		_, _, err := relayConnections(context.Background(), proxyClient, proxyClient, proxyUpstream, 40*time.Millisecond)
		result <- err
	}()
	select {
	case err := <-result:
		if !errors.Is(err, ErrTunnelIdleTimeout) {
			t.Fatalf("relay error = %v, want ErrTunnelIdleTimeout", err)
		}
	case <-time.After(time.Second):
		t.Fatal("idle tunnel was not closed")
	}
}

func TestRelayConnectionsTrafficInEitherDirectionRefreshesIdleTimeout(t *testing.T) {
	client, proxyClient := net.Pipe()
	proxyUpstream, upstream := net.Pipe()
	defer client.Close()
	defer upstream.Close()
	result := make(chan error, 1)
	go func() {
		_, _, err := relayConnections(context.Background(), proxyClient, proxyClient, proxyUpstream, 80*time.Millisecond)
		result <- err
	}()

	time.Sleep(50 * time.Millisecond)
	go func() { _, _ = client.Write([]byte("up")) }()
	if _, err := io.ReadFull(upstream, make([]byte, 2)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	go func() { _, _ = upstream.Write([]byte("down")) }()
	if _, err := io.ReadFull(client, make([]byte, 4)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		t.Fatalf("active tunnel closed early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrTunnelIdleTimeout) {
			t.Fatalf("relay error = %v, want ErrTunnelIdleTimeout", err)
		}
	case <-time.After(time.Second):
		t.Fatal("tunnel did not close after activity stopped")
	}
}

func TestRelayConnectionsZeroIdleTimeoutDoesNotSetTunnelDeadlines(t *testing.T) {
	client, rawProxyClient := net.Pipe()
	rawProxyUpstream, upstream := net.Pipe()
	proxyClient := &deadlineRecordingConn{Conn: rawProxyClient}
	proxyUpstream := &deadlineRecordingConn{Conn: rawProxyUpstream}
	defer client.Close()
	defer upstream.Close()

	result := make(chan error, 1)
	go func() {
		_, _, err := relayConnections(context.Background(), proxyClient, proxyClient, proxyUpstream, 0)
		result <- err
	}()

	go func() { _, _ = client.Write([]byte("up")) }()
	if _, err := io.ReadFull(upstream, make([]byte, 2)); err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = upstream.Write([]byte("down")) }()
	if _, err := io.ReadFull(client, make([]byte, 4)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		t.Fatalf("active tunnel closed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	_ = client.Close()
	_ = upstream.Close()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("relay error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("relay did not stop after both peers closed")
	}
	if calls := proxyClient.calls(); calls != 0 {
		t.Fatalf("client SetDeadline calls = %d, want 0", calls)
	}
	if calls := proxyUpstream.calls(); calls != 0 {
		t.Fatalf("upstream SetDeadline calls = %d, want 0", calls)
	}
}

func TestRelayConnectionsHalfClosePreservesReverseTraffic(t *testing.T) {
	client, proxyClient := newTCPConnPair(t)
	proxyUpstream, upstream := newTCPConnPair(t)
	defer client.Close()
	defer upstream.Close()

	result := make(chan error, 1)
	go func() {
		_, _, err := relayConnections(context.Background(), proxyClient, proxyClient, proxyUpstream, 0)
		result <- err
	}()

	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := client.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	request := make([]byte, len("request"))
	if _, err := io.ReadFull(upstream, request); err != nil {
		t.Fatal(err)
	}
	if string(request) != "request" {
		t.Fatalf("upstream payload = %q, want request", request)
	}
	if _, err := upstream.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("upstream read after client half-close = %v, want EOF", err)
	}

	if _, err := upstream.Write([]byte("response")); err != nil {
		t.Fatal(err)
	}
	if err := upstream.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len("response"))
	if _, err := io.ReadFull(client, response); err != nil {
		t.Fatal(err)
	}
	if string(response) != "response" {
		t.Fatalf("client payload = %q, want response", response)
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("relay error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("relay did not stop after both directions half-closed")
	}
}

func newTCPConnPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan *net.TCPConn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.AcceptTCP()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()
	client, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case server := <-accepted:
		return client, server
	case err := <-acceptErr:
		_ = client.Close()
		t.Fatal(err)
	case <-time.After(time.Second):
		_ = client.Close()
		t.Fatal("accept timed out")
	}
	return nil, nil
}

func TestProxyConstructorsRejectNegativeTunnelIdleTimeout(t *testing.T) {
	dialer := &fakeProxyDialer{}
	if _, err := NewHTTPProxy(HTTPProxyOptions{
		Dialer: dialer, HandshakeTimeout: time.Second, TunnelIdleTimeout: -time.Second,
	}); err == nil {
		t.Fatal("NewHTTPProxy(negative idle) error = nil")
	}
	if _, err := NewSOCKS5Proxy(SOCKS5Options{
		Dialer: dialer, HandshakeTimeout: time.Second, TunnelIdleTimeout: -time.Second,
	}); err == nil {
		t.Fatal("NewSOCKS5Proxy(negative idle) error = nil")
	}
}
