package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/eventlog"
	"github.com/s12ryt/s12ryt-ipv6/internal/node"
)

type watchdogRecorder struct {
	mu       sync.Mutex
	events   []eventlog.Event
	restarts int
}

func (r *watchdogRecorder) snapshotEvents() []eventlog.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]eventlog.Event(nil), r.events...)
}

func (r *watchdogRecorder) restartCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.restarts
}

func newWatchdogForTest(t *testing.T, mutate func(*WatchdogOptions)) (*watchdog, *watchdogRecorder) {
	t.Helper()
	rec := &watchdogRecorder{}
	options := WatchdogOptions{
		Interval: 10 * time.Millisecond,
		Timeout:  50 * time.Millisecond,
		Failures: 3,
		Cooldown: time.Hour,
		Targets: func() []ProbeTarget {
			return []ProbeTarget{{
				Node: "node-1", Address: "127.0.0.1:1080", Protocol: "socks5", Username: "u", Password: "p",
			}}
		},
		Probe: func(context.Context, ProbeTarget) error {
			return errors.New("dial tcp 1.2.3.4:443: connect: connection refused")
		},
		Restart: func() error { rec.mu.Lock(); rec.restarts++; rec.mu.Unlock(); return nil },
		Now:     func() time.Time { return time.Now() },
		Random:  func(n int) int { return 0 },
		OnEvent: func(event eventlog.Event) {
			rec.mu.Lock()
			rec.events = append(rec.events, event)
			rec.mu.Unlock()
		},
	}
	if mutate != nil {
		mutate(&options)
	}
	w, err := NewWatchdog(options)
	if err != nil {
		t.Fatalf("NewWatchdog() error = %v", err)
	}
	return w, rec
}

func TestWatchdogRequiresValidOptions(t *testing.T) {
	base := func() WatchdogOptions {
		return WatchdogOptions{
			Interval: time.Second, Timeout: time.Second, Failures: 3, Cooldown: time.Minute,
			Targets: func() []ProbeTarget { return nil },
			Probe:   func(context.Context, ProbeTarget) error { return nil },
			Restart: func() error { return nil },
			Now:     func() time.Time { return time.Now() },
			Random:  func(n int) int { return 0 },
			OnEvent: func(eventlog.Event) {},
		}
	}
	cases := []struct {
		name   string
		mutate func(*WatchdogOptions)
	}{
		{"interval", func(o *WatchdogOptions) { o.Interval = 0 }},
		{"timeout", func(o *WatchdogOptions) { o.Timeout = 0 }},
		{"failures", func(o *WatchdogOptions) { o.Failures = 0 }},
		{"targets", func(o *WatchdogOptions) { o.Targets = nil }},
		{"probe", func(o *WatchdogOptions) { o.Probe = nil }},
		{"now", func(o *WatchdogOptions) { o.Now = nil }},
		{"random", func(o *WatchdogOptions) { o.Random = nil }},
		{"onevent", func(o *WatchdogOptions) { o.OnEvent = nil }},
	}
	for _, tc := range cases {
		options := base()
		tc.mutate(&options)
		if _, err := NewWatchdog(options); err == nil {
			t.Fatalf("NewWatchdog() with invalid %s = nil error, want error", tc.name)
		}
	}
	if _, err := NewWatchdog(base()); err != nil {
		t.Fatalf("NewWatchdog(valid) error = %v", err)
	}
	// Restart nil is allowed (log-only watchdog).
	options := base()
	options.Restart = nil
	if _, err := NewWatchdog(options); err != nil {
		t.Fatalf("NewWatchdog(restart nil) error = %v", err)
	}
}

func TestWatchdogCountsFailuresAndRestartsAfterThreshold(t *testing.T) {
	w, rec := newWatchdogForTest(t, nil)
	w.probeOnce(context.Background())
	w.probeOnce(context.Background())
	if got := rec.restartCount(); got != 0 {
		t.Fatalf("restarts after 2 failures = %d, want 0", got)
	}

	w.probeOnce(context.Background())
	if got := rec.restartCount(); got != 1 {
		t.Fatalf("restarts after 3 failures = %d, want 1", got)
	}

	probeEvents := 0
	restartEvents := 0
	for _, event := range rec.snapshotEvents() {
		if event.Action == "watchdog.probe" {
			probeEvents++
			if event.Success {
				t.Fatalf("watchdog.probe event success = true, want false")
			}
			if event.Node != "node-1" {
				t.Fatalf("watchdog.probe node = %q, want node-1", event.Node)
			}
			if !strings.Contains(event.Error, "proxy probe failed: ") || !strings.Contains(event.Error, "connection refused") {
				t.Fatalf("watchdog.probe error = %q, want classified dial error", event.Error)
			}
		}
		if event.Action == "watchdog.restart" {
			restartEvents++
			if !event.Success {
				t.Fatalf("watchdog.restart event success = false, want true")
			}
		}
	}
	if probeEvents != 3 {
		t.Fatalf("watchdog.probe events = %d, want 3", probeEvents)
	}
	if restartEvents != 1 {
		t.Fatalf("watchdog.restart events = %d, want 1", restartEvents)
	}
}

