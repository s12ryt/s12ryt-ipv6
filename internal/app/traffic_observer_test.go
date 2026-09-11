package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/eventlog"
	"github.com/s12ryt/s12ryt-ipv6/internal/node"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
	"github.com/s12ryt/s12ryt-ipv6/internal/stats"
)

type recordingTrafficLogger struct {
	events []eventlog.Event
}

func (l *recordingTrafficLogger) Write(event eventlog.Event) error {
	l.events = append(l.events, event)
	return nil
}

func TestTrafficObserverMaintainsActiveAndCumulativeStatistics(t *testing.T) {
	registry := stats.NewRegistry()
	logger := &recordingTrafficLogger{}
	reported := make(chan error, 1)
	observer, err := NewTrafficObserver(registry, logger, func(err error) { reported <- err })
	if err != nil {
		t.Fatal(err)
	}
	source := netip.MustParseAddr("2001:db8::10")
	destination := netip.MustParseAddrPort("[2001:4860:4860::8888]:443")
	observer.Observe(node.TrafficEvent{Lifecycle: node.TrafficTCPOpened, NodeID: "edge", SourceIP: source})
	if got := registry.Snapshot().Nodes["edge"]; got.ActiveTCP != 1 || got.TotalConnections != 1 {
		t.Fatalf("counters after TCP open = %#v", got)
	}
	observer.Observe(node.TrafficEvent{
		Lifecycle: node.TrafficTCPClosed, NodeID: "edge", SourceIP: source,
		Traffic: proxy.ProxyTraffic{
			Protocol: "socks", UpBytes: 12, DownBytes: 34,
			Metadata: proxy.DialMetadata{Destination: destination, Source: netip.MustParseAddr("2001:4860:1::1"), Resolver: "cloudflare"},
		},
		Error: errors.New("secret upstream detail"),
	})
	observer.Observe(node.TrafficEvent{Lifecycle: node.TrafficUDPOpened, NodeID: "edge", SourceIP: source})
	observer.Observe(node.TrafficEvent{
		Lifecycle: node.TrafficUDPClosed, NodeID: "edge", SourceIP: source,
		Traffic: proxy.ProxyTraffic{Protocol: "socks", UpBytes: 5, DownBytes: 7},
	})
	observer.Observe(node.TrafficEvent{
		Lifecycle: node.TrafficTCPRejected, NodeID: "edge", SourceIP: source,
		Error: node.ErrTCPConnectionLimit, Rejected: true,
	})

	got := registry.Snapshot().Nodes["edge"]
	if got.ActiveTCP != 0 || got.ActiveUDP != 0 || got.TotalConnections != 3 || got.BytesUp != 17 || got.BytesDown != 41 || got.Errors != 2 {
		t.Fatalf("final counters = %#v", got)
	}
	if len(logger.events) != 3 {
		t.Fatalf("logged events = %#v", logger.events)
	}
	tcp := logger.events[0]
	if tcp.Kind != eventlog.KindProxy || tcp.Action != "connection.closed" || tcp.Success ||
		tcp.SourceIP != source.String() || tcp.DestinationHost != destination.Addr().String() ||
		tcp.DestinationPort != destination.Port() || tcp.OutboundIP != "2001:4860:1::1" ||
		tcp.Error != "proxy connection failed: secret upstream detail" {
		t.Fatalf("TCP log = %#v", tcp)
	}
	if logger.events[1].Action != "association.closed" || !logger.events[1].Success {
		t.Fatalf("UDP log = %#v", logger.events[1])
	}
	if logger.events[2].Action != "connection.rejected" || logger.events[2].Error != "connection limit reached" {
		t.Fatalf("rejection log = %#v", logger.events[2])
	}
	select {
	case err := <-reported:
		t.Fatalf("unexpected report: %v", err)
	default:
	}
}

func TestTrafficObserverReportsLogFailureAndValidatesDependencies(t *testing.T) {
	registry := stats.NewRegistry()
	logger := &failingTrafficLogger{err: errors.New("disk failed")}
	reported := make(chan error, 1)
	observer, err := NewTrafficObserver(registry, logger, func(err error) { reported <- err })
	if err != nil {
		t.Fatal(err)
	}
	observer.Observe(node.TrafficEvent{Lifecycle: node.TrafficTCPRejected, NodeID: "edge", Error: errors.New("rejected")})
	if err := <-reported; !errors.Is(err, logger.err) {
		t.Fatalf("reported error = %v", err)
	}
	if _, err := NewTrafficObserver(nil, logger, nil); err == nil {
		t.Fatal("NewTrafficObserver(nil stats) error = nil")
	}
	if _, err := NewTrafficObserver(registry, nil, nil); err == nil {
		t.Fatal("NewTrafficObserver(nil logger) error = nil")
	}
}

