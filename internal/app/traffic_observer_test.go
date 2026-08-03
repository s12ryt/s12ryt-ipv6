package app

import (
	"errors"
	"net/netip"
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
		tcp.Error != "proxy connection failed" {
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