func TestWatchdogSuccessResetsFailureCount(t *testing.T) {
	w, _ := newWatchdogForTest(t, nil)
	probeErr := errors.New("dial tcp: connection refused")
	failing := true
	w.options.Probe = func(context.Context, ProbeTarget) error {
		if failing {
			return probeErr
		}
		return nil
	}
	w.probeOnce(context.Background())
	w.probeOnce(context.Background())
	failing = false
	w.probeOnce(context.Background())
	failing = true
	w.probeOnce(context.Background())
	w.probeOnce(context.Background())
	if got := w.failureCount(); got != 2 {
		t.Fatalf("failureCount() = %d, want 2 after reset", got)
	}
}

func TestWatchdogTracksConsecutiveFailuresPerTarget(t *testing.T) {
	targets := []ProbeTarget{
		{Node: "broken", Address: "127.0.0.1:1080", Protocol: "socks5"},
		{Node: "healthy", Address: "127.0.0.1:1081", Protocol: "socks5"},
	}
	selections := []int{0, 1, 0, 0}
	selection := 0
	w, rec := newWatchdogForTest(t, func(o *WatchdogOptions) {
		o.Targets = func() []ProbeTarget { return targets }
		o.Random = func(int) int {
			selected := selections[selection]
			selection++
			return selected
		}
		o.Probe = func(_ context.Context, target ProbeTarget) error {
			if target.Node == "broken" {
				return errors.New("broken target")
			}
			return nil
		}
	})

	for range selections {
		w.probeOnce(context.Background())
	}
	if got := rec.restartCount(); got != 1 {
		t.Fatalf("restarts after 3 failures for one target = %d, want 1", got)
	}
}

func TestWatchdogRetriesFailingTargetBeforeRandomSelection(t *testing.T) {
	targets := []ProbeTarget{
		{Node: "broken", Address: "127.0.0.1:1080", Protocol: "socks"},
		{Node: "healthy", Address: "127.0.0.1:1081", Protocol: "socks"},
	}
	randomCalls := 0
	probed := make([]string, 0, 2)
	w, _ := newWatchdogForTest(t, func(o *WatchdogOptions) {
		o.Targets = func() []ProbeTarget { return targets }
		o.Random = func(int) int {
			index := randomCalls
			randomCalls++
			return index
		}
		o.Probe = func(_ context.Context, target ProbeTarget) error {
			probed = append(probed, target.Node)
			if target.Node == "broken" {
				return errors.New("broken")
			}
			return nil
		}
	})

	w.probeOnce(context.Background())
	w.probeOnce(context.Background())
	if got := strings.Join(probed, ","); got != "broken,broken" {
		t.Fatalf("probed targets = %q, want broken,broken", got)
	}
	if randomCalls != 1 {
		t.Fatalf("random selections = %d, want 1 while retrying failed target", randomCalls)
	}
}

func TestWatchdogSkipsWhenNoTargets(t *testing.T) {
	w, _ := newWatchdogForTest(t, func(o *WatchdogOptions) {
		o.Targets = func() []ProbeTarget { return nil }
	})
	w.probeOnce(context.Background())
	if got := w.failureCount(); got != 0 {
		t.Fatalf("failureCount() = %d, want 0 with no targets", got)
	}
}

func TestWatchdogDropsFailuresForRemovedTargets(t *testing.T) {
	targets := []ProbeTarget{{Node: "removed", Address: "127.0.0.1:1080", Protocol: "socks"}}
	w, _ := newWatchdogForTest(t, func(o *WatchdogOptions) {
		o.Targets = func() []ProbeTarget { return targets }
	})

	w.probeOnce(context.Background())
	if got := w.failureCount(); got != 1 {
		t.Fatalf("failureCount() = %d, want 1 before target removal", got)
	}
	targets = nil
	w.probeOnce(context.Background())
	if got := w.failureCount(); got != 0 {
		t.Fatalf("failureCount() = %d, want 0 after target removal", got)
	}
}

