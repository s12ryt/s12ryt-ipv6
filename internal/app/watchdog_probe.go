package app

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/dns64"
)

var nativeProbeDestinations = [...]string{
	// Dual-stack sites operated by independent networks and organizations.
	"one.one.one.one:443",
	"dns.google:443",
	"www.wikipedia.org:443",
	"www.facebook.com:443",
	"www.microsoft.com:443",
	"www.debian.org:443",
	"www.kernel.org:443",
	"www.quad9.net:443",
	"www.he.net:443",
	// IPv6-only sites with no A record.
	"v6.ident.me:443",
	"api6.ipify.org:443",
	"ipv6.google.com:443",
}

const (
	dns64ProbeDestination = "ipv4.google.com:443"
	nat64ProbeDestination = "1.1.1.1:443"
)

func watchdogProbeDestinations(nat64Enabled bool) []string {
	destinations := make([]string, 0, len(nativeProbeDestinations)+2)
	destinations = append(destinations, nativeProbeDestinations[:]...)
	if nat64Enabled {
		destinations = append(destinations, dns64ProbeDestination, nat64ProbeDestination)
	}
	return destinations
}

func nat64WatchdogProbeEnabled(status dns64.NAT64Status) bool {
	return status.Manual || status.State == dns64.NAT64Healthy && status.Prefix.IsValid()
}

func newProxyWatchdogProbe(
	nat64Enabled func() bool,
	probe func(context.Context, ProbeTarget, string) error,
) func(context.Context, ProbeTarget) error {
	return func(ctx context.Context, target ProbeTarget) error {
		destinations := watchdogProbeDestinations(nat64Enabled())
		errs := make([]error, len(destinations))
		for index, destination := range destinations {
			probeCtx, cancel := watchdogProbeContext(ctx, len(destinations)-index)
			err := probe(probeCtx, target, destination)
			cancel()
			if err != nil {
				errs[index] = fmt.Errorf("%s: %w", destination, err)
			}
		}
		for _, err := range errs {
			if err == nil {
				return nil
			}
		}
		return errors.Join(errs...)
	}
}

func watchdogProbeContext(parent context.Context, remainingDestinations int) (context.Context, context.CancelFunc) {
	deadline, ok := parent.Deadline()
	if !ok || remainingDestinations <= 0 {
		return context.WithCancel(parent)
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, remaining/time.Duration(remainingDestinations))
}

// probeViaProxyDestination dials the proxy listener and performs a complete
// proxy handshake toward destination. SOCKS listeners use the SOCKS5 CONNECT
// flow; HTTP and mixed listeners use HTTP CONNECT.
func probeViaProxyDestination(parent context.Context, target ProbeTarget, destination string) error {
	var dialer net.Dialer
	conn, err := dialer.DialContext(parent, "tcp", target.Address)
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, ok := parent.Deadline()
	if !ok {
		deadline = time.Now().Add(15 * time.Second)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	if target.Protocol == "socks" || target.Protocol == "socks5" {
		return socks5ProbeHandshake(conn, destination, target.Username, target.Password)
	}
	return httpConnectProbe(conn, destination, target.Username, target.Password)
}

func socks5ProbeHandshake(conn net.Conn, destination, username, password string) error {
	var methods []byte
	if username != "" || password != "" {
		methods = []byte{0x02, 0x00} // username/password, no-auth
	} else {
		methods = []byte{0x00}
	}
	greeting := append([]byte{0x05, byte(len(methods))}, methods...)
	if _, err := conn.Write(greeting); err != nil {
		return err
	}
	choice := make([]byte, 2)
	if _, err := io.ReadFull(conn, choice); err != nil {
		return err
	}
	if choice[0] != 0x05 {
		return fmt.Errorf("socks5: unexpected protocol version %#x", choice[0])
	}
	switch choice[1] {
	case 0x00:
		// no authentication required
	case 0x02:
		auth := []byte{0x01, byte(len(username))}
		auth = append(auth, username...)
		auth = append(auth, byte(len(password)))
		auth = append(auth, password...)
		if _, err := conn.Write(auth); err != nil {
			return err
		}
		verdict := make([]byte, 2)
		if _, err := io.ReadFull(conn, verdict); err != nil {
			return err
		}
		if verdict[0] != 0x01 || verdict[1] != 0x00 {
			return errors.New("socks5: proxy authentication failed")
		}
	default:
		return fmt.Errorf("socks5: proxy rejected authentication methods (%#x)", choice[1])
	}
	host, portText, err := net.SplitHostPort(destination)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return err
	}
	request := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	request = append(request, host...)
	request = append(request, byte(port>>8), byte(port))
	if _, err := conn.Write(request); err != nil {
		return err
	}
	replyHeader := make([]byte, 4)
	if _, err := io.ReadFull(conn, replyHeader); err != nil {
		return err
	}
	if replyHeader[1] != 0x00 {
		return fmt.Errorf("socks5: connect reply %#x (%s)", replyHeader[1], socks5ReplyText(replyHeader[1]))
	}
	var boundLen int
	switch replyHeader[3] {
	case 0x01:
		boundLen = 4
	case 0x04:
		boundLen = 16
	case 0x03:
		lenByte := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenByte); err != nil {
			return err
		}
		boundLen = int(lenByte[0])
	default:
		boundLen = 0
	}
	if boundLen > 0 {
		bound := make([]byte, boundLen+2)
		if _, err := io.ReadFull(conn, bound); err != nil {
			return err
		}
	}
	return nil
}

func socks5ReplyText(code byte) string {
	switch code {
	case 0x01:
		return "general SOCKS server failure"
	case 0x02:
		return "connection not allowed by ruleset"
	case 0x03:
		return "network unreachable"
	case 0x04:
		return "host unreachable"
	case 0x05:
		return "connection refused"
	case 0x06:
		return "TTL expired"
	case 0x07:
		return "command not supported"
	case 0x08:
		return "address type not supported"
	default:
		return "unknown error"
	}
}

func basicProxyAuthHeader(username, password string) string {
	credentials := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return "Basic " + credentials
}

func httpConnectProbe(conn net.Conn, destination, username, password string) error {
	var request strings.Builder
	request.WriteString("CONNECT " + destination + " HTTP/1.1\r\n")
	request.WriteString("Host: " + destination + "\r\n")
	if username != "" || password != "" {
		request.WriteString("Proxy-Authorization: " + basicProxyAuthHeader(username, password) + "\r\n")
	}
	request.WriteString("\r\n")
	if _, err := conn.Write([]byte(request.String())); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	fields := strings.Fields(strings.TrimSpace(statusLine))
	if len(fields) < 2 {
		return fmt.Errorf("http proxy: malformed status line %q", strings.TrimSpace(statusLine))
	}
	if !strings.HasPrefix(fields[0], "HTTP/") {
		return fmt.Errorf("http proxy: malformed status line %q", strings.TrimSpace(statusLine))
	}
	statusCode, err := strconv.Atoi(fields[1])
	if err != nil {
		return fmt.Errorf("http proxy: malformed status code in %q", strings.TrimSpace(statusLine))
	}
	if statusCode < 200 || statusCode >= 300 {
		return fmt.Errorf("http proxy: CONNECT failed with %d", statusCode)
	}
	return nil
}
