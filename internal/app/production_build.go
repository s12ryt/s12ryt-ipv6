package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/admin"
	"github.com/s12ryt/s12ryt-ipv6/internal/auth"
	"github.com/s12ryt/s12ryt-ipv6/internal/config"
	"github.com/s12ryt/s12ryt-ipv6/internal/dns64"
	"github.com/s12ryt/s12ryt-ipv6/internal/eventlog"
	"github.com/s12ryt/s12ryt-ipv6/internal/firewall"
	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/network"
	"github.com/s12ryt/s12ryt-ipv6/internal/node"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
	"github.com/s12ryt/s12ryt-ipv6/internal/secret"
	"github.com/s12ryt/s12ryt-ipv6/internal/stats"
	webui "github.com/s12ryt/s12ryt-ipv6/webui"
)

const (
	productionLogSize       = 100 * 1024 * 1024
	productionLogBackups    = 5
	productionStatsInterval = 30 * time.Second
	productionHostInterval  = 60 * time.Second
)

type productionPlatform struct {
	newKernel          func() (network.Kernel, error)
	newFirewallBackend func() (firewall.Backend, error)
	binder             proxy.SocketBinder
	managementListen   ManagementListenFunc
	controlListen      func(string) (net.Listener, error)
	newQueryer         func(time.Duration) (dns64.Queryer, error)
	frontend           fs.FS
	hostAddresses      func() ([]netip.Addr, error)
	connector          func(string, bool) proxy.Connector
}

type productionDedicatedPoolCleaner struct {
	resources *admin.ResourceCoordinator
}

func (c productionDedicatedPoolCleaner) DeleteDedicatedPool(ctx context.Context, name string) error {
	for _, pool := range c.resources.State().Pools {
		if pool.Name == name {
			if pool.Kind != ipv6resource.PoolDedicatedOutbound {
				return fmt.Errorf("pool %q is not dedicated to a node", name)
			}
			return c.resources.DeletePool(ctx, name)
		}
	}
	return fmt.Errorf("dedicated pool %q does not exist", name)
}

type productionService struct {
	service *Service
	close   func() error
	once    sync.Once
	err     error
}

func (s *productionService) Run(ctx context.Context) error {
	runErr := s.service.Run(ctx)
	s.once.Do(func() { s.err = s.close() })
	return errors.Join(runErr, s.err)
}

func BuildProduction(options ProductionOptions) (ProductionService, error) {
	queryer := func(timeout time.Duration) (dns64.Queryer, error) {
		return dns64.NewDoTQueryer(timeout, netip.Addr{})
	}
	return buildProduction(options, productionPlatform{
		newKernel: network.NewLinuxKernel, newFirewallBackend: firewall.NewNftBackend,
		binder: proxy.NewSystemSocketBinder(), managementListen: ListenManagementSocket,
		controlListen: func(path string) (net.Listener, error) {
			return listenPreparedControlSocket(path, prepareControlSocket, admin.ListenControlSocket)
		},
		newQueryer: queryer, frontend: webui.Dist,
		hostAddresses: SystemHostAddresses,
		connector: func(iface string, freebind bool) proxy.Connector {
			return proxy.NewSystemConnector(iface, freebind)
		},
	})
}

