package proxy

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	socks5 "github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/statute"
)

func TestSOCKS5ProxyAuthenticatedConnectRelaysTraffic(t *testing.T) {
	upstream, origin := net.Pipe()
	dialer := &fakeProxyDialer{
		conn: upstream,
		metadata: DialMetadata{
			Source:      netip.MustParseAddr("2001:4860:1::10"),
			Destination: netip.MustParseAddrPort("[2606:4700:4700::1111]:443"),
		},
	}
	proxy, err := NewSOCKS5Proxy(SOCKS5Options{
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

	if _, err := client.Write(statute.NewMethodRequest(statute.VersionSocks5, []byte{statute.MethodUserPassAuth}).Bytes()); err != nil {
		t.Fatal(err)
	}
	method, err := statute.ParseMethodReply(client)
	if err != nil {
		t.Fatal(err)
	}
	if method.Method != statute.MethodUserPassAuth {
		t.Fatalf("method = %d", method.Method)
	}
	if _, err := client.Write(statute.NewUserPassRequest(statute.UserPassAuthVersion, []byte("alice"), []byte("correct-horse-battery")).Bytes()); err != nil {
		t.Fatal(err)
	}
	authReply, err := statute.ParseUserPassReply(client)
	if err != nil {
		t.Fatal(err)
	}
	if authReply.Status != statute.AuthSuccess {
		t.Fatalf("auth status = %d", authReply.Status)
	}
	request := statute.Request{
		Version: statute.VersionSocks5, Command: statute.CommandConnect,
		DstAddr: statute.AddrSpec{AddrType: statute.ATYPDomain, FQDN: "example.com", Port: 443},
	}
	if _, err := client.Write(request.Bytes()); err != nil {
		t.Fatal(err)
	}
	reply, err := statute.ParseReply(client)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Response != statute.RepSuccess {
		t.Fatalf("reply = %#v", reply)
	}

	originDone := make(chan error, 1)
	go func() {
		payload := make([]byte, 4)
		if _, err := io.ReadFull(origin, payload); err != nil {
			originDone <- err
			return
		}
		if string(payload) != "ping" {
			originDone <- errors.New("unexpected SOCKS tunnel payload")
			return
		}
		_, err := origin.Write([]byte("pong"))
		originDone <- err
		_ = origin.Close()
	}()
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 4)
	if _, err := io.ReadFull(client, payload); err != nil {
		t.Fatal(err)
	}
	if string(payload) != "pong" {
		t.Fatalf("response = %q", payload)
	}
	_ = client.Close()
	if err := <-originDone; err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.traffic.Protocol != "socks" || result.traffic.UpBytes != 4 || result.traffic.DownBytes != 4 || result.traffic.Metadata != dialer.metadata {
		t.Fatalf("traffic = %#v", result.traffic)
	}
	if calls := dialer.Calls(); len(calls) != 1 || calls[0] != (proxyDialCall{network: "tcp", host: "example.com", port: 443}) {
		t.Fatalf("dial calls = %#v", calls)
	}
}

func TestSOCKS5ProxyRejectsInvalidCredentials(t *testing.T) {
	proxy, err := NewSOCKS5Proxy(SOCKS5Options{
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
	_, _ = client.Write(statute.NewMethodRequest(statute.VersionSocks5, []byte{statute.MethodUserPassAuth}).Bytes())
	if _, err := statute.ParseMethodReply(client); err != nil {
		t.Fatal(err)
	}
	_, _ = client.Write(statute.NewUserPassRequest(statute.UserPassAuthVersion, []byte("alice"), []byte("wrong-password")).Bytes())
	reply, err := statute.ParseUserPassReply(client)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Status != statute.AuthFailure {
		t.Fatalf("auth status = %d, want failure", reply.Status)
	}
	if serveErr := <-done; !errors.Is(serveErr, statute.ErrUserAuthFailed) {
		t.Fatalf("ServeConn() error = %v", serveErr)
	}
}

func TestSOCKS5ProxyExplicitlyRejectsBind(t *testing.T) {
	proxy, err := NewSOCKS5Proxy(SOCKS5Options{Dialer: &fakeProxyDialer{}, HandshakeTimeout: time.Second})
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
	_, _ = client.Write(statute.NewMethodRequest(statute.VersionSocks5, []byte{statute.MethodNoAuth}).Bytes())
	if _, err := statute.ParseMethodReply(client); err != nil {
		t.Fatal(err)
	}
	request := statute.Request{
		Version: statute.VersionSocks5, Command: statute.CommandBind,
		DstAddr: statute.AddrSpec{AddrType: statute.ATYPIPv4, IP: net.IPv4zero, Port: 0},
	}
	_, _ = client.Write(request.Bytes())
	reply, err := statute.ParseReply(bufio.NewReader(client))
	if err != nil {
		t.Fatal(err)
	}
	if reply.Response != statute.RepCommandNotSupported {
		t.Fatalf("reply response = %d", reply.Response)
	}
	if serveErr := <-done; !errors.Is(serveErr, ErrSOCKSCommandUnsupported) {
		t.Fatalf("ServeConn() error = %v", serveErr)
	}
}

func TestSOCKS5ProxyValidatesOptions(t *testing.T) {
	valid := SOCKS5Options{Dialer: &fakeProxyDialer{}, HandshakeTimeout: time.Second}
	invalid := []SOCKS5Options{
		{},
		{Dialer: &fakeProxyDialer{}},
		{Dialer: &fakeProxyDialer{}, Username: "only-user", HandshakeTimeout: time.Second},
		{Dialer: &fakeProxyDialer{}, Password: "only-password", HandshakeTimeout: time.Second},
	}
	for _, options := range invalid {
		if _, err := NewSOCKS5Proxy(options); err == nil {
			t.Fatalf("NewSOCKS5Proxy(%#v) error = nil", options)
		}
	}
	if _, err := NewSOCKS5Proxy(valid); err != nil {
		t.Fatal(err)
	}
	_ = socks5.NoAuthAuthenticator{}
}
