package admin

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/config"
	"github.com/s12ryt/s12ryt-ipv6/internal/dns64"
	"github.com/s12ryt/s12ryt-ipv6/internal/eventlog"
	"github.com/s12ryt/s12ryt-ipv6/internal/firewall"
	"github.com/s12ryt/s12ryt-ipv6/internal/stats"
)

type operationsLogger interface {
	Tail(eventlog.Filter, int) ([]eventlog.Event, error)
	Clear(string) error
	Write(eventlog.Event) error
}

type nat64Operations interface {
	Status() dns64.NAT64Status
	ManualPrefix() netip.Prefix
	SetManualPrefix(netip.Prefix) error
	Check(context.Context) dns64.NAT64Status
}

type firewallDiagnoser interface {
	Diagnose(context.Context) (firewall.Diagnosis, error)
}

type connectivityTester interface {
	Test(context.Context) ([]ConnectivityCheck, error)
}

type OperationsCoordinatorOptions struct {
	Logs             operationsLogger
	Stats            *stats.Registry
	SaveStats        func(stats.Snapshot) error
	NAT64            nat64Operations
	SaveNAT64        func(netip.Prefix) error
	Firewall         firewallDiagnoser
	Resolver         *dns64.Resolver
	Resolvers        []config.Resolver
	SaveResolvers    func([]config.Resolver) error
	Connectivity     connectivityTester
	BaseHealth       func() HealthState
	DiagnosisTimeout time.Duration
}

type OperationsCoordinator struct {
	mu               sync.RWMutex
	logs             operationsLogger
	stats            *stats.Registry
	saveStats        func(stats.Snapshot) error
	nat64            nat64Operations
	saveNAT64        func(netip.Prefix) error
	firewall         firewallDiagnoser
	resolver         *dns64.Resolver
	resolvers        []config.Resolver
	saveResolvers    func([]config.Resolver) error
	connectivity     connectivityTester
	baseHealth       func() HealthState
	diagnosisTimeout time.Duration
}

func NewOperationsCoordinator(options OperationsCoordinatorOptions) (*OperationsCoordinator, error) {
	if options.Logs == nil {
		return nil, errors.New("operations logger is required")
	}
	if options.Stats == nil || options.SaveStats == nil {
		return nil, errors.New("statistics registry and saver are required")
	}
	if options.NAT64 == nil || options.SaveNAT64 == nil || options.Firewall == nil {
		return nil, errors.New("network health dependencies are required")
	}
	if options.Resolver == nil || options.SaveResolvers == nil {
		return nil, errors.New("resolver runtime and saver are required")
	}
	if options.Connectivity == nil {
		return nil, errors.New("connectivity tester is required")
	}
	if options.BaseHealth == nil {
		return nil, errors.New("base health provider is required")
	}
	if options.DiagnosisTimeout <= 0 {
		return nil, errors.New("firewall diagnosis timeout must be positive")
	}
	if err := validateResolvers(options.Resolvers); err != nil {
		return nil, fmt.Errorf("validate configured resolvers: %w", err)
	}
	endpoints, err := enabledResolverEndpoints(options.Resolvers)
	if err != nil {
		return nil, err
	}
	if err := options.Resolver.UpdateEndpoints(endpoints); err != nil {
		return nil, fmt.Errorf("configure runtime resolvers: %w", err)
	}
	return &OperationsCoordinator{
		logs: options.Logs, stats: options.Stats, saveStats: options.SaveStats,
		nat64: options.NAT64, saveNAT64: options.SaveNAT64, firewall: options.Firewall, resolver: options.Resolver,
		resolvers: append([]config.Resolver(nil), options.Resolvers...), saveResolvers: options.SaveResolvers,
		connectivity: options.Connectivity, baseHealth: options.BaseHealth,
		diagnosisTimeout: options.DiagnosisTimeout,
	}, nil
}

func (c *OperationsCoordinator) Overview() OperationsSnapshot {
	ctx, cancel := context.WithTimeout(context.Background(), c.diagnosisTimeout)
	diagnosis, err := c.firewall.Diagnose(ctx)
	cancel()
	health := normalizedHealth(c.baseHealth())
	if err != nil {
		health = HealthUnhealthy
		diagnosis = firewall.Diagnosis{Degraded: true, Blockers: []string{"firewall diagnosis unavailable"}}
	} else if health == HealthHealthy && diagnosis.Degraded {
		health = HealthDegraded
	}
	nat64Status := c.nat64.Status()
	if health == HealthHealthy && nat64Status.State != dns64.NAT64Healthy {
		health = HealthDegraded
	}
	c.mu.RLock()
	resolvers := append([]config.Resolver(nil), c.resolvers...)
	c.mu.RUnlock()
	return OperationsSnapshot{Health: health, NAT64: nat64Status, Firewall: diagnosis, Resolvers: resolvers}
}

