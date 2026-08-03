package admin

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/config"
	"github.com/s12ryt/s12ryt-ipv6/internal/dns64"
	"github.com/s12ryt/s12ryt-ipv6/internal/eventlog"
	"github.com/s12ryt/s12ryt-ipv6/internal/firewall"
	"github.com/s12ryt/s12ryt-ipv6/internal/stats"
)

type fakeOperationsLogger struct {
	events     []eventlog.Event
	tail       []eventlog.Event
	clearActor string
	err        error
}

func (l *fakeOperationsLogger) Tail(eventlog.Filter, int) ([]eventlog.Event, error) {
	return append([]eventlog.Event(nil), l.tail...), l.err
}
func (l *fakeOperationsLogger) Clear(actor string) error {
	if l.err != nil {
		return l.err
	}
	l.clearActor = actor
	return nil
}
func (l *fakeOperationsLogger) Write(event eventlog.Event) error {
	if l.err != nil {
		return l.err
	}
	l.events = append(l.events, event)
	return nil
}

type fakeNAT64Operations struct {
	status dns64.NAT64Status
	manual netip.Prefix
	checks int
	err    error
}

func (n *fakeNAT64Operations) Status() dns64.NAT64Status  { return n.status }
func (n *fakeNAT64Operations) ManualPrefix() netip.Prefix { return n.manual }
func (n *fakeNAT64Operations) SetManualPrefix(prefix netip.Prefix) error {
	if n.err != nil {
		return n.err
	}
	n.manual = prefix
	return nil
}
func (n *fakeNAT64Operations) Check(context.Context) dns64.NAT64Status {
	n.checks++
	n.status = dns64.NAT64Status{
		State: dns64.NAT64Healthy, Prefix: n.manual, Source: "manual", Manual: n.manual.IsValid(),
	}
	return n.status
}

type fakeFirewallDiagnoser struct {
	diagnosis firewall.Diagnosis
	err       error
}

func (f *fakeFirewallDiagnoser) Diagnose(context.Context) (firewall.Diagnosis, error) {
	return f.diagnosis, f.err
}

type fakeConnectivityTester struct {
	checks []ConnectivityCheck
	calls  int
	err    error
}

func (t *fakeConnectivityTester) Test(context.Context) ([]ConnectivityCheck, error) {
	t.calls++
	return append([]ConnectivityCheck(nil), t.checks...), t.err
}

