package node

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
	"github.com/s12ryt/s12ryt-ipv6/internal/secret"
)

var (
	ErrNodeExists                      = errors.New("node already exists")
	ErrNodeNotFound                    = errors.New("node was not found")
	ErrNodeLimit                       = errors.New("node limit reached")
	ErrBatchSize                       = errors.New("node batch size must be between 1 and 100")
	ErrFolderExists                    = errors.New("node folder already exists")
	ErrFolderNotFound                  = errors.New("node folder was not found")
	ErrUnauthenticatedRiskConfirmation = errors.New("unauthenticated proxy risk confirmation is required")
	ErrPreviousRuntimeCleanup          = errors.New("replacement is running but previous node runtime cleanup failed")
)

const MaxBatchCreate = 100

type Protocol string

const (
	ProtocolSOCKS Protocol = "socks"
	ProtocolHTTP  Protocol = "http"
	ProtocolMixed Protocol = "mixed"
)

type Status string

const (
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
)

type InboundMode string

const (
	InboundIPv4 InboundMode = "ipv4"
	InboundIPv6 InboundMode = "ipv6"
	InboundDual InboundMode = "dual"
)

type Config struct {
	ID                string
	Name              string
	Folder            string
	Protocol          Protocol
	Username          string
	Password          string
	MaxTCP            int
	MaxUDP            int
	DialTimeout       time.Duration
	HandshakeTimeout  time.Duration
	TunnelIdleTimeout time.Duration
	UDPIdleTimeout    time.Duration
	ULAOverride       policy.ULAOverride
	Outbound          string
	DedicatedPool     string
	Port              uint16
	InboundMode       InboundMode
	InboundResource   string
	Inbound           []proxy.BindSpec
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return errors.New("node ID is required")
	}
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("node name is required")
	}
	if _, err := NormalizeFolderName(c.Folder); err != nil {
		return err
	}
	switch c.Protocol {
	case ProtocolSOCKS, ProtocolHTTP, ProtocolMixed:
	default:
		return fmt.Errorf("unsupported node protocol %q", c.Protocol)
	}
	if (c.Username == "") != (c.Password == "") {
		return errors.New("node username and password must both be set or both be empty")
	}
	if c.Username != "" {
		if _, err := secret.NewProxyCredentials(c.Username, c.Password, nil); err != nil {
			return err
		}
	}
	if c.MaxTCP <= 0 {
		return errors.New("node TCP connection limit must be positive")
	}
	if c.MaxUDP <= 0 {
		return errors.New("node UDP association limit must be positive")
	}
	if c.DialTimeout <= 0 {
		return errors.New("node dial timeout must be positive")
	}
	if c.HandshakeTimeout <= 0 {
		return errors.New("node handshake timeout must be positive")
	}
	if c.TunnelIdleTimeout < 0 {
		return errors.New("node tunnel idle timeout must not be negative")
	}
	if c.UDPIdleTimeout <= 0 {
		return errors.New("node UDP idle timeout must be positive")
	}
	switch c.ULAOverride {
	case "", policy.ULAInherit, policy.ULAAllow, policy.ULADeny:
	default:
		return fmt.Errorf("invalid node ULA override %q", c.ULAOverride)
	}
	if strings.TrimSpace(c.Outbound) == "" {
		return errors.New("node outbound resource is required")
	}
	switch c.InboundMode {
	case "":
		if len(c.Inbound) == 0 {
			return errors.New("node must have at least one inbound listener")
		}
	case InboundIPv4:
		if strings.TrimSpace(c.InboundResource) != "" {
			return errors.New("IPv4-only inbound cannot reference an IPv6 resource")
		}
	case InboundIPv6, InboundDual:
		if strings.TrimSpace(c.InboundResource) == "" {
			return errors.New("IPv6 inbound resource is required")
		}
	default:
		return fmt.Errorf("unsupported inbound mode %q", c.InboundMode)
	}
	for _, inbound := range c.Inbound {
		if inbound.Protocol != proxy.BindTCP {
			return errors.New("node inbound listeners must use TCP")
		}
	}
	return nil
}

func NormalizeFolderName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !utf8.ValidString(value) {
		return "", errors.New("node folder must be valid UTF-8")
	}
	if utf8.RuneCountInString(value) > 64 {
		return "", errors.New("node folder must not exceed 64 characters")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", errors.New("node folder must not contain control characters")
		}
	}
	return value, nil
}

type Node struct {
	Config Config
	Status Status
}

type Runtime interface {
	Port() uint16
	Stop(context.Context) error
}

