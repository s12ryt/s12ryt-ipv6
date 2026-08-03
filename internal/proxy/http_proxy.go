package proxy

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrProxyAuthenticationRequired = errors.New("proxy authentication required")
	ErrProxyBadRequest             = errors.New("invalid HTTP proxy request")
	ErrTunnelIdleTimeout           = errors.New("proxy tunnel idle timeout")
)

type ProxyDialer interface {
	Dial(context.Context, string, string, uint16) (net.Conn, DialMetadata, error)
}

type ProxyTraffic struct {
	Protocol  string
	Metadata  DialMetadata
	UpBytes   int64
	DownBytes int64
}

type HTTPProxyOptions struct {
	Dialer            ProxyDialer
	Username          string
	Password          string
	HandshakeTimeout  time.Duration
	TunnelIdleTimeout time.Duration
}

type HTTPProxy struct {
	dialer            ProxyDialer
	username          string
	password          string
	handshakeTimeout  time.Duration
	tunnelIdleTimeout time.Duration
}

func NewHTTPProxy(options HTTPProxyOptions) (*HTTPProxy, error) {
	if options.Dialer == nil {
		return nil, errors.New("HTTP proxy dialer is required")
	}
	if options.HandshakeTimeout <= 0 {
		return nil, errors.New("HTTP proxy handshake timeout must be positive")
	}
	if options.TunnelIdleTimeout < 0 {
		return nil, errors.New("HTTP proxy tunnel idle timeout must not be negative")
	}
	if (options.Username == "") != (options.Password == "") {
		return nil, errors.New("HTTP proxy username and password must both be set or both be empty")
	}
	return &HTTPProxy{
		dialer: options.Dialer, username: options.Username, password: options.Password,
		handshakeTimeout: options.HandshakeTimeout, tunnelIdleTimeout: options.TunnelIdleTimeout,
	}, nil
}

func (p *HTTPProxy) ServeConn(ctx context.Context, client net.Conn) (ProxyTraffic, error) {
	if client == nil {
		return ProxyTraffic{}, errors.New("HTTP proxy client connection is required")
	}
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(p.handshakeTimeout)); err != nil {
		return ProxyTraffic{}, errors.New("set HTTP proxy handshake deadline failed")
	}
	reader := bufio.NewReader(client)
	request, err := http.ReadRequest(reader)
	if err != nil {
		_ = writeHTTPProxyStatus(client, http.StatusBadRequest, false)
		return ProxyTraffic{}, ErrProxyBadRequest
	}
	defer request.Body.Close()
	if !p.authorized(request.Header.Get("Proxy-Authorization")) {
		_ = writeHTTPProxyStatus(client, http.StatusProxyAuthRequired, true)
		return ProxyTraffic{}, ErrProxyAuthenticationRequired
	}

	host, port, err := httpProxyTarget(request)
	if err != nil {
		_ = writeHTTPProxyStatus(client, http.StatusBadRequest, false)
		return ProxyTraffic{}, ErrProxyBadRequest
	}
	upstream, metadata, err := p.dialer.Dial(ctx, "tcp", host, port)
	if err != nil {
		_ = writeHTTPProxyStatus(client, http.StatusBadGateway, false)
		return ProxyTraffic{}, fmt.Errorf("HTTP proxy destination dial failed: %w", err)
	}
	if upstream == nil {
		_ = writeHTTPProxyStatus(client, http.StatusBadGateway, false)
		return ProxyTraffic{}, errors.New("HTTP proxy dialer returned a nil connection")
	}
	defer upstream.Close()
	if err := client.SetDeadline(time.Time{}); err != nil {
		return ProxyTraffic{}, errors.New("clear HTTP proxy handshake deadline failed")
	}

	if request.Method == http.MethodConnect {
		if err := writeHTTPProxyStatus(client, http.StatusOK, false); err != nil {
			return ProxyTraffic{Protocol: "http", Metadata: metadata}, errors.New("write HTTP CONNECT response failed")
		}
		up, down, relayErr := relayConnections(ctx, client, reader, upstream, p.tunnelIdleTimeout)
		return ProxyTraffic{Protocol: "http", Metadata: metadata, UpBytes: up, DownBytes: down}, relayErr
	}

	request.Header.Del("Proxy-Authorization")
	request.Header.Del("Proxy-Connection")
	request.Header.Set("Connection", "close")
	request.Close = true
	request.RequestURI = ""
	request.URL.Scheme = ""
	request.URL.Host = ""
	upCounter := &countingWriter{writer: upstream}
	if err := request.Write(upCounter); err != nil {
		return ProxyTraffic{Protocol: "http", Metadata: metadata, UpBytes: upCounter.count}, errors.New("forward HTTP proxy request failed")
	}
	downCounter := &countingWriter{writer: client}
	if _, err := io.Copy(downCounter, upstream); err != nil && !isExpectedClose(err) {
		return ProxyTraffic{Protocol: "http", Metadata: metadata, UpBytes: upCounter.count, DownBytes: downCounter.count}, errors.New("forward HTTP proxy response failed")
	}
	return ProxyTraffic{Protocol: "http", Metadata: metadata, UpBytes: upCounter.count, DownBytes: downCounter.count}, nil
}

