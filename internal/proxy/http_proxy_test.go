package proxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

type proxyDialCall struct {
	network string
	host    string
	port    uint16
}

type fakeProxyDialer struct {
	mu       sync.Mutex
	calls    []proxyDialCall
	conn     net.Conn
	metadata DialMetadata
	err      error
}

func (d *fakeProxyDialer) Dial(_ context.Context, network, host string, port uint16) (net.Conn, DialMetadata, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, proxyDialCall{network: network, host: host, port: port})
	return d.conn, d.metadata, d.err
}

func (d *fakeProxyDialer) Calls() []proxyDialCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]proxyDialCall(nil), d.calls...)
}

func TestHTTPProxyRequiresBasicAuthentication(t *testing.T) {
	proxy, err := NewHTTPProxy(HTTPProxyOptions{
		Dialer: &fakeProxyDialer{}, Username: "alice", Password: "correct-horse-battery",
		HandshakeTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	defer client.Close()
	done := make(chan error, 1)
	go func() {
		_, serveErr := proxy.ServeConn(context.Background(), server)
		done <- serveErr
	}()

	if _, err := io.WriteString(client, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(client), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("status = %d, want 407", response.StatusCode)
	}
	if serveErr := <-done; !errors.Is(serveErr, ErrProxyAuthenticationRequired) {
		t.Fatalf("ServeConn() error = %v", serveErr)
	}
}

func TestHTTPProxyConnectRelaysTrafficWithDialMetadata(t *testing.T) {
	upstream, origin := net.Pipe()
	dialer := &fakeProxyDialer{
		conn: upstream,
		metadata: DialMetadata{
			Source:      netip.MustParseAddr("2001:4860:1::10"),
			Destination: netip.MustParseAddrPort("[2606:4700:4700::1111]:443"),
		},
	}
	proxy, err := NewHTTPProxy(HTTPProxyOptions{
		Dialer: dialer, Username: "alice", Password: "correct-horse-battery",
		HandshakeTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	done := make(chan struct {
		traffic ProxyTraffic
		err     error
	}, 1)
	go func() {
		traffic, serveErr := proxy.ServeConn(context.Background(), server)
		done <- struct {
			traffic ProxyTraffic
			err     error
		}{traffic, serveErr}
	}()

	authorization := base64.StdEncoding.EncodeToString([]byte("alice:correct-horse-battery"))
	if _, err := io.WriteString(client, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\nProxy-Authorization: Basic "+authorization+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(client)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	originDone := make(chan error, 1)
	go func() {
		payload := make([]byte, 4)
		if _, err := io.ReadFull(origin, payload); err != nil {
			originDone <- err
			return
		}
		if string(payload) != "ping" {
			originDone <- errors.New("unexpected tunnel payload")
			return
		}
		_, err := io.WriteString(origin, "pong")
		originDone <- err
		_ = origin.Close()
	}()
	if _, err := io.WriteString(client, "ping"); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 4)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatal(err)
	}
	if string(payload) != "pong" {
		t.Fatalf("tunnel response = %q", payload)
	}
	_ = client.Close()
	if err := <-originDone; err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.traffic.Protocol != "http" || result.traffic.UpBytes != 4 || result.traffic.DownBytes != 4 || result.traffic.Metadata != dialer.metadata {
		t.Fatalf("traffic = %#v", result.traffic)
	}
	if calls := dialer.Calls(); len(calls) != 1 || calls[0] != (proxyDialCall{network: "tcp", host: "example.com", port: 443}) {
		t.Fatalf("dial calls = %#v", calls)
	}
}

func TestHTTPProxyForwardsAbsoluteFormWithoutProxySecrets(t *testing.T) {
	upstream, origin := net.Pipe()
	dialer := &fakeProxyDialer{conn: upstream}
	proxy, err := NewHTTPProxy(HTTPProxyOptions{Dialer: dialer, HandshakeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	done := make(chan error, 1)
	go func() {
		_, serveErr := proxy.ServeConn(context.Background(), server)
		done <- serveErr
	}()

	originDone := make(chan error, 1)
	go func() {
		request, readErr := http.ReadRequest(bufio.NewReader(origin))
		if readErr != nil {
			originDone <- readErr
			return
		}
		if request.RequestURI != "/private/path?token=hidden" || request.Header.Get("Proxy-Authorization") != "" || request.Header.Get("Proxy-Connection") != "" {
			originDone <- errors.New("forwarded request was not sanitized")
			return
		}
		_, writeErr := io.WriteString(origin, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
		originDone <- writeErr
		_ = origin.Close()
	}()
	request := "GET http://example.com/private/path?token=hidden HTTP/1.1\r\n" +
		"Host: example.com\r\nProxy-Authorization: Basic should-not-forward\r\nProxy-Connection: keep-alive\r\n\r\n"
	if _, err := io.WriteString(client, request); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(client), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	_ = client.Close()
	if response.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("response = %d %q", response.StatusCode, body)
	}
	if err := <-originDone; err != nil {
		t.Fatal(err)
	}
	if serveErr := <-done; serveErr != nil && strings.Contains(serveErr.Error(), "/private/path") {
		t.Fatalf("ServeConn leaked URL path in error: %v", serveErr)
	} else if serveErr != nil {
		t.Fatal(serveErr)
	}
	if calls := dialer.Calls(); len(calls) != 1 || calls[0] != (proxyDialCall{network: "tcp", host: "example.com", port: 80}) {
		t.Fatalf("dial calls = %#v", calls)
	}
}

func TestHTTPProxyValidatesOptions(t *testing.T) {
	valid := HTTPProxyOptions{Dialer: &fakeProxyDialer{}, HandshakeTimeout: time.Second}
	invalid := []HTTPProxyOptions{
		{},
		{Dialer: &fakeProxyDialer{}},
		{Dialer: &fakeProxyDialer{}, Username: "only-user", HandshakeTimeout: time.Second},
		{Dialer: &fakeProxyDialer{}, Password: "only-password", HandshakeTimeout: time.Second},
	}
	for _, options := range invalid {
		if _, err := NewHTTPProxy(options); err == nil {
			t.Fatalf("NewHTTPProxy(%#v) error = nil", options)
		}
	}
	if _, err := NewHTTPProxy(valid); err != nil {
		t.Fatal(err)
	}
}