type RuntimeFactory interface {
	Start(context.Context, Config) (Runtime, error)
}

type RuntimeReplacementFactory interface {
	Replace(context.Context, Runtime, Config) (Runtime, error)
}

type BindingRefreshRuntime interface {
	Runtime
	RefreshBindings(context.Context, Config, func(proxy.BindEndpoint)) error
}

type BindingDrainRuntime interface {
	Runtime
	ForceDrainBindings([]netip.Addr) error
}

type InboundDrainedObserver func(nodeID, resource string, endpoint proxy.BindEndpoint)

type DedicatedPoolCleaner interface {
	DeleteDedicatedPool(context.Context, string) error
}

type managedNode struct {
	config  Config
	status  Status
	runtime Runtime
}

type Manager struct {
	mu       sync.Mutex
	factory  RuntimeFactory
	pool     DedicatedPoolCleaner
	maxNodes int
	nodes    map[string]*managedNode
}

func NewManager(factory RuntimeFactory, pool DedicatedPoolCleaner, maxNodes int) (*Manager, error) {
	if factory == nil {
		return nil, errors.New("node runtime factory is required")
	}
	if maxNodes <= 0 {
		return nil, errors.New("maximum node count must be positive")
	}
	return &Manager{factory: factory, pool: pool, maxNodes: maxNodes, nodes: make(map[string]*managedNode)}, nil
}

