package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"unicode/utf8"

	"github.com/s12ryt/s12ryt-ipv6/internal/eventlog"
	"github.com/s12ryt/s12ryt-ipv6/internal/node"
	"github.com/s12ryt/s12ryt-ipv6/internal/stats"
)

type TrafficLogger interface {
	Write(eventlog.Event) error
}

type TrafficObserver struct {
	stats  *stats.Registry
	logger TrafficLogger
	report func(error)
}

func NewTrafficObserver(registry *stats.Registry, logger TrafficLogger, report func(error)) (*TrafficObserver, error) {
	if registry == nil {
		return nil, errors.New("traffic statistics registry is required")
	}
	if logger == nil {
		return nil, errors.New("traffic event logger is required")
	}
	if report == nil {
		report = func(error) {}
	}
	return &TrafficObserver{stats: registry, logger: logger, report: report}, nil
}

func (o *TrafficObserver) Observe(event node.TrafficEvent) {
	switch event.Lifecycle {
	case node.TrafficTCPOpened:
		o.stats.TCPOpened(event.NodeID)
		return
	case node.TrafficUDPOpened:
		o.stats.UDPAssociationOpened(event.NodeID)
		return
	case node.TrafficTCPClosed:
		o.stats.TCPClosed(event.NodeID, uint64(event.Traffic.UpBytes), uint64(event.Traffic.DownBytes), event.Error != nil)
		o.write(event, "connection.closed", "proxy connection failed", true)
	case node.TrafficUDPClosed:
		o.stats.UDPAssociationClosed(event.NodeID, uint64(event.Traffic.UpBytes), uint64(event.Traffic.DownBytes), event.Error != nil)
		o.write(event, "association.closed", "proxy association failed", true)
	case node.TrafficTCPRejected:
		o.stats.TCPOpened(event.NodeID)
		o.stats.TCPClosed(event.NodeID, 0, 0, true)
		message := "connection rejected"
		if errors.Is(event.Error, node.ErrTCPConnectionLimit) {
			message = "connection limit reached"
		}
		o.write(event, "connection.rejected", message, false)
	}
}

func (o *TrafficObserver) write(event node.TrafficEvent, action, failure string, classify bool) {
	record := eventlog.Event{
		Kind: eventlog.KindProxy, Action: action, Node: event.NodeID,
		Protocol: event.Traffic.Protocol, Success: event.Error == nil,
	}
	if event.SourceIP.IsValid() {
		record.SourceIP = event.SourceIP.String()
	}
	if event.Traffic.Metadata.Destination.IsValid() {
		record.DestinationHost = event.Traffic.Metadata.Destination.Addr().String()
		record.DestinationPort = event.Traffic.Metadata.Destination.Port()
	}
	if event.Traffic.Metadata.Source.IsValid() {
		record.OutboundIP = event.Traffic.Metadata.Source.String()
	}
	if event.Error != nil {
		record.Error = failure
		if classify {
			record.Error = failure + ": " + describeDialError(event.Error)
		}
	}
	if err := o.logger.Write(record); err != nil {
		o.report(fmt.Errorf("write proxy traffic event: %w", err))
	}
}

// describeDialError maps a proxy dial or association failure to a short,
// classification-first description for the event log. Recognized kernel and
// network failures get stable labels so operators can group incidents; DNS
// errors never include the queried name. Unknown errors fall back to the error
// text, truncated and still subject to eventlog secret redaction.
func describeDialError(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		switch {
		case dnsErr.IsNotFound:
			return "dns lookup not found"
		case dnsErr.IsTimeout:
			return "dns lookup timeout"
		case dnsErr.IsTemporary:
			return "dns lookup temporary failure"
		}
		return "dns lookup failed"
	}
	switch {
	case errors.Is(err, syscall.EMFILE), errors.Is(err, syscall.ENFILE):
		return "fd limit reached"
	case errors.Is(err, syscall.EADDRNOTAVAIL):
		return "source address unavailable"
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return "permission denied"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, syscall.ENETUNREACH):
		return "network unreachable"
	case errors.Is(err, syscall.EHOSTUNREACH):
		return "host unreachable"
	case errors.Is(err, syscall.ETIMEDOUT):
		return "connection timed out"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection reset"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded):
		return "deadline exceeded"
	}
	return truncateErrorDetail(err.Error())
}

// truncateErrorDetail bounds unknown error text at 200 bytes without splitting
// multi-byte runes.
func truncateErrorDetail(message string) string {
	const limit = 200
	if len(message) <= limit {
		return message
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut] + "..."
}
