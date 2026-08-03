package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

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