func TestWatchdogCooldownBlocksRepeatRestart(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	w, rec := newWatchdogForTest(t, func(o *WatchdogOptions) {
		o.Cooldown = 10 * time.Minute
		o.Now = func() time.Time { return now }
	})
	for i := 0; i < 6; i++ {
		w.probeOnce(context.Background())
		now = now.Add(time.Minute)
	}
	if got := rec.restartCount(); got != 1 {
		t.Fatalf("restarts within cooldown = %d, want 1", got)
	}
	now = now.Add(30 * time.Minute)
	w.probeOnce(context.Background())
	if got := rec.restartCount(); got != 2 {
		t.Fatalf("restarts after cooldown = %d, want 2", got)
	}
}

func TestWatchdogRestartNilLogsOnly(t *testing.T) {
	w, rec := newWatchdogForTest(t, func(o *WatchdogOptions) {
		o.Restart = nil
	})
	w.probeOnce(context.Background())
	w.probeOnce(context.Background())
	w.probeOnce(context.Background())
	found := false
	for _, event := range rec.snapshotEvents() {
		if event.Action == "watchdog.restart" {
			found = true
			if event.Success {
				t.Fatalf("watchdog.restart success = true with nil restart, want false")
			}
			if !strings.Contains(event.Error, "unavailable") {
				t.Fatalf("watchdog.restart error = %q, want unavailable", event.Error)
			}
		}
	}
	if !found {
		t.Fatal("watchdog.restart event missing with nil restart")
	}
}

func TestWatchdogRunStopsOnContextCancel(t *testing.T) {
	w, _ := newWatchdogForTest(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not stop after context cancel")
	}
}

// ---- protocol probe tests over net.Pipe ----

func serveSocks5Probe(t *testing.T, server net.Conn, requireAuth bool) {
	t.Helper()
	reader := bufio.NewReader(server)
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return
	}
	hasNoAuth := false
	hasUserPass := false
	for _, m := range methods {
		switch m {
		case 0x00:
			hasNoAuth = true
		case 0x02:
			hasUserPass = true
		}
	}
	if requireAuth {
		if !hasUserPass {
			_, _ = server.Write([]byte{0x05, 0xFF})
			return
		}
		_, _ = server.Write([]byte{0x05, 0x02})
		ver := make([]byte, 2)
		if _, err := io.ReadFull(reader, ver); err != nil {
			return
		}
		user := make([]byte, ver[1])
		if _, err := io.ReadFull(reader, user); err != nil {
			return
		}
		passLen := make([]byte, 1)
		if _, err := io.ReadFull(reader, passLen); err != nil {
			return
		}
		pass := make([]byte, passLen[0])
		if _, err := io.ReadFull(reader, pass); err != nil {
			return
		}
		if string(user) != "u" || string(pass) != "p" {
			_, _ = server.Write([]byte{0x01, 0x01})
			return
		}
		_, _ = server.Write([]byte{0x01, 0x00})
	} else {
		if !hasNoAuth {
			_, _ = server.Write([]byte{0x05, 0xFF})
			return
		}
		_, _ = server.Write([]byte{0x05, 0x00})
	}
	request := make([]byte, 4)
	if _, err := io.ReadFull(reader, request); err != nil {
		return
	}
	if request[3] == 0x03 {
		lenByte := make([]byte, 1)
		if _, err := io.ReadFull(reader, lenByte); err != nil {
			return
		}
		rest := make([]byte, int(lenByte[0])+2)
		if _, err := io.ReadFull(reader, rest); err != nil {
			return
		}
	} else if request[3] == 0x01 {
		rest := make([]byte, 4+2)
		if _, err := io.ReadFull(reader, rest); err != nil {
			return
		}
	}
	_, _ = server.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
}

func TestSocks5ProbeHandshakeSucceedsWithoutAuth(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go serveSocks5Probe(t, server, false)
	if err := socks5ProbeHandshake(client, "one.one.one.one:443", "", ""); err != nil {
		t.Fatalf("socks5ProbeHandshake() error = %v", err)
	}
	server.Close()
}