func (m *Manager) Create(ctx context.Context, config Config, confirmUnauthenticated bool) (Node, error) {
	folder, folderErr := NormalizeFolderName(config.Folder)
	if folderErr != nil {
		return Node{}, folderErr
	}
	config.Folder = folder
	if err := validateMutation(config, confirmUnauthenticated); err != nil {
		return Node{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.nodes[config.ID]; exists {
		return Node{}, ErrNodeExists
	}
	if len(m.nodes) >= m.maxNodes {
		return Node{}, ErrNodeLimit
	}
	runtime, err := m.factory.Start(ctx, config)
	if err != nil {
		return Node{}, fmt.Errorf("start node: %w", err)
	}
	if runtime == nil {
		return Node{}, errors.New("node runtime factory returned nil")
	}
	config.Port = runtime.Port()
	m.nodes[config.ID] = &managedNode{config: config, status: StatusRunning, runtime: runtime}
	return Node{Config: cloneConfig(config), Status: StatusRunning}, nil
}

func (m *Manager) CreateBatch(ctx context.Context, configs []Config, confirmUnauthenticated bool) ([]Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(configs) == 0 || len(configs) > MaxBatchCreate {
		return nil, ErrBatchSize
	}
	normalized := make([]Config, len(configs))
	ids := make(map[string]struct{}, len(configs))
	names := make(map[string]struct{}, len(configs))
	ports := make(map[uint16]struct{}, len(configs))
	folder := ""
	for index, config := range configs {
		normalizedFolder, err := NormalizeFolderName(config.Folder)
		if err != nil {
			return nil, fmt.Errorf("node %d folder: %w", index, err)
		}
		if normalizedFolder == "" {
			return nil, errors.New("batch node folder is required")
		}
		config.Folder = normalizedFolder
		if folder == "" {
			folder = normalizedFolder
		} else if folder != normalizedFolder {
			return nil, errors.New("batch nodes must use one folder")
		}
		if err := validateMutation(config, confirmUnauthenticated); err != nil {
			return nil, fmt.Errorf("validate batch node %q: %w", config.ID, err)
		}
		if _, duplicate := ids[config.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate batch node ID %q", ErrNodeExists, config.ID)
		}
		ids[config.ID] = struct{}{}
		if _, duplicate := names[config.Name]; duplicate {
			return nil, fmt.Errorf("duplicate batch node name %q", config.Name)
		}
		names[config.Name] = struct{}{}
		if config.Port != 0 {
			if _, duplicate := ports[config.Port]; duplicate {
				return nil, fmt.Errorf("duplicate batch node port %d", config.Port)
			}
			ports[config.Port] = struct{}{}
		}
		normalized[index] = config
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.nodes)+len(normalized) > m.maxNodes {
		return nil, ErrNodeLimit
	}
	for _, current := range m.nodes {
		if current.config.Folder == folder {
			return nil, ErrFolderExists
		}
	}
	for _, config := range normalized {
		if _, exists := m.nodes[config.ID]; exists {
			return nil, ErrNodeExists
		}
	}

	createdIDs := make([]string, 0, len(normalized))
	created := make([]Node, 0, len(normalized))
	for _, config := range normalized {
		runtime, err := m.factory.Start(ctx, config)
		if err != nil {
			rollbackErr := m.rollbackBatchLocked(context.WithoutCancel(ctx), createdIDs)
			return nil, errors.Join(fmt.Errorf("start batch node %q: %w", config.ID, err), rollbackErr)
		}
		if runtime == nil {
			rollbackErr := m.rollbackBatchLocked(context.WithoutCancel(ctx), createdIDs)
			return nil, errors.Join(fmt.Errorf("start batch node %q: runtime factory returned nil", config.ID), rollbackErr)
		}
		config.Port = runtime.Port()
		m.nodes[config.ID] = &managedNode{config: config, status: StatusRunning, runtime: runtime}
		createdIDs = append(createdIDs, config.ID)
		created = append(created, Node{Config: cloneConfig(config), Status: StatusRunning})
	}
	return created, nil
}

func (m *Manager) rollbackBatch(ctx context.Context, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rollbackBatchLocked(ctx, ids)
}

func (m *Manager) rollbackBatchLocked(ctx context.Context, ids []string) error {
	var failures []error
	for index := len(ids) - 1; index >= 0; index-- {
		id := ids[index]
		current := m.nodes[id]
		if current == nil {
			continue
		}
		if current.status == StatusRunning {
			if err := current.runtime.Stop(ctx); err != nil {
				failures = append(failures, fmt.Errorf("stop rolled back node %q: %w", id, err))
				continue
			}
			current.runtime = nil
			current.status = StatusStopped
		}
		if current.config.DedicatedPool != "" && m.pool != nil {
			if err := m.pool.DeleteDedicatedPool(ctx, current.config.DedicatedPool); err != nil {
				failures = append(failures, fmt.Errorf("clean rolled back node %q dedicated pool: %w", id, err))
				continue
			}
		}
		delete(m.nodes, id)
	}
	return errors.Join(failures...)
}

func (m *Manager) Update(ctx context.Context, id string, config Config, confirmUnauthenticated bool) (Node, error) {
	folder, folderErr := NormalizeFolderName(config.Folder)
	if folderErr != nil {
		return Node{}, folderErr
	}
	config.Folder = folder
	if config.ID != id {
		return Node{}, errors.New("node ID cannot be changed")
	}
	if err := validateMutation(config, confirmUnauthenticated); err != nil {
		return Node{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.nodes[id]
	if current == nil {
		return Node{}, ErrNodeNotFound
	}
	if current.status == StatusStopped {
		current.config = config
		return nodeSnapshot(current), nil
	}
	var replacement Runtime
	var err error
	if replacer, ok := m.factory.(RuntimeReplacementFactory); ok {
		replacement, err = replacer.Replace(ctx, current.runtime, config)
	} else {
		replacement, err = m.factory.Start(ctx, config)
	}
	if err != nil {
		return Node{}, fmt.Errorf("start replacement node: %w", err)
	}
	if replacement == nil {
		return Node{}, errors.New("node runtime factory returned nil")
	}
	var cleanupErr error
	if replacement != current.runtime {
		cleanupErr = current.runtime.Stop(ctx)
	}
	config.Port = replacement.Port()
	current.config = config
	current.runtime = replacement
	snapshot := nodeSnapshot(current)
	if cleanupErr != nil {
		return snapshot, fmt.Errorf("%w: %v", ErrPreviousRuntimeCleanup, cleanupErr)
	}
	return snapshot, nil
}

func (m *Manager) Start(ctx context.Context, id string) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.nodes[id]
	if current == nil {
		return Node{}, ErrNodeNotFound
	}
	if current.status == StatusRunning {
		return nodeSnapshot(current), nil
	}
	runtime, err := m.factory.Start(ctx, current.config)
	if err != nil {
		return Node{}, fmt.Errorf("start node: %w", err)
	}
	if runtime == nil {
		return Node{}, errors.New("node runtime factory returned nil")
	}
	current.config.Port = runtime.Port()
	current.runtime = runtime
	current.status = StatusRunning
	return nodeSnapshot(current), nil
}

func (m *Manager) Stop(ctx context.Context, id string) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.nodes[id]
	if current == nil {
		return Node{}, ErrNodeNotFound
	}
	if current.status == StatusStopped {
		return nodeSnapshot(current), nil
	}
	if err := current.runtime.Stop(ctx); err != nil {
		return Node{}, fmt.Errorf("stop node: %w", err)
	}
	current.runtime = nil
	current.status = StatusStopped
	return nodeSnapshot(current), nil
}

func (m *Manager) MoveToFolder(ctx context.Context, id, folder string) (Node, error) {
	if err := ctx.Err(); err != nil {
		return Node{}, err
	}
	normalized, err := NormalizeFolderName(folder)
	if err != nil {
		return Node{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.nodes[id]
	if current == nil {
		return Node{}, ErrNodeNotFound
	}
	current.config.Folder = normalized
	return nodeSnapshot(current), nil
}

func (m *Manager) RenameFolder(ctx context.Context, source, target string) ([]Node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, err := NormalizeFolderName(source)
	if err != nil || source == "" {
		if err == nil {
			err = errors.New("source node folder is required")
		}
		return nil, err
	}
	target, err = NormalizeFolderName(target)
	if err != nil || target == "" {
		if err == nil {
			err = errors.New("target node folder is required")
		}
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	found := false
	if source != target {
		for _, current := range m.nodes {
			if current.config.Folder == target {
				return nil, ErrFolderExists
			}
		}
	}
	result := make([]Node, 0)
	for _, current := range m.nodes {
		if current.config.Folder != source {
			continue
		}
		found = true
		current.config.Folder = target
		result = append(result, nodeSnapshot(current))
	}
	if !found {
		return nil, ErrFolderNotFound
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Config.ID < result[j].Config.ID })
	return result, nil
}

func (m *Manager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.nodes[id]
	if current == nil {
		return ErrNodeNotFound
	}
	if current.status == StatusRunning {
		if err := current.runtime.Stop(ctx); err != nil {
			return fmt.Errorf("stop node before deletion: %w", err)
		}
		current.runtime = nil
		current.status = StatusStopped
	}
	if current.config.DedicatedPool != "" && m.pool != nil {
		if err := m.pool.DeleteDedicatedPool(ctx, current.config.DedicatedPool); err != nil {
			return fmt.Errorf("delete node dedicated pool: %w", err)
		}
	}
	delete(m.nodes, id)
	return nil
}

func (m *Manager) detach(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.nodes[id]
	if current == nil {
		return ErrNodeNotFound
	}
	if current.status == StatusRunning {
		if err := current.runtime.Stop(ctx); err != nil {
			return fmt.Errorf("stop node before detaching: %w", err)
		}
	}
	delete(m.nodes, id)
	return nil
}

func (m *Manager) restoreNode(ctx context.Context, snapshot Node) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.nodes[snapshot.Config.ID]; exists {
		return ErrNodeExists
	}
	if len(m.nodes) >= m.maxNodes {
		return ErrNodeLimit
	}
	if snapshot.Status == StatusStopped {
		m.nodes[snapshot.Config.ID] = &managedNode{config: cloneConfig(snapshot.Config), status: StatusStopped}
		return nil
	}
	runtime, err := m.factory.Start(ctx, snapshot.Config)
	if err != nil {
		return fmt.Errorf("restart detached node: %w", err)
	}
	if runtime == nil {
		return errors.New("node runtime factory returned nil")
	}
	config := cloneConfig(snapshot.Config)
	config.Port = runtime.Port()
	m.nodes[config.ID] = &managedNode{config: config, status: StatusRunning, runtime: runtime}
	return nil
}

func (m *Manager) cleanupDedicatedPool(ctx context.Context, config Config) error {
	if config.DedicatedPool == "" || m.pool == nil {
		return nil
	}
	if err := m.pool.DeleteDedicatedPool(ctx, config.DedicatedPool); err != nil {
		return fmt.Errorf("delete node dedicated pool: %w", err)
	}
	return nil
}

func (m *Manager) Get(id string) (Node, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.nodes[id]
	if current == nil {
		return Node{}, false
	}
	return nodeSnapshot(current), true
}

func (m *Manager) List() []Node {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Node, 0, len(m.nodes))
	for _, current := range m.nodes {
		result = append(result, nodeSnapshot(current))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Config.ID < result[j].Config.ID })
	return result
}

func (m *Manager) State() State {
	return State{Nodes: m.List()}
}

func (m *Manager) Restore(ctx context.Context, state State) error {
	normalized, err := normalizeState(state)
	if err != nil {
		return err
	}
	if len(normalized.Nodes) > m.maxNodes {
		return ErrNodeLimit
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.nodes) != 0 {
		return errors.New("node manager must be empty before restore")
	}
	for _, current := range normalized.Nodes {
		m.nodes[current.Config.ID] = &managedNode{config: cloneConfig(current.Config), status: StatusStopped}
	}

	var failures []error
	for _, desired := range normalized.Nodes {
		if desired.Status != StatusRunning {
			continue
		}
		runtime, startErr := m.factory.Start(ctx, desired.Config)
		if startErr != nil {
			failures = append(failures, fmt.Errorf("restore node %q: %w", desired.Config.ID, startErr))
			continue
		}
		if runtime == nil {
			failures = append(failures, fmt.Errorf("restore node %q: runtime factory returned nil", desired.Config.ID))
			continue
		}
		current := m.nodes[desired.Config.ID]
		current.config.Port = runtime.Port()
		current.runtime = runtime
		current.status = StatusRunning
	}
	return errors.Join(failures...)
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.nodes))
	for id := range m.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var failures []error
	for _, id := range ids {
		current := m.nodes[id]
		if current.status != StatusRunning {
			continue
		}
		if err := current.runtime.Stop(ctx); err != nil {
			failures = append(failures, fmt.Errorf("stop node %q: %w", id, err))
			continue
		}
		current.runtime = nil
		current.status = StatusStopped
	}
	return errors.Join(failures...)
}