type failingTrafficLogger struct {
	err error
}

func (l *failingTrafficLogger) Write(eventlog.Event) error { return l.err }

func TestTrafficObserverRecordsRealDialErrorClassification(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name: "emfile maps to fd limit reached",
			err: &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{
				Syscall: "socket", Err: syscall.EMFILE,
			}},
			expected: "proxy connection failed: fd limit reached",
		},
		{
			name: "enfile maps to fd limit reached",
			err: &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{
				Syscall: "connect", Err: syscall.ENFILE,
			}},
			expected: "proxy connection failed: fd limit reached",
		},
		{
			name: "eaddrnotavail maps to source address unavailable",
			err: &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{
				Syscall: "connect", Err: syscall.EADDRNOTAVAIL,
			}},
			expected: "proxy connection failed: source address unavailable",
		},
		{
			name: "econnrefused maps to connection refused",
			err: &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{
				Syscall: "connect", Err: syscall.ECONNREFUSED,
			}},
			expected: "proxy connection failed: connection refused",
		},
		{
			name: "enetunreach maps to network unreachable",
			err: &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{
				Syscall: "connect", Err: syscall.ENETUNREACH,
			}},
			expected: "proxy connection failed: network unreachable",
		},
		{
			name: "kernel etimedout maps to connection timed out",
			err: &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{
				Syscall: "connect", Err: syscall.ETIMEDOUT,
			}},
			expected: "proxy connection failed: connection timed out",
		},
		{
			name:     "context deadline maps to deadline exceeded",
			err:      fmt.Errorf("dial: %w", context.DeadlineExceeded),
			expected: "proxy connection failed: deadline exceeded",
		},
		{
			name:     "dns timeout maps to dns classification without leaking query name",
			err:      &net.DNSError{Err: "i/o timeout", Name: "secret-example.invalid", IsTimeout: true, IsTemporary: true},
			expected: "proxy connection failed: dns lookup timeout",
		},
		{
			name:     "dns not found maps to dns classification without leaking query name",
			err:      &net.DNSError{Err: "no such host", Name: "secret-example.invalid", IsNotFound: true},
			expected: "proxy connection failed: dns lookup not found",
		},
		{
			name:     "unknown error falls back to truncated message",
			err:      errors.New(strings.Repeat("x", 500)),
			expected: "proxy connection failed: " + strings.Repeat("x", 200) + "...",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry := stats.NewRegistry()
			logger := &recordingTrafficLogger{}
			observer, err := NewTrafficObserver(registry, logger, nil)
			if err != nil {
				t.Fatal(err)
			}
			observer.Observe(node.TrafficEvent{
				Lifecycle: node.TrafficTCPClosed, NodeID: "edge",
				Traffic: proxy.ProxyTraffic{Protocol: "socks"},
				Error:   tc.err,
			})
			if len(logger.events) != 1 {
				t.Fatalf("logged events = %#v", logger.events)
			}
			if got := logger.events[0].Error; got != tc.expected {
				t.Fatalf("error = %q, want %q", got, tc.expected)
			}
			if strings.Contains(logger.events[0].Error, "secret-example.invalid") {
				t.Fatalf("dns query name leaked: %q", logger.events[0].Error)
			}
		})
	}
}

func TestTrafficObserverRecordsUDPAssociationErrorClassification(t *testing.T) {
	registry := stats.NewRegistry()
	logger := &recordingTrafficLogger{}
	observer, err := NewTrafficObserver(registry, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	observer.Observe(node.TrafficEvent{
		Lifecycle: node.TrafficUDPClosed, NodeID: "edge",
		Traffic: proxy.ProxyTraffic{Protocol: "socks"},
		Error: &net.OpError{Op: "dial", Net: "udp6", Err: &os.SyscallError{
			Syscall: "socket", Err: syscall.EMFILE,
		}},
	})
	if len(logger.events) != 1 {
		t.Fatalf("logged events = %#v", logger.events)
	}
	if got := logger.events[0].Error; got != "proxy association failed: fd limit reached" {
		t.Fatalf("error = %q", got)
	}
}
