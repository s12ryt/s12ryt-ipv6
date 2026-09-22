package proxy

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/things-go/go-socks5/statute"
)

// countingUDPDialer records per-destination dial attempts so tests can assert
// that a hostile or malfunctioning client cannot trigger unbounded dials.
type countingUDPDialer struct {
	mu    sync.Mutex
	dials map[string]int
	fail  map[string]bool
	open  []net.Conn
}

func newCountingUDPDialer() *countingUDPDialer {
	return &countingUDPDialer{dials: make(map[string]int), fail: make(map[string]bool)}
}

func (d *countingUDPDialer) Dial(_ context.Context, network, host string, port uint16) (net.Conn, DialMetadata, error) {
	key := udpDialKey(network, host, port)
	d.mu.Lock()
	d.dials[key]++
	shouldFail := d.fail[key]
	d.mu.Unlock()
	if shouldFail {
		return nil, DialMetadata{}, errors.New("scripted dial failure")
	}
	client, server := net.Pipe()
	d.mu.Lock()
	d.open = append(d.open, server)
	d.mu.Unlock()
	return client, DialMetadata{Source: netip.MustParseAddr("2001:db8::1")}, nil
}

func (d *countingUDPDialer) setFailure(network, host string, port uint16, fail bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fail[udpDialKey(network, host, port)] = fail
}

func (d *countingUDPDialer) dialCount(network, host string, port uint16) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dials[udpDialKey(network, host, port)]
}

func (d *countingUDPDialer) closeAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, conn := range d.open {
		_ = conn.Close()
	}
	d.open = nil
}

func udpDialKey(network, host string, port uint16) string {
	return network + "|" + host + "|" + strconv.Itoa(int(port))
}

func newTestUDPAssociation(dialer ProxyDialer) *udpAssociation {
	return &udpAssociation{
		dialer:      dialer,
		idleTimeout: time.Minute,
		mappings:    make(map[string]*udpMapping),
	}
}

func testUDPClientAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 34567}
}

func testUDPDestination(host string, port uint16) statute.AddrSpec {
	return statute.AddrSpec{IP: net.ParseIP(host).To4(), Port: int(port)}
}

func testUDPKey(host string, port uint16) string {
	return "client\x00" + host + "\x00" + strconv.Itoa(int(port))
}

// TestUDPAssociationCachesDestinationDialFailures verifies that a destination
// which failed to dial is not dialed again for every subsequent packet.
func TestUDPAssociationCachesDestinationDialFailures(t *testing.T) {
	dialer := newCountingUDPDialer()
	dialer.setFailure("udp", "192.0.2.1", 53, true)
	association := newTestUDPAssociation(dialer)
	defer association.closeMappings()

	ctx := context.Background()
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := association.mapping(
			ctx, testUDPKey("192.0.2.1", 53), testUDPClientAddr(),
			testUDPDestination("192.0.2.1", 53), "192.0.2.1", 53,
		); err == nil {
			t.Fatalf("attempt %d: mapping() error = nil, want dial failure", attempt)
		}
	}
	if got := dialer.dialCount("udp", "192.0.2.1", 53); got != 1 {
		t.Fatalf("dial attempts = %d, want 1 (failed destinations must be negatively cached)", got)
	}
}

// TestUDPAssociationRetriesDestinationAfterFailureTTL verifies the negative
// cache expires so a destination can recover.
func TestUDPAssociationRetriesDestinationAfterFailureTTL(t *testing.T) {
	restore := destinationDialFailureTTL
	destinationDialFailureTTL = 20 * time.Millisecond
	defer func() { destinationDialFailureTTL = restore }()

	dialer := newCountingUDPDialer()
	dialer.setFailure("udp", "192.0.2.2", 53, true)
	association := newTestUDPAssociation(dialer)
	defer association.closeMappings()
	defer dialer.closeAll()

	ctx := context.Background()
	if _, err := association.mapping(
		ctx, testUDPKey("192.0.2.2", 53), testUDPClientAddr(),
		testUDPDestination("192.0.2.2", 53), "192.0.2.2", 53,
	); err == nil {
		t.Fatal("first mapping() error = nil, want dial failure")
	}
	if _, err := association.mapping(
		ctx, testUDPKey("192.0.2.2", 53), testUDPClientAddr(),
		testUDPDestination("192.0.2.2", 53), "192.0.2.2", 53,
	); err == nil {
		t.Fatal("cached mapping() error = nil, want cached failure")
	}
	if got := dialer.dialCount("udp", "192.0.2.2", 53); got != 1 {
		t.Fatalf("dial attempts within TTL = %d, want 1", got)
	}

	dialer.setFailure("udp", "192.0.2.2", 53, false)
	time.Sleep(40 * time.Millisecond)
	if _, err := association.mapping(
		ctx, testUDPKey("192.0.2.2", 53), testUDPClientAddr(),
		testUDPDestination("192.0.2.2", 53), "192.0.2.2", 53,
	); err != nil {
		t.Fatalf("mapping() after TTL error = %v, want retry", err)
	}
	if got := dialer.dialCount("udp", "192.0.2.2", 53); got != 2 {
		t.Fatalf("dial attempts after TTL = %d, want 2", got)
	}
}