func (m *Manager) RefreshInboundBindings(ctx context.Context, resolver InboundConfigResolver, onDrained InboundDrainedObserver) error {
	if resolver == nil {
		return errors.New("inbound config resolver is required")
	}
	type refresh struct {
		nodeID   string
		resource string
		config   Config
		runtime  BindingRefreshRuntime
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.nodes))
	for id := range m.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	refreshes := make([]refresh, 0, len(ids))
	for _, id := range ids {
		current := m.nodes[id]
		if current.status != StatusRunning || current.config.InboundMode == "" {
			continue
		}
		resolved, err := resolver.Resolve(current.config)
		if err != nil {
			return fmt.Errorf("resolve inbound bindings for node %q: %w", id, err)
		}
		if err := resolved.Validate(); err != nil {
			return fmt.Errorf("validate resolved inbound bindings for node %q: %w", id, err)
		}
		runtime, ok := current.runtime.(BindingRefreshRuntime)
		if !ok {
			return fmt.Errorf("node %q runtime does not support binding refresh", id)
		}
		refreshes = append(refreshes, refresh{
			nodeID: id, resource: current.config.InboundResource,
			config: resolved, runtime: runtime,
		})
	}
	for _, item := range refreshes {
		callback := func(endpoint proxy.BindEndpoint) {
			if onDrained != nil {
				onDrained(item.nodeID, item.resource, endpoint)
			}
		}
		if err := item.runtime.RefreshBindings(ctx, item.config, callback); err != nil {
			return fmt.Errorf("refresh inbound bindings for node %q: %w", item.nodeID, err)
		}
	}
	return nil
}