func TestOperationsCoordinatorConnectsRuntimeServices(t *testing.T) {
	queryer := &fakeAdminDNSQueryer{}
	resolver, err := dns64.NewResolver([]dns64.Endpoint{{
		Name: "old", Address: netip.MustParseAddr("2606:4700:4700::64"), Port: 853, ServerName: "cloudflare-dns.com",
	}}, queryer, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	registry := stats.NewRegistry()
	registry.TCPOpened("edge")
	registry.TCPClosed("edge", 10, 20, true)
	logger := &fakeOperationsLogger{tail: []eventlog.Event{{Kind: eventlog.KindSystem, Action: "ready", Success: true}}}
	nat64 := &fakeNAT64Operations{status: dns64.NAT64Status{State: dns64.NAT64Degraded, Error: "unavailable"}}
	fw := &fakeFirewallDiagnoser{diagnosis: firewall.Diagnosis{Degraded: true, Blockers: []string{"foreign drop"}}}
	connectivity := &fakeConnectivityTester{checks: []ConnectivityCheck{{Name: "dot", Kind: "dot", Success: true}}}
	var savedStats stats.Snapshot
	var savedResolvers []config.Resolver
	var savedNAT64 netip.Prefix
	service, err := NewOperationsCoordinator(OperationsCoordinatorOptions{
		Logs: logger, Stats: registry, SaveStats: func(snapshot stats.Snapshot) error {
			savedStats = snapshot
			return nil
		},
		NAT64: nat64, SaveNAT64: func(prefix netip.Prefix) error {
			savedNAT64 = prefix
			return nil
		}, Firewall: fw, Resolver: resolver,
		Resolvers: []config.Resolver{{Name: "old", Address: "2606:4700:4700::64", Port: 853, ServerName: "cloudflare-dns.com", Enabled: true}},
		SaveResolvers: func(values []config.Resolver) error {
			savedResolvers = append([]config.Resolver(nil), values...)
			return nil
		},
		Connectivity: connectivity, BaseHealth: func() HealthState { return HealthHealthy },
		DiagnosisTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	overview := service.Overview()
	if overview.Health != HealthDegraded || !overview.Firewall.Degraded || len(overview.Resolvers) != 1 {
		t.Fatalf("Overview() = %#v", overview)
	}
	if service.Statistics().Nodes["edge"].TotalConnections != 1 {
		t.Fatalf("Statistics() = %#v", service.Statistics())
	}
	logs, err := service.TailLogs(eventlog.Filter{}, 10)
	if err != nil || len(logs) != 1 || logs[0].Action != "ready" {
		t.Fatalf("TailLogs() = %#v, %v", logs, err)
	}
	if err := service.ResetStatistics("edge"); err != nil {
		t.Fatal(err)
	}
	if savedStats.Nodes["edge"].TotalConnections != 0 || registry.Snapshot().Nodes["edge"].TotalConnections != 0 {
		t.Fatalf("reset snapshots: saved=%#v live=%#v", savedStats, registry.Snapshot())
	}
	if len(logger.events) != 1 || logger.events[0].Kind != eventlog.KindAudit || logger.events[0].Action != "statistics.reset" {
		t.Fatalf("audit events = %#v", logger.events)
	}
	if err := service.ClearLogs("admin"); err != nil || logger.clearActor != "admin" {
		t.Fatalf("ClearLogs() actor=%q error=%v", logger.clearActor, err)
	}

	prefix := netip.MustParsePrefix("2001:4860:64::/96")
	status, err := service.SetManualNAT64(context.Background(), prefix)
	if err != nil || nat64.checks != 1 || status.Prefix != prefix || savedNAT64 != prefix {
		t.Fatalf("SetManualNAT64() = %#v, %v checks=%d saved=%s", status, err, nat64.checks, savedNAT64)
	}

	replacement := []config.Resolver{
		{Name: "disabled", Address: "2001:db8::1", Port: 853, ServerName: "disabled.example", Enabled: false},
		{Name: "new", Address: "2001:4860:4860::6464", Port: 853, ServerName: "dns.google", Enabled: true},
	}
	if err := service.UpdateResolvers(context.Background(), replacement); err != nil {
		t.Fatal(err)
	}
	if len(savedResolvers) != 2 || len(resolver.Endpoints()) != 1 || resolver.Endpoints()[0].Name != "new" {
		t.Fatalf("resolver update: saved=%#v runtime=%#v", savedResolvers, resolver.Endpoints())
	}
	checks, err := service.TestConnectivity(context.Background())
	if err != nil || connectivity.calls != 1 || len(checks) != 1 {
		t.Fatalf("TestConnectivity() = %#v, %v calls=%d", checks, err, connectivity.calls)
	}
}

func TestOperationsCoordinatorRollsBackRuntimeResolverWhenPersistenceFails(t *testing.T) {
	resolver, err := dns64.NewResolver([]dns64.Endpoint{{
		Name: "old", Address: netip.MustParseAddr("2606:4700:4700::64"), Port: 853, ServerName: "cloudflare-dns.com",
	}}, &fakeAdminDNSQueryer{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	service := newTestOperationsCoordinator(t, resolver, func([]config.Resolver) error { return errors.New("disk contains secret detail") })
	replacement := []config.Resolver{{Name: "new", Address: "2001:4860:4860::6464", Port: 853, ServerName: "dns.google", Enabled: true}}
	if err := service.UpdateResolvers(context.Background(), replacement); err == nil {
		t.Fatal("UpdateResolvers() error = nil")
	}
	if got := resolver.Endpoints(); len(got) != 1 || got[0].Name != "old" {
		t.Fatalf("runtime endpoints after rollback = %#v", got)
	}
	if got := service.Overview().Resolvers; len(got) != 1 || got[0].Name != "old" {
		t.Fatalf("configured resolvers after rollback = %#v", got)
	}
}

func TestOperationsCoordinatorRollsBackNAT64WhenPersistenceFails(t *testing.T) {
	resolver, err := dns64.NewResolver([]dns64.Endpoint{{
		Name: "old", Address: netip.MustParseAddr("2606:4700:4700::64"), Port: 853, ServerName: "cloudflare-dns.com",
	}}, &fakeAdminDNSQueryer{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	oldPrefix := netip.MustParsePrefix("64:ff9b::/96")
	nat64 := &fakeNAT64Operations{
		manual: oldPrefix,
		status: dns64.NAT64Status{State: dns64.NAT64Healthy, Prefix: oldPrefix, Source: "manual", Manual: true},
	}
	logger := &fakeOperationsLogger{}
	service, err := NewOperationsCoordinator(OperationsCoordinatorOptions{
		Logs: logger, Stats: stats.NewRegistry(), SaveStats: func(stats.Snapshot) error { return nil },
		NAT64: nat64, SaveNAT64: func(netip.Prefix) error { return errors.New("disk contains secret detail") },
		Firewall: &fakeFirewallDiagnoser{}, Resolver: resolver,
		Resolvers:     []config.Resolver{{Name: "old", Address: "2606:4700:4700::64", Port: 853, ServerName: "cloudflare-dns.com", Enabled: true}},
		SaveResolvers: func([]config.Resolver) error { return nil }, Connectivity: &fakeConnectivityTester{},
		BaseHealth: func() HealthState { return HealthHealthy }, DiagnosisTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	replacement := netip.MustParsePrefix("2001:4860:64::/96")
	if _, err := service.SetManualNAT64(context.Background(), replacement); err == nil {
		t.Fatal("SetManualNAT64() error = nil")
	}
	if nat64.manual != oldPrefix || nat64.status.Prefix != oldPrefix || nat64.checks != 1 {
		t.Fatalf("NAT64 after rollback: manual=%s status=%#v checks=%d", nat64.manual, nat64.status, nat64.checks)
	}
	if len(logger.events) != 0 {
		t.Fatalf("audit events after failed update = %#v", logger.events)
	}
}

func TestOperationsCoordinatorRejectsMissingDependencies(t *testing.T) {
	if _, err := NewOperationsCoordinator(OperationsCoordinatorOptions{}); err == nil {
		t.Fatal("NewOperationsCoordinator(empty) error = nil")
	}
}

type fakeAdminDNSQueryer struct{}

func (*fakeAdminDNSQueryer) Query(context.Context, dns64.Endpoint, string, dns64.RecordType) (dns64.QueryResult, error) {
	return dns64.QueryResult{}, nil
}

func newTestOperationsCoordinator(t *testing.T, resolver *dns64.Resolver, saveResolvers func([]config.Resolver) error) *OperationsCoordinator {
	t.Helper()
	service, err := NewOperationsCoordinator(OperationsCoordinatorOptions{
		Logs: &fakeOperationsLogger{}, Stats: stats.NewRegistry(), SaveStats: func(stats.Snapshot) error { return nil },
		NAT64: &fakeNAT64Operations{status: dns64.NAT64Status{State: dns64.NAT64Healthy}}, SaveNAT64: func(netip.Prefix) error { return nil },
		Firewall: &fakeFirewallDiagnoser{}, Resolver: resolver,
		Resolvers:     []config.Resolver{{Name: "old", Address: "2606:4700:4700::64", Port: 853, ServerName: "cloudflare-dns.com", Enabled: true}},
		SaveResolvers: saveResolvers, Connectivity: &fakeConnectivityTester{},
		BaseHealth: func() HealthState { return HealthHealthy }, DiagnosisTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}
