package node

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
)

type ResourceStateSynchronizer interface {
	Sync(ipv6resource.State) error
}

type InboundStateSynchronizer interface {
	ResourceStateSynchronizer
	InboundConfigResolver
}

type InboundBindingRefresher interface {
	RefreshInboundBindings(context.Context, InboundConfigResolver, InboundDrainedObserver) error
	List() []Node
}

type DrainStatePreparer interface {
	Prepare(ipv6resource.State, []Node) error
}

type RuntimeResourceSynchronizerOptions struct {
	Policy           ResourceStateSynchronizer
	Outbound         ResourceStateSynchronizer
	Inbound          InboundStateSynchronizer
	Nodes            InboundBindingRefresher
	Drains           DrainStatePreparer
	Timeout          time.Duration
	OnInboundDrained InboundDrainedObserver
}

type RuntimeResourceSynchronizer struct {
	policy           ResourceStateSynchronizer
	outbound         ResourceStateSynchronizer
	inbound          InboundStateSynchronizer
	nodes            InboundBindingRefresher
	drains           DrainStatePreparer
	timeout          time.Duration
	onInboundDrained InboundDrainedObserver
}

func NewRuntimeResourceSynchronizer(options RuntimeResourceSynchronizerOptions) (*RuntimeResourceSynchronizer, error) {
	if options.Policy == nil {
		return nil, errors.New("destination policy synchronizer is required")
	}
	if options.Outbound == nil {
		return nil, errors.New("outbound resource synchronizer is required")
	}
	if options.Inbound == nil {
		return nil, errors.New("inbound resource synchronizer is required")
	}
	if options.Nodes == nil {
		return nil, errors.New("node binding refresher is required")
	}
	if options.Drains == nil {
		return nil, errors.New("drain state preparer is required")
	}
	if options.Timeout <= 0 {
		return nil, errors.New("resource runtime synchronization timeout must be positive")
	}
	return &RuntimeResourceSynchronizer{
		policy: options.Policy, outbound: options.Outbound, inbound: options.Inbound, nodes: options.Nodes, drains: options.Drains,
		timeout: options.Timeout, onInboundDrained: options.OnInboundDrained,
	}, nil
}

func (s *RuntimeResourceSynchronizer) Sync(state ipv6resource.State) error {
	if err := s.drains.Prepare(state, s.nodes.List()); err != nil {
		return fmt.Errorf("prepare draining resources: %w", err)
	}
	if err := s.policy.Sync(state); err != nil {
		return fmt.Errorf("synchronize destination policy: %w", err)
	}
	if err := s.outbound.Sync(state); err != nil {
		return fmt.Errorf("synchronize outbound resources: %w", err)
	}
	if err := s.inbound.Sync(state); err != nil {
		return fmt.Errorf("synchronize inbound resources: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	if err := s.nodes.RefreshInboundBindings(ctx, s.inbound, s.onInboundDrained); err != nil {
		return fmt.Errorf("refresh node inbound bindings: %w", err)
	}
	return nil
}

var _ ResourceStateSynchronizer = (*RuntimeResourceSynchronizer)(nil)
