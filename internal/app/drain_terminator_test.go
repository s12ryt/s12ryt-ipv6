package app

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
)

type recordingOutboundDrainer struct {
	pool      string
	addresses []netip.Addr
	err       error
}

func (d *recordingOutboundDrainer) ForceDrain(pool string, addresses []netip.Addr) error {
	d.pool = pool
	d.addresses = append([]netip.Addr(nil), addresses...)
	return d.err
}

type recordingInboundDrainer struct {
	pool      string
	addresses []netip.Addr
	err       error
}

func (d *recordingInboundDrainer) ForceDrainInbound(_ context.Context, pool string, addresses []netip.Addr) error {
	d.pool = pool
	d.addresses = append([]netip.Addr(nil), addresses...)
	return d.err
}

func TestRuntimeDrainTerminatorDispatchesByPoolKind(t *testing.T) {
	outbound := &recordingOutboundDrainer{}
	inbound := &recordingInboundDrainer{}
	terminator, err := NewRuntimeDrainTerminator(outbound, inbound)
	if err != nil {
		t.Fatal(err)
	}
	addresses := []netip.Addr{netip.MustParseAddr("2001:4860:1::1")}
	if err := terminator.ForceDrain(context.Background(), "inbound", "drain-1", ipv6resource.PoolInbound, addresses); err != nil {
		t.Fatal(err)
	}
	if inbound.pool != "inbound" || !reflect.DeepEqual(inbound.addresses, addresses) || outbound.pool != "" {
		t.Fatalf("inbound dispatch = inbound:%q/%v outbound:%q", inbound.pool, inbound.addresses, outbound.pool)
	}
	if err := terminator.ForceDrain(context.Background(), "shared", "drain-2", ipv6resource.PoolSharedOutbound, addresses); err != nil {
		t.Fatal(err)
	}
	if outbound.pool != "shared" || !reflect.DeepEqual(outbound.addresses, addresses) {
		t.Fatalf("outbound dispatch = %q/%v", outbound.pool, outbound.addresses)
	}
	if err := terminator.ForceDrain(context.Background(), "dedicated", "drain-3", ipv6resource.PoolDedicatedOutbound, addresses); err != nil {
		t.Fatal(err)
	}
	if outbound.pool != "dedicated" {
		t.Fatalf("dedicated dispatch pool = %q", outbound.pool)
	}
}

func TestRuntimeDrainTerminatorValidatesAndReturnsLayerErrors(t *testing.T) {
	forcedErr := errors.New("close failed")
	outbound := &recordingOutboundDrainer{err: forcedErr}
	inbound := &recordingInboundDrainer{}
	terminator, err := NewRuntimeDrainTerminator(outbound, inbound)
	if err != nil {
		t.Fatal(err)
	}
	address := netip.MustParseAddr("2001:4860:1::1")
	if err := terminator.ForceDrain(context.Background(), "shared", "drain-1", ipv6resource.PoolSharedOutbound, []netip.Addr{address}); !errors.Is(err, forcedErr) {
		t.Fatalf("ForceDrain() error = %v, want layer error", err)
	}
	invalid := []struct {
		pool      string
		batch     string
		kind      ipv6resource.PoolKind
		addresses []netip.Addr
	}{
		{"", "drain-1", ipv6resource.PoolInbound, []netip.Addr{address}},
		{"inbound", "", ipv6resource.PoolInbound, []netip.Addr{address}},
		{"inbound", "drain-1", ipv6resource.PoolInbound, nil},
		{"inbound", "drain-1", "unknown", []netip.Addr{address}},
		{"inbound", "drain-1", ipv6resource.PoolInbound, []netip.Addr{netip.MustParseAddr("192.0.2.1")}},
	}
	for _, candidate := range invalid {
		if err := terminator.ForceDrain(context.Background(), candidate.pool, candidate.batch, candidate.kind, candidate.addresses); err == nil {
			t.Fatalf("ForceDrain(%#v) error = nil", candidate)
		}
	}
	if _, err := NewRuntimeDrainTerminator(nil, inbound); err == nil {
		t.Fatal("NewRuntimeDrainTerminator(nil outbound) error = nil")
	}
	if _, err := NewRuntimeDrainTerminator(outbound, nil); err == nil {
		t.Fatal("NewRuntimeDrainTerminator(nil inbound) error = nil")
	}
}
