package app

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/dns64"
)

func TestWatchdogProbeDestinationsUseTwelveVerifiedIPv6Sites(t *testing.T) {
	destinations := watchdogProbeDestinations(false)
	want := []string{
		"one.one.one.one:443",
		"dns.google:443",
		"www.wikipedia.org:443",
		"www.facebook.com:443",
		"www.microsoft.com:443",
		"www.debian.org:443",
		"www.kernel.org:443",
		"www.quad9.net:443",
		"www.he.net:443",
		"v6.ident.me:443",
		"api6.ipify.org:443",
		"ipv6.google.com:443",
	}
	if !slices.Equal(destinations, want) {
		t.Fatalf("native probe destinations = %v, want %v", destinations, want)
	}
	seen := make(map[string]struct{}, len(destinations))
	for _, destination := range destinations {
		host, port, err := net.SplitHostPort(destination)
		if err != nil {
			t.Fatalf("SplitHostPort(%q) error = %v", destination, err)
		}
		if _, err := netip.ParseAddr(host); err == nil {
			t.Fatalf("native probe host = %q, want a hostname with AAAA records", host)
		}
		if port != "443" {
			t.Fatalf("native probe port = %q, want 443", port)
		}
		if _, exists := seen[host]; exists {
			t.Fatalf("duplicate native probe host %q", host)
		}
		seen[host] = struct{}{}
	}
}

func TestWatchdogProbeDestinationsAddDNS64AndNAT64WhenEnabled(t *testing.T) {
	disabled := watchdogProbeDestinations(false)
	enabled := watchdogProbeDestinations(true)
	if slices.Contains(disabled, dns64ProbeDestination) || slices.Contains(disabled, nat64ProbeDestination) {
		t.Fatalf("disabled destinations = %v, want no DNS64/NAT64 probes", disabled)
	}
	if !slices.Contains(enabled, dns64ProbeDestination) || !slices.Contains(enabled, nat64ProbeDestination) {
		t.Fatalf("enabled destinations = %v, want %q and %q", enabled, dns64ProbeDestination, nat64ProbeDestination)
	}
	if len(enabled) != len(disabled)+2 {
		t.Fatalf("enabled destination count = %d, want %d", len(enabled), len(disabled)+2)
	}

	dns64Host, _, err := net.SplitHostPort(dns64ProbeDestination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := netip.ParseAddr(dns64Host); err == nil {
		t.Fatalf("DNS64 probe host = %q, want an IPv4-only hostname", dns64Host)
	}
	nat64Host, _, err := net.SplitHostPort(nat64ProbeDestination)
	if err != nil {
		t.Fatal(err)
	}
	nat64Address, err := netip.ParseAddr(nat64Host)
	if err != nil || !nat64Address.Is4() {
		t.Fatalf("NAT64 probe host = %q, want an IPv4 literal", nat64Host)
	}
}

func TestProxyWatchdogProbeRunsEveryDestinationAndAcceptsPartialSuccess(t *testing.T) {
	var mu sync.Mutex
	var attempted []string
	probe := newProxyWatchdogProbe(
		func() bool { return true },
		func(_ context.Context, _ ProbeTarget, destination string) error {
			mu.Lock()
			attempted = append(attempted, destination)
			mu.Unlock()
			if destination == "dns.google:443" {
				return nil
			}
			return errors.New("unavailable")
		},
	)
	if err := probe(context.Background(), ProbeTarget{Node: "node-1"}); err != nil {
		t.Fatalf("aggregate probe error = %v, want success when one destination succeeds", err)
	}

	want := watchdogProbeDestinations(true)
	sort.Strings(attempted)
	sort.Strings(want)
	if !slices.Equal(attempted, want) {
		t.Fatalf("attempted destinations = %v, want %v", attempted, want)
	}
}

func TestProxyWatchdogProbeFailsOnlyWhenEveryDestinationFails(t *testing.T) {
	probe := newProxyWatchdogProbe(
		func() bool { return false },
		func(_ context.Context, _ ProbeTarget, destination string) error {
			return errors.New("failed " + destination)
		},
	)
	err := probe(context.Background(), ProbeTarget{Node: "node-1"})
	if err == nil {
		t.Fatal("aggregate probe error = nil, want failure when every destination fails")
	}
	for _, destination := range watchdogProbeDestinations(false) {
		if !strings.Contains(err.Error(), destination) {
			t.Fatalf("aggregate probe error = %q, want destination %q", err, destination)
		}
	}
}

func TestProxyWatchdogProbeDoesNotCompeteForNodeConnectionLimit(t *testing.T) {
	var calls atomic.Int32
	var active atomic.Int32
	var maximum atomic.Int32
	secondStarted := make(chan struct{})
	probe := newProxyWatchdogProbe(
		func() bool { return false },
		func(_ context.Context, _ ProbeTarget, _ string) error {
			call := calls.Add(1)
			current := active.Add(1)
			defer active.Add(-1)
			for previous := maximum.Load(); current > previous; previous = maximum.Load() {
				if maximum.CompareAndSwap(previous, current) {
					break
				}
			}
			if call == 1 {
				select {
				case <-secondStarted:
				case <-time.After(50 * time.Millisecond):
				}
			} else if call == 2 {
				close(secondStarted)
			}
			return nil
		},
	)
	if err := probe(context.Background(), ProbeTarget{Node: "node-1"}); err != nil {
		t.Fatal(err)
	}
	if got := maximum.Load(); got != 1 {
		t.Fatalf("concurrent destination probes = %d, want 1 to respect MaxTCP=1 nodes", got)
	}
}

func TestProxyWatchdogProbeSharesParentDeadlineAcrossDestinations(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	parentDeadline, _ := parent.Deadline()
	var deadlines []time.Time
	probe := newProxyWatchdogProbe(
		func() bool { return false },
		func(ctx context.Context, _ ProbeTarget, _ string) error {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("destination probe context has no deadline")
			}
			deadlines = append(deadlines, deadline)
			return errors.New("unavailable")
		},
	)
	if err := probe(parent, ProbeTarget{Node: "node-1"}); err == nil {
		t.Fatal("aggregate probe error = nil, want all destinations to fail")
	}
	if got, want := len(deadlines), len(watchdogProbeDestinations(false)); got != want {
		t.Fatalf("destination deadline count = %d, want %d", got, want)
	}
	if got := parentDeadline.Sub(deadlines[0]); got < time.Second {
		t.Fatalf("first destination received almost the full parent deadline (difference %s), want a fair share", got)
	}
}

func TestNAT64WatchdogProbeEnabledForManualOrHealthyRuntime(t *testing.T) {
	cases := []struct {
		name   string
		status dns64.NAT64Status
		want   bool
	}{
		{name: "unchecked automatic", status: dns64.NAT64Status{State: dns64.NAT64Degraded}},
		{name: "manual configured", status: dns64.NAT64Status{State: dns64.NAT64Degraded, Manual: true}, want: true},
		{name: "automatic healthy", status: dns64.NAT64Status{State: dns64.NAT64Healthy, Prefix: netip.MustParsePrefix("64:ff9b::/96")}, want: true},
		{name: "invalid healthy state", status: dns64.NAT64Status{State: dns64.NAT64Healthy}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nat64WatchdogProbeEnabled(tc.status); got != tc.want {
				t.Fatalf("nat64WatchdogProbeEnabled(%#v) = %t, want %t", tc.status, got, tc.want)
			}
		})
	}
}
