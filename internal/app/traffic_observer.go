package app

import (
	"errors"
	"fmt"

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
		o.write(event, "connection.closed", "proxy connection failed")
	case node.TrafficUDPClosed:
		o.stats.UDPAssociationClosed(event.NodeID, uint64(event.Traffic.UpBytes), uint64(event.Traffic.DownBytes), event.Error != nil)
		o.write(event, "association.closed", "proxy association failed")
	case node.TrafficTCPRejected:
		o.stats.TCPOpened(event.NodeID)
		o.stats.TCPClosed(event.NodeID, 0, 0, true)
		message := "connection rejected"
		if errors.Is(event.Error, node.ErrTCPConnectionLimit) {
			message = "connection limit reached"
		}
		o.write(event, "connection.rejected", message)
	}
}

func (o *TrafficObserver) write(event node.TrafficEvent, action, failure string) {
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
	}
	if err := o.logger.Write(record); err != nil {
		o.report(fmt.Errorf("write proxy traffic event: %w", err))
	}
}
