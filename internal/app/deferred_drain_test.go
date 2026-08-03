package app

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
)

type recordingDrainTerminator struct {
	pool      string
	batch     string
	kind      ipv6resource.PoolKind
	addresses []netip.Addr
	err       error
}

func (t *recordingDrainTerminator) ForceDrain(_ context.Context, pool, batch string, kind ipv6resource.PoolKind, addresses []netip.Addr) error {
	t.pool = pool
	t.batch = batch
	t.kind = kind
	t.addresses = append([]netip.Addr(nil), addresses...)
	return t.err
}

func TestDeferredDrainTerminatorRequiresAndDelegatesToOneTarget(t *testing.T) {
	deferred := NewDeferredDrainTerminator()
	address := netip.MustParseAddr("2001:4860::1")
	if err := deferred.ForceDrain(context.Background(), "pool", "drain-1", ipv6resource.PoolInbound, []netip.Addr{address}); err == nil {
		t.Fatal("ForceDrain before Set succeeded")
	}
	if err := deferred.Set(nil); err == nil {
		t.Fatal("Set(nil) succeeded")
	}

	expected := errors.New("terminate failed")
	target := &recordingDrainTerminator{err: expected}
	if err := deferred.Set(target); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := deferred.Set(&recordingDrainTerminator{}); err == nil {
		t.Fatal("second Set succeeded")
	}
	err := deferred.ForceDrain(context.Background(), "pool", "drain-1", ipv6resource.PoolInbound, []netip.Addr{address})
	if !errors.Is(err, expected) {
		t.Fatalf("ForceDrain() error = %v, want wrapped target error", err)
	}
	if target.pool != "pool" || target.batch != "drain-1" || target.kind != ipv6resource.PoolInbound || len(target.addresses) != 1 || target.addresses[0] != address {
		t.Fatalf("delegated request = %#v", target)
	}
}
