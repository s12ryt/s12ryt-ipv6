package app

import (
	"context"
	"errors"
	"net/netip"
	"sync"

	"github.com/s12ryt/s12ryt-ipv6/internal/admin"
	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
)

type DeferredDrainTerminator struct {
	mu     sync.RWMutex
	target admin.DrainTerminator
}

func NewDeferredDrainTerminator() *DeferredDrainTerminator {
	return &DeferredDrainTerminator{}
}

func (t *DeferredDrainTerminator) Set(target admin.DrainTerminator) error {
	if target == nil {
		return errors.New("drain terminator is required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.target != nil {
		return errors.New("drain terminator is already configured")
	}
	t.target = target
	return nil
}

func (t *DeferredDrainTerminator) ForceDrain(ctx context.Context, pool, batch string, kind ipv6resource.PoolKind, addresses []netip.Addr) error {
	t.mu.RLock()
	target := t.target
	t.mu.RUnlock()
	if target == nil {
		return errors.New("drain terminator is not configured")
	}
	return target.ForceDrain(ctx, pool, batch, kind, addresses)
}

var _ admin.DrainTerminator = (*DeferredDrainTerminator)(nil)