// TestUDPAssociationLimitsDestinationMappings verifies that a single association
// cannot grow its destination mapping table without bound.
func TestUDPAssociationLimitsDestinationMappings(t *testing.T) {
	restore := maxDestinationMappingsPerAssociation
	maxDestinationMappingsPerAssociation = 2
	defer func() { maxDestinationMappingsPerAssociation = restore }()

	dialer := newCountingUDPDialer()
	association := newTestUDPAssociation(dialer)
	defer association.closeMappings()
	defer dialer.closeAll()

	ctx := context.Background()
	for index := 1; index <= 2; index++ {
		host := "192.0.2." + strconv.Itoa(index)
		if _, err := association.mapping(
			ctx, testUDPKey(host, 53), testUDPClientAddr(),
			testUDPDestination(host, 53), host, 53,
		); err != nil {
			t.Fatalf("mapping %s error = %v, want success", host, err)
		}
	}
	if _, err := association.mapping(
		ctx, testUDPKey("192.0.2.3", 53), testUDPClientAddr(),
		testUDPDestination("192.0.2.3", 53), "192.0.2.3", 53,
	); err == nil {
		t.Fatal("mapping beyond destination limit error = nil, want rejection")
	}
	if got := dialer.dialCount("udp", "192.0.2.3", 53); got != 0 {
		t.Fatalf("dial attempts for rejected destination = %d, want 0", got)
	}
}

// TestUDPAssociationBoundsDestinationFailureCache verifies that spraying datagrams
// at many distinct unreachable destinations cannot grow the negative dial cache
// without bound. The mapping table stays empty because every dial fails, so the
// mapping limit alone does not bound this map; the cache needs its own bound.
func TestUDPAssociationBoundsDestinationFailureCache(t *testing.T) {
	restore := maxDestinationFailuresPerAssociation
	maxDestinationFailuresPerAssociation = 2
	defer func() { maxDestinationFailuresPerAssociation = restore }()

	dialer := newCountingUDPDialer()
	association := newTestUDPAssociation(dialer)
	defer association.closeMappings()

	ctx := context.Background()
	for index := 1; index <= 8; index++ {
		host := "192.0.2." + strconv.Itoa(index)
		dialer.setFailure("udp", host, 53, true)
		if _, err := association.mapping(
			ctx, testUDPKey(host, 53), testUDPClientAddr(),
			testUDPDestination(host, 53), host, 53,
		); err == nil {
			t.Fatalf("mapping %s error = nil, want dial failure", host)
		}
	}

	association.mu.Lock()
	cached := len(association.failures)
	association.mu.Unlock()
	if cached > maxDestinationFailuresPerAssociation {
		t.Fatalf("cached destination failures = %d, want at most %d", cached, maxDestinationFailuresPerAssociation)
	}
}

// TestUDPAssociationKeepsDialingWhenFailureCacheIsFull verifies the failure cache is
// best effort: once full, an unrelated destination must still be dialed instead of
// being refused because other destinations failed.
func TestUDPAssociationKeepsDialingWhenFailureCacheIsFull(t *testing.T) {
	restore := maxDestinationFailuresPerAssociation
	maxDestinationFailuresPerAssociation = 1
	defer func() { maxDestinationFailuresPerAssociation = restore }()

	dialer := newCountingUDPDialer()
	dialer.setFailure("udp", "192.0.2.1", 53, true)
	association := newTestUDPAssociation(dialer)
	defer association.closeMappings()
	defer dialer.closeAll()

	ctx := context.Background()
	if _, err := association.mapping(
		ctx, testUDPKey("192.0.2.1", 53), testUDPClientAddr(),
		testUDPDestination("192.0.2.1", 53), "192.0.2.1", 53,
	); err == nil {
		t.Fatal("mapping of failing destination error = nil, want dial failure")
	}

	if _, err := association.mapping(
		ctx, testUDPKey("192.0.2.2", 53), testUDPClientAddr(),
		testUDPDestination("192.0.2.2", 53), "192.0.2.2", 53,
	); err != nil {
		t.Fatalf("mapping of healthy destination error = %v, want success", err)
	}
	if got := dialer.dialCount("udp", "192.0.2.2", 53); got != 1 {
		t.Fatalf("dial attempts for healthy destination = %d, want 1", got)
	}
}