func (m *Manager) ForceDrainInbound(ctx context.Context, resource string, addresses []netip.Addr) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	resource = strings.TrimSpace(resource)
	if resource == "" {
		return errors.New("inbound pool name is required")
	}
	if len(addresses) == 0 {
		return errors.New("at least one draining inbound address is required")
	}
	normalized := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !address.IsValid() || !address.Is6() || address.Is4In6() {
			return errors.New("draining inbound address must be native IPv6")
		}
		if _, duplicate := seen[address]; duplicate {
			continue
		}
		seen[address] = struct{}{}
		normalized = append(normalized, address)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.nodes))
	for id := range m.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var failures []error
	for _, id := range ids {
		current := m.nodes[id]
		if current.status != StatusRunning || current.config.InboundResource != resource ||
			(current.config.InboundMode != InboundIPv6 && current.config.InboundMode != InboundDual) {
			continue
		}
		runtime, ok := current.runtime.(BindingDrainRuntime)
		if !ok {
			failures = append(failures, fmt.Errorf("node %q runtime does not support forced binding drain", id))
			continue
		}
		if err := runtime.ForceDrainBindings(normalized); err != nil {
			failures = append(failures, fmt.Errorf("force drain inbound bindings for node %q: %w", id, err))
		}
	}
	return errors.Join(failures...)
}

func validateMutation(config Config, confirmUnauthenticated bool) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if config.Username == "" && !confirmUnauthenticated {
		return ErrUnauthenticatedRiskConfirmation
	}
	return nil
}

func nodeSnapshot(current *managedNode) Node {
	return Node{Config: cloneConfig(current.config), Status: current.status}
}

func cloneConfig(config Config) Config {
	config.Inbound = append([]proxy.BindSpec(nil), config.Inbound...)
	return config
}
