package proxy

import (
	"errors"
	"net/netip"
	"slices"
	"testing"
)

type countingCloser struct {
	closed int
	err    error
}

func (c *countingCloser) Close() error {
	c.closed++
	return c.err
}

func TestSourcePoolSelectsCurrentAddressesRoundRobin(t *testing.T) {
	a := netip.MustParseAddr("2001:4860:1::1")
	b := netip.MustParseAddr("2001:4860:1::2")
	pool, err := NewSourcePool([]netip.Addr{a, b}, nil)
	if err != nil {
		t.Fatal(err)
	}

	first, err := pool.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	second, err := pool.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	third, err := pool.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if first.Address() != a || second.Address() != b || third.Address() != a {
		t.Fatalf("round robin = %s, %s, %s", first.Address(), second.Address(), third.Address())
	}
	first.Release()
	second.Release()
	third.Release()
}

func TestSourcePoolReplaceDrainsOnlyAddressesWithActiveLeases(t *testing.T) {
	a := netip.MustParseAddr("2001:4860:1::1")
	b := netip.MustParseAddr("2001:4860:1::2")
	c := netip.MustParseAddr("2001:4860:1::3")
	drained := make([]netip.Addr, 0, 2)
	pool, err := NewSourcePool([]netip.Addr{a, b}, func(address netip.Addr) {
		drained = append(drained, address)
	})
	if err != nil {
		t.Fatal(err)
	}

	lease, err := pool.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if lease.Address() != a {
		t.Fatalf("lease address = %s, want %s", lease.Address(), a)
	}
	if err := pool.Replace([]netip.Addr{c}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(drained, []netip.Addr{b}) {
		t.Fatalf("immediately drained = %v, want [%s]", drained, b)
	}
	status := pool.Draining()
	if len(status) != 1 || status[a] != 1 {
		t.Fatalf("Draining() = %v, want %s with one lease", status, a)
	}

	lease.Release()
	lease.Release()
	if !slices.Equal(drained, []netip.Addr{b, a}) {
		t.Fatalf("drained after release = %v, want [%s %s]", drained, b, a)
	}
	if len(pool.Draining()) != 0 {
		t.Fatalf("draining remains after final release: %v", pool.Draining())
	}
	next, err := pool.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if next.Address() != c {
		t.Fatalf("new lease address = %s, want %s", next.Address(), c)
	}
	next.Release()
}

func TestSourcePoolValidatesAndOwnsItsAddressSlices(t *testing.T) {
	a := netip.MustParseAddr("2001:4860:1::1")
	b := netip.MustParseAddr("2001:4860:1::2")
	addresses := []netip.Addr{a}
	pool, err := NewSourcePool(addresses, nil)
	if err != nil {
		t.Fatal(err)
	}
	addresses[0] = b
	lease, err := pool.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if lease.Address() != a {
		t.Fatal("pool aliases constructor input")
	}
	lease.Release()

	bad := [][]netip.Addr{
		nil,
		{netip.MustParseAddr("192.0.2.1")},
		{netip.MustParseAddr("::ffff:192.0.2.1")},
		{a, a},
	}
	for _, candidate := range bad {
		if _, err := NewSourcePool(candidate, nil); err == nil {
			t.Fatalf("NewSourcePool(%v) error = nil", candidate)
		}
	}
}

func TestSourcePoolForceDrainClosesEveryLeaseAndCompletesOnce(t *testing.T) {
	a := netip.MustParseAddr("2001:4860:1::1")
	b := netip.MustParseAddr("2001:4860:1::2")
	drained := make([]netip.Addr, 0, 1)
	pool, err := NewSourcePool([]netip.Addr{a}, func(address netip.Addr) {
		drained = append(drained, address)
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := pool.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	second, err := pool.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	firstConn := &countingCloser{}
	secondConn := &countingCloser{err: errors.New("close failed")}
	if err := first.Attach(firstConn); err != nil {
		t.Fatal(err)
	}
	if err := second.Attach(secondConn); err != nil {
		t.Fatal(err)
	}
	if err := pool.Replace([]netip.Addr{b}); err != nil {
		t.Fatal(err)
	}

	err = pool.ForceDrain(a)
	if err == nil || !errors.Is(err, secondConn.err) {
		t.Fatalf("ForceDrain() error = %v, want close error", err)
	}
	if firstConn.closed != 1 || secondConn.closed != 1 {
		t.Fatalf("closed counts = %d, %d, want 1, 1", firstConn.closed, secondConn.closed)
	}
	if !slices.Equal(drained, []netip.Addr{a}) {
		t.Fatalf("drained callbacks = %v, want [%s]", drained, a)
	}
	if len(pool.Draining()) != 0 {
		t.Fatalf("draining remains after force: %v", pool.Draining())
	}
	first.Release()
	second.Release()
	if firstConn.closed != 1 || secondConn.closed != 1 || len(drained) != 1 {
		t.Fatal("releasing forced leases was not idempotent")
	}
}

func TestSourcePoolForceDrainRejectsCurrentAndUnknownAddresses(t *testing.T) {
	a := netip.MustParseAddr("2001:4860:1::1")
	pool, err := NewSourcePool([]netip.Addr{a}, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, address := range []netip.Addr{a, netip.MustParseAddr("2001:4860:1::2"), {}} {
		if err := pool.ForceDrain(address); !errors.Is(err, ErrSourceNotDraining) {
			t.Fatalf("ForceDrain(%s) error = %v, want ErrSourceNotDraining", address, err)
		}
	}
}

func TestSourceLeaseAttachAfterForceClosesConnection(t *testing.T) {
	a := netip.MustParseAddr("2001:4860:1::1")
	b := netip.MustParseAddr("2001:4860:1::2")
	pool, err := NewSourcePool([]netip.Addr{a}, nil)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := pool.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Replace([]netip.Addr{b}); err != nil {
		t.Fatal(err)
	}
	if err := pool.ForceDrain(a); err != nil {
		t.Fatal(err)
	}
	connection := &countingCloser{}
	if err := lease.Attach(connection); !errors.Is(err, ErrSourceDrainForced) {
		t.Fatalf("Attach() error = %v, want ErrSourceDrainForced", err)
	}
	if connection.closed != 1 {
		t.Fatalf("connection Close calls = %d, want 1", connection.closed)
	}
	if err := lease.Attach(nil); err == nil {
		t.Fatal("Attach(nil) error = nil")
	}
}