func (c *OperationsCoordinator) Statistics() stats.Snapshot {
	return c.stats.Snapshot()
}

func (c *OperationsCoordinator) TailLogs(filter eventlog.Filter, limit int) ([]eventlog.Event, error) {
	return c.logs.Tail(filter, limit)
}

func (c *OperationsCoordinator) ClearLogs(actor string) error {
	return c.logs.Clear(actor)
}

func (c *OperationsCoordinator) ResetStatistics(node string) error {
	if node == "" {
		c.stats.ResetAll()
	} else {
		c.stats.ResetNode(node)
	}
	snapshot := c.stats.Snapshot()
	if err := c.saveStats(snapshot); err != nil {
		return fmt.Errorf("persist reset statistics: %w", err)
	}
	target := node
	if target == "" {
		target = "all"
	}
	if err := c.logs.Write(eventlog.Event{
		Kind: eventlog.KindAudit, Action: "statistics.reset", Actor: "admin", Node: target, Success: true,
	}); err != nil {
		return fmt.Errorf("write statistics reset audit: %w", err)
	}
	return nil
}

func (c *OperationsCoordinator) SetManualNAT64(ctx context.Context, prefix netip.Prefix) (dns64.NAT64Status, error) {
	if err := ctx.Err(); err != nil {
		return dns64.NAT64Status{}, err
	}
	if prefix.IsValid() {
		if err := dns64.ValidateNAT64Prefix(prefix); err != nil {
			return dns64.NAT64Status{}, err
		}
		prefix = prefix.Masked()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	before := c.nat64.ManualPrefix()
	if err := c.nat64.SetManualPrefix(prefix); err != nil {
		return dns64.NAT64Status{}, fmt.Errorf("set NAT64 prefix: %w", err)
	}
	if err := c.saveNAT64(prefix); err != nil {
		rollbackErr := c.nat64.SetManualPrefix(before)
		if rollbackErr == nil {
			c.nat64.Check(context.WithoutCancel(ctx))
		}
		return dns64.NAT64Status{}, errors.Join(fmt.Errorf("persist NAT64 prefix: %w", err), rollbackErr)
	}
	status := c.nat64.Check(ctx)
	if err := c.logs.Write(eventlog.Event{Kind: eventlog.KindAudit, Action: "nat64.update", Actor: "admin", Success: true}); err != nil {
		return status, fmt.Errorf("write NAT64 update audit: %w", err)
	}
	return status, nil
}

func (c *OperationsCoordinator) UpdateResolvers(ctx context.Context, values []config.Resolver) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateResolvers(values); err != nil {
		return err
	}
	endpoints, err := enabledResolverEndpoints(values)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	beforeEndpoints := c.resolver.Endpoints()
	if err := c.resolver.UpdateEndpoints(endpoints); err != nil {
		return fmt.Errorf("update runtime resolvers: %w", err)
	}
	if err := c.saveResolvers(append([]config.Resolver(nil), values...)); err != nil {
		rollbackErr := c.resolver.UpdateEndpoints(beforeEndpoints)
		return errors.Join(fmt.Errorf("persist resolver settings: %w", err), rollbackErr)
	}
	c.resolvers = append([]config.Resolver(nil), values...)
	if err := c.logs.Write(eventlog.Event{Kind: eventlog.KindAudit, Action: "resolver.update", Actor: "admin", Success: true}); err != nil {
		return fmt.Errorf("write resolver update audit: %w", err)
	}
	return nil
}

func (c *OperationsCoordinator) TestConnectivity(ctx context.Context) ([]ConnectivityCheck, error) {
	checks, err := c.connectivity.Test(ctx)
	return append([]ConnectivityCheck(nil), checks...), err
}

func enabledResolverEndpoints(values []config.Resolver) ([]dns64.Endpoint, error) {
	endpoints := make([]dns64.Endpoint, 0, len(values))
	for _, value := range values {
		if !value.Enabled {
			continue
		}
		address, err := netip.ParseAddr(value.Address)
		if err != nil {
			return nil, fmt.Errorf("parse resolver %q address: %w", value.Name, err)
		}
		endpoints = append(endpoints, dns64.Endpoint{
			Name: value.Name, Address: address.Unmap(), Port: value.Port, ServerName: value.ServerName,
		})
	}
	return endpoints, nil
}

func normalizedHealth(health HealthState) HealthState {
	switch health {
	case HealthHealthy, HealthDegraded, HealthUnhealthy:
		return health
	default:
		return HealthUnhealthy
	}
}

var _ OperationsService = (*OperationsCoordinator)(nil)