func buildProduction(options ProductionOptions, platform productionPlatform) (_ ProductionService, err error) {
	if strings.TrimSpace(options.DataDirectory) == "" || options.Stdout == nil {
		return nil, errors.New("production data directory and stdout are required")
	}
	if platform.newKernel == nil || platform.newFirewallBackend == nil || platform.binder == nil ||
		platform.managementListen == nil || platform.controlListen == nil || platform.newQueryer == nil ||
		platform.frontend == nil || platform.hostAddresses == nil || platform.connector == nil {
		return nil, errors.New("production platform is incomplete")
	}
	paths, err := NewDataPaths(options.DataDirectory)
	if err != nil {
		return nil, err
	}
	configuration, err := NewConfigStore(paths.Configuration)
	if err != nil {
		return nil, err
	}
	settings, _, err := configuration.LoadOrCreate()
	if err != nil {
		return nil, fmt.Errorf("load configuration: %w", err)
	}
	logger, err := eventlog.New(paths.EventLog, productionLogSize, productionLogBackups, options.Stdout, time.Now)
	if err != nil {
		return nil, err
	}
	cleanupLog := true
	defer func() {
		if err != nil && cleanupLog {
			err = errors.Join(err, logger.Close())
		}
	}()
	health := NewHealthTracker()
	report := func(component string, cause error) {
		if cause == nil {
			return
		}
		health.ReportDegraded(component, cause)
		_ = logger.Write(eventlog.Event{Kind: eventlog.KindSystem, Action: "component.degraded", Success: false, Error: "component degraded"})
	}

	key, _, err := secret.LoadOrCreateMasterKey(paths.MasterKey, nil)
	if err != nil {
		return nil, err
	}
	vault, err := secret.NewVault(key, nil)
	if err != nil {
		return nil, err
	}
	registry, err := LoadStatistics(paths.Statistics)
	if err != nil {
		return nil, err
	}
	traffic, err := NewTrafficObserver(registry, logger, func(cause error) { report("traffic-log", cause) })
	if err != nil {
		return nil, err
	}

	queryer, err := platform.newQueryer(settings.Timeouts.Dial)
	if err != nil {
		return nil, fmt.Errorf("create DoT queryer: %w", err)
	}
	endpoints, err := productionResolverEndpoints(settings.Resolvers)
	if err != nil {
		return nil, err
	}
	resolver, err := dns64.NewResolver(endpoints, queryer, time.Now)
	if err != nil {
		return nil, err
	}
	manual, err := productionManualPrefix(settings.NAT64Prefix)
	if err != nil {
		return nil, err
	}

	var resources *admin.ResourceCoordinator
	var policyProvider *PolicyProvider
	probe := func(ctx context.Context, requested netip.Prefix) (dns64.Discovery, error) {
		if resources == nil || policyProvider == nil {
			return dns64.Discovery{}, errors.New("NAT64 runtime is not initialized")
		}
		discovery := dns64.Discovery{Prefix: requested, Source: "manual"}
		if !requested.IsValid() {
			var discoverErr error
			discovery, discoverErr = dns64.DiscoverNAT64Prefix(ctx, resolver.Endpoints(), queryer)
			if discoverErr != nil {
				return dns64.Discovery{}, discoverErr
			}
		}
		if err := verifyNAT64Path(ctx, resolver, policyProvider, resources.State(), platform.connector, discovery.Prefix, settings.Timeouts.Dial); err != nil {
			return dns64.Discovery{}, err
		}
		return discovery, nil
	}
	monitor, err := dns64.NewNAT64Monitor(dns64.DefaultProbeInterval, manual, probe, time.Now)
	if err != nil {
		return nil, err
	}
	prefix := func() netip.Prefix {
		status := monitor.Status()
		if status.State != dns64.NAT64Healthy {
			return netip.Prefix{}
		}
		return status.Prefix
	}
	policyProvider, err = NewPolicyProvider(PolicyProviderOptions{
		ScanHostAddresses: platform.hostAddresses, Configuration: configuration.Snapshot, NAT64Prefix: prefix,
	})
	if err != nil {
		return nil, err
	}
	if err := policyProvider.RefreshHostAddresses(); err != nil {
		report("host-addresses", err)
	}

	kernel, err := platform.newKernel()
	if err != nil {
		return nil, fmt.Errorf("create Linux network kernel: %w", err)
	}
	ownership, err := network.NewFileOwnershipStore(paths.NetworkOwnership)
	if err != nil {
		return nil, err
	}
	networkManager, err := network.NewResourceManager(kernel, ownership, 60*time.Second)
	if err != nil {
		return nil, err
	}
	backend, err := platform.newFirewallBackend()
	if err != nil {
		return nil, fmt.Errorf("create nftables backend: %w", err)
	}
	firewallManager, err := firewall.NewManager(backend)
	if err != nil {
		return nil, err
	}
	deferredFirewall := NewDeferredRuntimeFirewall()
	deferredDrain := NewDeferredDrainTerminator()
	resourceStates, err := ipv6resource.NewFileStateStore(paths.Resources)
	if err != nil {
		return nil, err
	}
	protectedResources, err := newProtectedResourceStateStore(resourceStates)
	if err != nil {
		return nil, err
	}
	resources, err = admin.NewResourceCoordinator(protectedResources, networkManager, deferredDrain, nil)
	if err != nil {
		return nil, err
	}
	drainQueue, err := NewDrainQueue(resources, func(cause error) { report("resource-drain", cause) })
	if err != nil {
		return nil, err
	}
	drainTracker, err := NewDrainTracker(drainQueue.Enqueue)
	if err != nil {
		return nil, err
	}
	connectorForTemplate := func(template ipv6resource.PrefixTemplate) (proxy.Connector, error) {
		return platform.connector(template.Interface, template.Mode == ipv6resource.ModeLocalRouteFreebind), nil
	}
	outbound, err := node.NewOutboundRegistry(node.OutboundRegistryOptions{
		Resolver: resolver, Policy: policyProvider.Policy, NAT64Prefix: prefix,
		Connector: connectorForTemplate, OnDrained: drainTracker.OutboundDrained,
	})
	if err != nil {
		return nil, err
	}
	inbound, err := node.NewInboundRegistry()
	if err != nil {
		return nil, err
	}
	allocator, err := proxy.NewPortAllocator(settings.Ports.Min, settings.Ports.Max, platform.binder)
	if err != nil {
		return nil, err
	}
	udpFactory, err := node.NewUDPRelayFactory(node.UDPRelayFactoryOptions{Allocator: allocator, Firewall: deferredFirewall, Observe: traffic.Observe})
	if err != nil {
		return nil, err
	}
	handlers, err := node.NewProtocolHandlerBuilder(outbound, udpFactory)
	if err != nil {
		return nil, err
	}
	listenerFactory, err := node.NewListenerRuntimeFactory(node.ListenerRuntimeOptions{
		Allocator: allocator, Handlers: handlers, Firewall: deferredFirewall, Observe: traffic.Observe,
	})
	if err != nil {
		return nil, err
	}
	resolvedFactory, err := node.NewResolvedRuntimeFactory(inbound, listenerFactory)
	if err != nil {
		return nil, err
	}
	nodeStates, err := node.NewFileStateStore(paths.Nodes, vault)
	if err != nil {
		return nil, err
	}
	manager, err := node.NewManager(resolvedFactory, productionDedicatedPoolCleaner{resources: resources}, settings.Limits.MaxNodes)
	if err != nil {
		return nil, err
	}
	persistentNodes, err := node.NewPersistentManager(manager, nodeStates)
	if err != nil {
		return nil, err
	}
	startupNodes, err := newStartupNodeRefresher(persistentNodes, tolerantStartupNodeLoader{delegate: nodeStates})
	if err != nil {
		return nil, err
	}
	terminator, err := NewRuntimeDrainTerminator(outbound, persistentNodes)
	if err != nil {
		return nil, err
	}
	if err := deferredDrain.Set(terminator); err != nil {
		return nil, err
	}
	runtimeResources, err := node.NewRuntimeResourceSynchronizer(node.RuntimeResourceSynchronizerOptions{
		Policy: policyProvider, Outbound: outbound, Inbound: inbound, Nodes: startupNodes,
		Drains: drainTracker, Timeout: 60 * time.Second, OnInboundDrained: drainTracker.InboundDrained,
	})
	if err != nil {
		return nil, err
	}
	if err := resources.SetRuntimeSynchronizer(runtimeResources); err != nil {
		return nil, err
	}

	connectivity, err := NewConnectivityTester(ConnectivityTesterOptions{
		Queryer: queryer, Resolver: resolver, Endpoints: resolver.Endpoints, Resources: resources.State,
		Policy: policyProvider.Policy, NAT64Prefix: prefix, Connector: connectorForTemplate, Timeout: settings.Timeouts.Dial,
	})
	if err != nil {
		return nil, err
	}
	sessions := auth.NewSessionManager(time.Now, nil, 30*time.Minute, 12*time.Hour)
	passwordStore, err := admin.NewFilePasswordStore(paths.AdminPassword)
	if err != nil {
		return nil, err
	}
	passwords, err := admin.NewPasswordManager(passwordStore, secret.DefaultPasswordHasher(), sessions, nil)
	if err != nil {
		return nil, err
	}
	events, err := admin.NewEventHub(128)
	if err != nil {
		return nil, err
	}
	operations, err := admin.NewOperationsCoordinator(admin.OperationsCoordinatorOptions{
		Logs: logger, Stats: registry, SaveStats: func(snapshot stats.Snapshot) error { return stats.Save(paths.Statistics, snapshot) },
		NAT64: monitor, SaveNAT64: configuration.SaveNAT64, Firewall: firewallManager,
		Resolver: resolver, Resolvers: settings.Resolvers, SaveResolvers: configuration.SaveResolvers,
		Connectivity: connectivity, BaseHealth: health.State, DiagnosisTimeout: 10 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	httpServer, err := admin.NewHTTPServer(admin.HTTPServerOptions{
		Passwords: passwords, Sessions: sessions,
		Limiter: auth.NewLoginLimiter(time.Now, 5, 500, 15*time.Minute),
		Health: func() admin.HealthState {
			state := health.State()
			if state == admin.HealthHealthy && monitor.Status().State != dns64.NAT64Healthy {
				return admin.HealthDegraded
			}
			return state
		},
		Events: events, SSEHeartbeat: 15 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	nodeService, err := newSecretRegisteringNodeService(persistentNodes, logger)
	if err != nil {
		return nil, err
	}
	if err := httpServer.SetNodeService(nodeService); err != nil {
		return nil, err
	}
	if err := httpServer.SetResourceService(resources); err != nil {
		return nil, err
	}
	if err := httpServer.SetOperationsService(operations); err != nil {
		return nil, err
	}
	if err := httpServer.SetFrontend(platform.frontend); err != nil {
		return nil, err
	}
	management, err := NewManagementHTTP(ManagementHTTPOptions{
		Handler: httpServer.Handler(), Listen: platform.managementListen, ShutdownTimeout: 15 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	control, err := admin.NewControlServer(passwords, 30*time.Second)
	if err != nil {
		return nil, err
	}
	service, err := NewService(ServiceOptions{
		ListenManagement: func(ctx context.Context) ([]net.Listener, error) {
			return management.Listen(ctx, settings.Management.Port)
		},
		InitializePassword: passwords.Initialize,
		InitializeRuntime: func(ctx context.Context) error {
			coordinator, createErr := node.NewFirewallCoordinator(ctx, firewallManager, productionManagementEndpoints(settings.Management.Port))
			if createErr != nil {
				return createErr
			}
			return deferredFirewall.Set(coordinator)
		},
		ReconcileResources: func(ctx context.Context) error {
			if startupErr := protectedResources.StartupError(); startupErr != nil {
				return fmt.Errorf("resource state is unavailable: %w", startupErr)
			}
			return resources.Reconcile(ctx)
		},
		RestoreNodes: func(ctx context.Context) error {
			restoreErr := persistentNodes.Restore(ctx)
			startupNodes.MarkRestored()
			for _, current := range persistentNodes.List() {
				logger.RegisterSecret(current.Config.Username)
				logger.RegisterSecret(current.Config.Password)
			}
			return restoreErr
		},
		ServeHTTP: management.Serve,
		ServeControl: func(ctx context.Context) error {
			listener, listenErr := platform.controlListen(paths.ControlSocket)
			if listenErr != nil {
				return listenErr
			}
			defer listener.Close()
			return control.Serve(ctx, listener)
		},
		RunNAT64: func(ctx context.Context) error {
			go monitor.Run(ctx)
			go func() {
				_ = RunPeriodicRefresh(
					ctx,
					productionHostInterval,
					policyProvider.RefreshHostAddresses,
					func(cause error) { report("host-addresses", cause) },
					func() { health.Recover("host-addresses") },
				)
			}()
			return drainQueue.Run(ctx)
		},
		ShutdownNodes: persistentNodes.Shutdown, ShutdownNetwork: networkManager.Shutdown,
		ShutdownFirewall: firewallManager.Shutdown,
		SaveStatistics:   func() error { return stats.Save(paths.Statistics, registry.Snapshot()) },
		CloseLog:         logger.Close,
		ReportDegraded:   func(cause error) { report("service", cause) },
		PasswordOutput:   options.Stdout, StatsInterval: productionStatsInterval,
	})
	if err != nil {
		return nil, err
	}
	cleanupLog = false
	return &productionService{service: service, close: logger.Close}, nil
}

func productionResolverEndpoints(values []config.Resolver) ([]dns64.Endpoint, error) {
	result := make([]dns64.Endpoint, 0, len(values))
	for _, value := range values {
		if !value.Enabled {
			continue
		}
		address, err := netip.ParseAddr(value.Address)
		if err != nil {
			return nil, fmt.Errorf("parse resolver address: %w", err)
		}
		result = append(result, dns64.Endpoint{Name: value.Name, Address: address, Port: value.Port, ServerName: value.ServerName})
	}
	if len(result) == 0 {
		return nil, errors.New("at least one resolver endpoint is required")
	}
	return result, nil
}

func productionManualPrefix(value string) (netip.Prefix, error) {
	if strings.TrimSpace(value) == "" {
		return netip.Prefix{}, nil
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return netip.Prefix{}, err
	}
	if err := dns64.ValidateNAT64Prefix(prefix); err != nil {
		return netip.Prefix{}, err
	}
	return prefix.Masked(), nil
}

func productionManagementEndpoints(port uint16) []proxy.BindEndpoint {
	return []proxy.BindEndpoint{
		{Protocol: proxy.BindTCP, Family: proxy.BindIPv4, Port: port},
		{Protocol: proxy.BindTCP, Family: proxy.BindIPv6, Port: port},
	}
}

func verifyNAT64Path(
	ctx context.Context,
	resolver *dns64.Resolver,
	policies *PolicyProvider,
	state ipv6resource.State,
	connector func(string, bool) proxy.Connector,
	prefix netip.Prefix,
	timeout time.Duration,
) error {
	resolution, err := resolver.Resolve(ctx, connectivityNAT64IPv4, policies.Policy(), "inherit", prefix)
	if err != nil || !resolution.Synthesized || len(resolution.Addresses) == 0 {
		return errors.New("NAT64 destination resolution failed")
	}
	store, err := ipv6resource.NewStoreFromState(state)
	if err != nil {
		return err
	}
	sources, err := connectivitySources(store)
	if err != nil || len(sources) == 0 {
		return errors.New("NAT64 has no outbound source")
	}
	destination := netip.AddrPortFrom(resolution.Addresses[0], connectivityPort)
	client := connector(sources[0].template.Interface, sources[0].template.Mode == ipv6resource.ModeLocalRouteFreebind)
	if client == nil {
		return errors.New("NAT64 connector is unavailable")
	}
	connection, err := client.DialContext(ctx, "tcp6", destination, sources[0].address, timeout)
	if err != nil {
		return errors.New("NAT64 connection failed")
	}
	if connection == nil {
		return errors.New("NAT64 connector returned no connection")
	}
	return connection.Close()
}

func prepareControlSocket(path string) error {
	return prepareControlSocketWith(path, os.Lstat, os.Remove)
}

func prepareControlSocketWith(
	path string,
	lstat func(string) (os.FileInfo, error),
	remove func(string) error,
) error {
	if strings.TrimSpace(path) == "" || lstat == nil || remove == nil {
		return errors.New("control socket preparation is incomplete")
	}
	info, err := lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect control socket path: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("control socket path is occupied by a non-socket file")
	}
	if err := remove(path); err != nil {
		return fmt.Errorf("remove stale control socket: %w", err)
	}
	return nil
}

func listenPreparedControlSocket(
	path string,
	prepare func(string) error,
	listen func(string) (net.Listener, error),
) (net.Listener, error) {
	if prepare == nil || listen == nil {
		return nil, errors.New("control socket listener is incomplete")
	}
	if err := prepare(path); err != nil {
		return nil, err
	}
	return listen(path)
}
