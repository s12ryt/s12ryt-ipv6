package app

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
)

type OutboundDrainForcer interface {
	ForceDrain(string, []netip.Addr) error
}

type InboundDrainForcer interface {
	ForceDrainInbound(context.Context, string, []netip.Addr) error
}

type RuntimeDrainTerminator struct {
	outbound OutboundDrainForcer
	inbound  InboundDrainForcer
}

func NewRuntimeDrainTerminator(outbound OutboundDrainForcer, inbound InboundDrainForcer) (*RuntimeDrainTerminator, error) {
	if outbound == nil {
		return nil, errors.New("outbound drain forcer is required")
	}
	if inbound == nil {
		return nil, errors.New("inbound drain forcer is required")
	}
	return &RuntimeDrainTerminator{outbound: outbound, inbound: inbound}, nil
}

func (t *RuntimeDrainTerminator) ForceDrain(ctx context.Context, pool, batch string, kind ipv6resource.PoolKind, addresses []netip.Addr) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(pool) == "" {
		return errors.New("draining pool name is required")
	}
	if strings.TrimSpace(batch) == "" {
		return errors.New("draining batch ID is required")
	}
	if len(addresses) == 0 {
		return errors.New("draining batch must contain at least one address")
	}
	normalized := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !address.IsValid() || !address.Is6() || address.Is4In6() {
			return errors.New("draining address must be native IPv6")
		}
		if _, duplicate := seen[address]; duplicate {
			continue
		}
		seen[address] = struct{}{}
		normalized = append(normalized, address)
	}
	switch kind {
	case ipv6resource.PoolInbound:
		if err := t.inbound.ForceDrainInbound(ctx, pool, normalized); err != nil {
			return fmt.Errorf("force drain inbound batch %q: %w", batch, err)
		}
	case ipv6resource.PoolSharedOutbound, ipv6resource.PoolDedicatedOutbound:
		if err := t.outbound.ForceDrain(pool, normalized); err != nil {
			return fmt.Errorf("force drain outbound batch %q: %w", batch, err)
		}
	default:
		return fmt.Errorf("unsupported draining pool kind %q", kind)
	}
	return nil
}