func TestSocks5ProbeHandshakeSucceedsWithAuth(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go serveSocks5Probe(t, server, true)
	if err := socks5ProbeHandshake(client, "one.one.one.one:443", "u", "p"); err != nil {
		t.Fatalf("socks5ProbeHandshake(auth) error = %v", err)
	}
	server.Close()
}

func TestSocks5ProbeHandshakeRejectsAuthFailure(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go serveSocks5Probe(t, server, true)
	err := socks5ProbeHandshake(client, "one.one.one.one:443", "u", "wrong")
	if err == nil || !strings.Contains(err.Error(), "authentication") {
		t.Fatalf("socks5ProbeHandshake(bad password) = %v, want authentication error", err)
	}
	server.Close()
}

func TestSocks5ProbeHandshakeRejectsRefusedReply(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go func() {
		reader := bufio.NewReader(server)
		header := make([]byte, 2)
		if _, err := io.ReadFull(reader, header); err != nil {
			return
		}
		methods := make([]byte, int(header[1]))
		if _, err := io.ReadFull(reader, methods); err != nil {
			return
		}
		_, _ = server.Write([]byte{0x05, 0x00})
		request := make([]byte, 4)
		if _, err := io.ReadFull(reader, request); err != nil {
			return
		}
		if request[3] == 0x03 {
			lenByte := make([]byte, 1)
			if _, err := io.ReadFull(reader, lenByte); err != nil {
				return
			}
			rest := make([]byte, int(lenByte[0])+2)
			if _, err := io.ReadFull(reader, rest); err != nil {
				return
			}
		}
		_, _ = server.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	}()
	err := socks5ProbeHandshake(client, "one.one.one.one:443", "", "")
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("socks5ProbeHandshake(refused) = %v, want refused error", err)
	}
	server.Close()
}

func serveHTTPConnectProbe(t *testing.T, server net.Conn, status string, expectUser string, expectPass string) {
	t.Helper()
	reader := bufio.NewReader(server)
	request, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	_ = request
	auth := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		if strings.HasPrefix(strings.ToLower(line), "proxy-authorization:") {
			auth = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		}
		if line == "\r\n" {
			break
		}
	}
	if expectUser != "" {
		expected := basicProxyAuthHeader(expectUser, expectPass)
		if auth != expected {
			_, _ = fmt.Fprintf(server, "HTTP/1.1 407 Proxy Authentication Required\r\n\r\n")
			return
		}
	}
	_, _ = fmt.Fprintf(server, "HTTP/1.1 %s\r\n\r\n", status)
}

func TestHTTPConnectProbeSucceeds(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go serveHTTPConnectProbe(t, server, "200 Connection established", "", "")
	if err := httpConnectProbe(client, "one.one.one.one:443", "", ""); err != nil {
		t.Fatalf("httpConnectProbe() error = %v", err)
	}
	server.Close()
}

func TestHTTPConnectProbeSendsBasicAuth(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go serveHTTPConnectProbe(t, server, "200 Connection established", "u", "p")
	if err := httpConnectProbe(client, "one.one.one.one:443", "u", "p"); err != nil {
		t.Fatalf("httpConnectProbe(auth) error = %v", err)
	}
	server.Close()
}

func TestHTTPConnectProbeRejectsFailureStatus(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go serveHTTPConnectProbe(t, server, "502 Bad Gateway", "", "")
	err := httpConnectProbe(client, "one.one.one.one:443", "", "")
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("httpConnectProbe(502) = %v, want 502 error", err)
	}
	server.Close()
}

func TestProbeViaProxyUsesListenerAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		serveHTTPConnectProbe(t, conn, "200 Connection established", "u", "p")
	}()
	address := listener.Addr().String()
	target := ProbeTarget{Node: "n", Address: address, Protocol: "http", Username: "u", Password: "p"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := probeViaProxyDestination(ctx, target, "one.one.one.one:443"); err != nil {
		t.Fatalf("probeViaProxyDestination() error = %v", err)
	}
}

func TestProbeViaProxyUsesSOCKSHandshakeForNodeProtocol(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		serveSocks5Probe(t, conn, false)
	}()
	target := ProbeTarget{
		Node: "n", Address: listener.Addr().String(), Protocol: string(node.ProtocolSOCKS),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := probeViaProxyDestination(ctx, target, "one.one.one.one:443"); err != nil {
		t.Fatalf("probeViaProxyDestination(node SOCKS protocol) error = %v", err)
	}
}
