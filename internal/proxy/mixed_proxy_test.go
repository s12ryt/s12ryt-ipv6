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

type recordingConnectionProxy struct {
	mu        sync.Mutex
	readBytes int
	calls     int
	payload   []byte
	traffic   ProxyTraffic
	err       error
}

func (p *recordingConnectionProxy) ServeConn(_ context.Context, conn net.Conn) (ProxyTraffic, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	payload := make([]byte, p.readBytes)
	_, readErr := io.ReadFull(conn, payload)
	p.mu.Lock()
	p.payload = append([]byte(nil), payload...)
	p.mu.Unlock()
	if readErr != nil {
		return ProxyTraffic{}, readErr
	}
	return p.traffic, p.err
}

func (p *recordingConnectionProxy) snapshot() (int, []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls, append([]byte(nil), p.payload...)
}

func TestMixedProxyRoutesSOCKS5WithoutConsumingVersionByte(t *testing.T) {
	payload := []byte{0x05, 0x01, 0x00}
	socks := &recordingConnectionProxy{readBytes: len(payload), traffic: ProxyTraffic{UpBytes: 3}}
	http := &recordingConnectionProxy{readBytes: len(payload)}
	proxy, err := NewMixedProxy(MixedProxyOptions{SOCKS5: socks, HTTP: http, HandshakeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}

	traffic, serveErr := serveMixedPayload(t, proxy, payload)
	if serveErr != nil {
		t.Fatal(serveErr)
	}
	if traffic.Protocol != "socks" || traffic.UpBytes != 3 {
		t.Fatalf("traffic = %#v", traffic)
	}
	if calls, got := socks.snapshot(); calls != 1 || string(got) != string(payload) {
		t.Fatalf("SOCKS calls/payload = %d/%v", calls, got)
	}
	if calls, _ := http.snapshot(); calls != 0 {
		t.Fatalf("HTTP calls = %d", calls)
	}
}

func TestMixedProxyRoutesHTTPWithoutConsumingMethodByte(t *testing.T) {
	payload := []byte("CONNECT example.com:443 HTTP/1.1\r\n\r\n")
	socks := &recordingConnectionProxy{readBytes: len(payload)}
	http := &recordingConnectionProxy{readBytes: len(payload), traffic: ProxyTraffic{DownBytes: 7}}
	proxy, err := NewMixedProxy(MixedProxyOptions{SOCKS5: socks, HTTP: http, HandshakeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}

	traffic, serveErr := serveMixedPayload(t, proxy, payload)
	if serveErr != nil {
		t.Fatal(serveErr)
	}
	if traffic.Protocol != "http" || traffic.DownBytes != 7 {
		t.Fatalf("traffic = %#v", traffic)
	}
	if calls, got := http.snapshot(); calls != 1 || string(got) != string(payload) {
		t.Fatalf("HTTP calls/payload = %d/%q", calls, got)
	}
	if calls, _ := socks.snapshot(); calls != 0 {
		t.Fatalf("SOCKS calls = %d", calls)
	}
}

func TestMixedProxyRejectsUnknownProtocol(t *testing.T) {
	socks := &recordingConnectionProxy{readBytes: 1}
	http := &recordingConnectionProxy{readBytes: 1}
	proxy, err := NewMixedProxy(MixedProxyOptions{SOCKS5: socks, HTTP: http, HandshakeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}

	_, serveErr := serveMixedPayload(t, proxy, []byte{0x01})
	if !errors.Is(serveErr, ErrMixedProtocol) {
		t.Fatalf("ServeConn() error = %v, want ErrMixedProtocol", serveErr)
	}
	if calls, _ := socks.snapshot(); calls != 0 {
		t.Fatalf("SOCKS calls = %d", calls)
	}
	if calls, _ := http.snapshot(); calls != 0 {
		t.Fatalf("HTTP calls = %d", calls)
	}
}

func TestNewMixedProxyValidatesOptions(t *testing.T) {
	handler := &recordingConnectionProxy{}
	tests := []MixedProxyOptions{
		{HTTP: handler, HandshakeTimeout: time.Second},
		{SOCKS5: handler, HandshakeTimeout: time.Second},
		{SOCKS5: handler, HTTP: handler},
	}
	for _, options := range tests {
		if _, err := NewMixedProxy(options); err == nil {
			t.Fatalf("NewMixedProxy(%#v) error = nil", options)
		}
	}
}

func serveMixedPayload(t *testing.T, proxy *MixedProxy, payload []byte) (ProxyTraffic, error) {
	t.Helper()
	client, server := net.Pipe()
	result := make(chan struct {
		traffic ProxyTraffic
		err     error
	}, 1)
	go func() {
		traffic, err := proxy.ServeConn(context.Background(), server)
		result <- struct {
			traffic ProxyTraffic
			err     error
		}{traffic: traffic, err: err}
	}()
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-result:
		_ = client.Close()
		return got.traffic, got.err
	case <-time.After(time.Second):
		_ = client.Close()
		t.Fatal("timed out waiting for mixed proxy result")
		return ProxyTraffic{}, nil
	}
}