func (p *HTTPProxy) authorized(header string) bool {
	if p.username == "" && p.password == "" {
		return true
	}
	scheme, encoded, found := strings.Cut(strings.TrimSpace(header), " ")
	if !found || !strings.EqualFold(scheme, "Basic") {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return false
	}
	want := []byte(p.username + ":" + p.password)
	return len(decoded) == len(want) && subtle.ConstantTimeCompare(decoded, want) == 1
}

func httpProxyTarget(request *http.Request) (string, uint16, error) {
	if request.Method == http.MethodConnect {
		return splitProxyHostPort(request.Host, 0)
	}
	if request.URL == nil || !request.URL.IsAbs() || !strings.EqualFold(request.URL.Scheme, "http") || request.URL.User != nil {
		return "", 0, ErrProxyBadRequest
	}
	return splitProxyHostPort(request.URL.Host, 80)
}

func splitProxyHostPort(authority string, defaultPort uint16) (string, uint16, error) {
	authority = strings.TrimSpace(authority)
	if authority == "" {
		return "", 0, ErrProxyBadRequest
	}
	host, portText, err := net.SplitHostPort(authority)
	if err != nil {
		if defaultPort == 0 || strings.Contains(err.Error(), "missing port") == false {
			return "", 0, ErrProxyBadRequest
		}
		host = strings.Trim(authority, "[]")
		portText = strconv.Itoa(int(defaultPort))
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 || strings.TrimSpace(host) == "" {
		return "", 0, ErrProxyBadRequest
	}
	return host, uint16(port), nil
}

func writeHTTPProxyStatus(writer io.Writer, status int, authenticate bool) error {
	text := http.StatusText(status)
	if text == "" {
		text = "Proxy Error"
	}
	var response strings.Builder
	fmt.Fprintf(&response, "HTTP/1.1 %d %s\r\n", status, text)
	if authenticate {
		response.WriteString("Proxy-Authenticate: Basic realm=\"s12ryt-ipv6\"\r\n")
	}
	response.WriteString("Content-Length: 0\r\nConnection: close\r\n\r\n")
	_, err := io.WriteString(writer, response.String())
	return err
}

type countingWriter struct {
	writer io.Writer
	count  int64
}

func (w *countingWriter) Write(payload []byte) (int, error) {
	written, err := w.writer.Write(payload)
	w.count += int64(written)
	return written, err
}

type copyResult struct {
	direction string
	bytes     int64
	err       error
}

func relayConnections(ctx context.Context, client net.Conn, clientReader io.Reader, upstream net.Conn, idleTimeout time.Duration) (int64, int64, error) {
	var refresher *tunnelDeadlineRefresher
	if idleTimeout > 0 {
		refresher = &tunnelDeadlineRefresher{client: client, upstream: upstream, idleTimeout: idleTimeout}
		if err := refresher.touch(); err != nil {
			return 0, 0, errors.New("set proxy tunnel idle deadline failed")
		}
	}
	results := make(chan copyResult, 2)
	go copyHalf(results, "up", upstream, clientReader, refresher)
	go copyHalf(results, "down", client, upstream, refresher)
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Close()
			_ = upstream.Close()
		case <-stopWatch:
		}
	}()

	var up, down int64
	var failures []error
	for range 2 {
		result := <-results
		if result.direction == "up" {
			up = result.bytes
		} else {
			down = result.bytes
		}
		if isTimeout(result.err) {
			failures = append(failures, ErrTunnelIdleTimeout)
		} else if result.err != nil && !isExpectedClose(result.err) {
			failures = append(failures, errors.New("proxy tunnel transfer failed"))
		}
	}
	if ctx.Err() != nil {
		failures = append(failures, ctx.Err())
	}
	return up, down, errors.Join(failures...)
}

func copyHalf(results chan<- copyResult, direction string, destination net.Conn, source io.Reader, refresher *tunnelDeadlineRefresher) {
	var writer io.Writer = destination
	if refresher != nil {
		writer = &activityWriter{writer: destination, refresher: refresher}
	}
	written, err := io.Copy(writer, source)
	if closeWriter, ok := destination.(interface{ CloseWrite() error }); ok {
		_ = closeWriter.CloseWrite()
	}
	results <- copyResult{direction: direction, bytes: written, err: err}
}

type tunnelDeadlineRefresher struct {
	mu          sync.Mutex
	client      net.Conn
	upstream    net.Conn
	idleTimeout time.Duration
}

func (r *tunnelDeadlineRefresher) touch() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	deadline := time.Now().Add(r.idleTimeout)
	return errors.Join(r.client.SetDeadline(deadline), r.upstream.SetDeadline(deadline))
}

type activityWriter struct {
	writer    io.Writer
	refresher *tunnelDeadlineRefresher
}

func (w *activityWriter) Write(payload []byte) (int, error) {
	written, err := w.writer.Write(payload)
	if written > 0 {
		err = errors.Join(err, w.refresher.touch())
	}
	return written, err
}

func isTimeout(err error) bool {
	var timeout net.Error
	return errors.As(err, &timeout) && timeout.Timeout()
}

func isExpectedClose(err error) bool {
	return err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed)
}
