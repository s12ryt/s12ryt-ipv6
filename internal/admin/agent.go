package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/config"
	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/node"
	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
	"github.com/s12ryt/s12ryt-ipv6/internal/secret"
)

type AgentSettingsStore interface {
	Snapshot() config.Config
	Replace(config.Config) error
}

type AgentServiceOptions struct {
	Settings            AgentSettingsStore
	ActiveSettings      config.Config
	Resources           ResourceService
	Nodes               NodeService
	Operations          OperationsService
	Health              func() HealthState
	GenerateCredentials func() (secret.ProxyCredentials, error)
}

type AgentService struct {
	settings            AgentSettingsStore
	activeSettings      config.Config
	resources           ResourceService
	nodes               NodeService
	operations          OperationsService
	health              func() HealthState
	generateCredentials func() (secret.ProxyCredentials, error)
}

type agentRequest struct {
	Command     string          `json:"command"`
	Input       json.RawMessage `json:"input,omitempty"`
	ShowSecrets bool            `json:"show_secrets,omitempty"`
	Yes         bool            `json:"yes,omitempty"`
	Prune       bool            `json:"prune,omitempty"`
	DryRun      bool            `json:"dry_run,omitempty"`
	Arguments   json.RawMessage `json:"arguments,omitempty"`
}

type agentError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type agentEnvelope struct {
	OK    bool        `json:"ok"`
	Data  any         `json:"data,omitempty"`
	Error *agentError `json:"error,omitempty"`
}

type agentValidationError struct {
	message string
	details map[string]any
}

func (e *agentValidationError) Error() string { return e.message }

func NewAgentService(options AgentServiceOptions) (*AgentService, error) {
	switch {
	case options.Settings == nil:
		return nil, errors.New("agent settings store is required")
	case options.Resources == nil:
		return nil, errors.New("agent resource service is required")
	case options.Nodes == nil:
		return nil, errors.New("agent node service is required")
	case options.Operations == nil:
		return nil, errors.New("agent operations service is required")
	case options.Health == nil:
		return nil, errors.New("agent health provider is required")
	}
	generateCredentials := options.GenerateCredentials
	if generateCredentials == nil {
		generateCredentials = func() (secret.ProxyCredentials, error) {
			return secret.NewProxyCredentials("", "", nil)
		}
	}
	return &AgentService{
		settings: options.Settings, activeSettings: cloneAgentConfig(options.ActiveSettings),
		resources: options.Resources, nodes: options.Nodes, operations: options.Operations, health: options.Health,
		generateCredentials: generateCredentials,
	}, nil
}

func (s *AgentService) HandleAgent(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	var request agentRequest
	if err := decodeAgentJSON(payload, &request); err != nil {
		return marshalAgentEnvelope(agentEnvelope{OK: false, Error: &agentError{Code: "invalid_usage", Message: "invalid agent request"}})
	}
	if strings.TrimSpace(request.Command) == "" {
		return marshalAgentEnvelope(agentEnvelope{OK: false, Error: &agentError{Code: "invalid_usage", Message: "agent command is required"}})
	}

	var response agentEnvelope
	switch request.Command {
	case "schema":
		if len(request.Input) != 0 || request.ShowSecrets || request.Yes || request.Prune || request.DryRun || len(request.Arguments) != 0 {
			response = agentFailure("invalid_usage", "schema does not accept additional options", nil)
		} else {
			response = agentEnvelope{OK: true, Data: agentDocumentSchema()}
		}
	case "export":
		if len(request.Input) != 0 || request.Yes || request.Prune || request.DryRun || len(request.Arguments) != 0 {
			response = agentFailure("invalid_usage", "export received unsupported options", nil)
		} else {
			response = s.exportDocument(request.ShowSecrets)
		}
	case "apply":
		response = s.applyDocument(ctx, request)
	case "status":
		if len(request.Input) != 0 || request.ShowSecrets || request.Yes || request.Prune || request.DryRun || len(request.Arguments) != 0 {
			response = agentFailure("invalid_usage", "status does not accept additional options", nil)
		} else {
			response = s.status()
		}
	default:
		response = s.command(ctx, request)
	}
	return marshalAgentEnvelope(response)
}

func (s *AgentService) status() agentEnvelope {
	configured := s.settings.Snapshot()
	restartFields := changedStartupFields(s.activeSettings, configured)
	return agentEnvelope{OK: true, Data: map[string]any{
		"health":           s.health(),
		"restart_required": len(restartFields) != 0,
		"restart_fields":   restartFields,
		"configured":       exportAgentSettings(configured),
		"active":           exportAgentSettings(s.activeSettings),
		"resources":        resourceCounts(s.resources.Snapshot()),
		"nodes":            len(s.nodes.List()),
	}}
}

func (s *AgentService) exportDocument(showSecrets bool) agentEnvelope {
	settings := s.settings.Snapshot()
	snapshot := s.resources.Snapshot()
	resources, err := exportAgentResources(snapshot)
	if err != nil {
		return agentFailure("operation_failed", "resource state cannot be exported", nil)
	}
	nodes := s.nodes.List()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Config.ID < nodes[j].Config.ID })
	exportedNodes := make([]agentNodeDocument, 0, len(nodes))
	for _, current := range nodes {
		exportedNodes = append(exportedNodes, exportAgentNode(current, showSecrets))
	}
	return agentEnvelope{OK: true, Data: agentDocument{
		SchemaVersion: agentDocumentSchemaVersion,
		Settings:      exportAgentSettings(settings), Resources: resources,
		Nodes: &exportedNodes, Network: exportAgentNetwork(settings),
	}}
}

func (s *AgentService) applyDocument(ctx context.Context, request agentRequest) agentEnvelope {
	if len(request.Input) == 0 || len(request.Arguments) != 0 || request.ShowSecrets {
		return agentFailure("invalid_usage", "apply requires one configuration document", nil)
	}
	if request.Prune && !request.DryRun && !request.Yes {
		return agentFailure("confirmation_required", "prune requires --yes", nil)
	}
	var document agentDocument
	if err := decodeAgentJSON(request.Input, &document); err != nil {
		return agentFailure("invalid_document", "invalid configuration document", nil)
	}
	if document.SchemaVersion != agentDocumentSchemaVersion {
		return agentFailure("invalid_document", "unsupported configuration schema version", map[string]any{"schema_version": document.SchemaVersion})
	}

	currentSettings := s.settings.Snapshot()
	candidateSettings, err := mergeAgentSettings(currentSettings, document.Settings)
	if err != nil {
		var validation *agentValidationError
		if errors.As(err, &validation) {
			return agentFailure("invalid_document", validation.message, validation.details)
		}
		return agentFailure("invalid_document", "invalid settings", nil)
	}
	if err := candidateSettings.Validate(); err != nil {
		return agentFailure("invalid_document", "invalid configuration settings", nil)
	}
	resourceSnapshot := s.resources.Snapshot()
	if request.Prune && document.Resources != nil {
		if response := preflightAgentPrunedResourceDependencies(*document.Resources); response != nil {
			return *response
		}
	}
	if response := preflightAgentResources(document.Resources, resourceSnapshot, candidateSettings); response != nil {
		return *response
	}
	if response := preflightAgentNodes(document.Nodes, s.nodes); response != nil {
		return *response
	}
	if response := preflightAgentNetwork(document.Network); response != nil {
		return *response
	}
	nodePlans, response := buildAgentNodePlans(document.Nodes, s.nodes, candidateSettings)
	if response != nil {
		return *response
	}
	if response := preflightAgentNodeResources(document, nodePlans, resourceSnapshot, request.Prune, s.nodes); response != nil {
		return *response
	}

	restartFields := changedStartupFields(currentSettings, candidateSettings)
	data := map[string]any{
		"dry_run": request.DryRun, "completed": []string{},
		"restart_required": len(restartFields) != 0, "restart_fields": restartFields,
	}
	if request.DryRun {
		return agentEnvelope{OK: true, Data: data}
	}
	nodePlans, response = s.materializeAgentNodeCredentials(nodePlans)
	if response != nil {
		return *response
	}
	completed := make([]string, 0)
	if document.Settings != nil && !reflect.DeepEqual(currentSettings, candidateSettings) {
		if err := s.settings.Replace(candidateSettings); err != nil {
			return agentFailure("operation_failed", "settings update failed", map[string]any{"completed": completed})
		}
		completed = append(completed, "settings")
	}
	if response := s.applyResourceCreates(ctx, document.Resources, resourceSnapshot, candidateSettings, &completed); response != nil {
		return *response
	}
	if response := s.applyNetwork(ctx, document.Network, &completed); response != nil {
		return *response
	}
	if response := s.applyNodes(ctx, nodePlans, &completed); response != nil {
		return *response
	}
	if request.Prune && document.Nodes != nil {
		if response := s.pruneNodes(ctx, *document.Nodes, &completed); response != nil {
			return *response
		}
	}
	if request.Prune && document.Resources != nil {
		if response := s.pruneResources(ctx, *document.Resources, resourceSnapshot, &completed); response != nil {
			return *response
		}
	}
	data["completed"] = completed
	return agentEnvelope{OK: true, Data: data}
}

type agentNodePlan struct {
	configuration       node.Config
	status              node.Status
	existing            *node.Node
	confirm             bool
	generateCredentials bool
}

func buildAgentNodePlans(input *[]agentNodeDocument, service NodeService, settings config.Config) ([]agentNodePlan, *agentEnvelope) {
	if input == nil {
		return nil, nil
	}
	plans := make([]agentNodePlan, 0, len(*input))
	for _, item := range *input {
		current, exists := service.Get(item.ID)
		configuration := node.Config{
			ID: item.ID, MaxTCP: settings.Limits.TCPPerNode, MaxUDP: settings.Limits.UDPPerNode,
			DialTimeout: settings.Timeouts.Dial, HandshakeTimeout: settings.Timeouts.Handshake,
			TunnelIdleTimeout: settings.Timeouts.TunnelIdle, UDPIdleTimeout: settings.Timeouts.UDPIdle,
			ULAOverride: policy.ULAInherit,
		}
		var existing *node.Node
		if exists {
			configuration = current.Config
			copy := current
			existing = &copy
		}
		if item.Name != nil {
			configuration.Name = *item.Name
		}
		if item.Folder != nil {
			configuration.Folder = *item.Folder
		}
		if item.Protocol != nil {
			configuration.Protocol = *item.Protocol
		}
		if item.MaxTCP != nil {
			configuration.MaxTCP = *item.MaxTCP
		}
		if item.MaxUDP != nil {
			configuration.MaxUDP = *item.MaxUDP
		}
		if response := applyAgentNodeDurations(item, &configuration); response != nil {
			return nil, response
		}
		if item.ULAOverride != nil {
			configuration.ULAOverride = *item.ULAOverride
		}
		if item.Outbound != nil {
			configuration.Outbound = *item.Outbound
		}
		if item.DedicatedPool != nil {
			configuration.DedicatedPool = *item.DedicatedPool
		}
		if item.Port != nil {
			configuration.Port = *item.Port
		}
		if item.InboundMode != nil {
			configuration.InboundMode = *item.InboundMode
		}
		if item.InboundResource != nil {
			configuration.InboundResource = *item.InboundResource
		}
		generateCredentials := false
		switch item.Authentication.Action {
		case "preserve":
		case "set":
			configuration.Username, configuration.Password = item.Authentication.Username, item.Authentication.Password
		case "generate":
			configuration.Username, configuration.Password = "pending", "pending-credential-value"
			generateCredentials = true
		case "none":
			configuration.Username, configuration.Password = "", ""
		}
		if err := configuration.Validate(); err != nil {
			result := agentFailure("invalid_document", "invalid node configuration", map[string]any{"id": item.ID})
			return nil, &result
		}
		plans = append(plans, agentNodePlan{
			configuration: configuration, status: item.DesiredStatus, existing: existing,
			confirm: item.ConfirmUnauthenticated, generateCredentials: generateCredentials,
		})
	}
	return plans, nil
}

func (s *AgentService) materializeAgentNodeCredentials(plans []agentNodePlan) ([]agentNodePlan, *agentEnvelope) {
	for index := range plans {
		if !plans[index].generateCredentials {
			continue
		}
		credentials, err := s.generateCredentials()
		if err != nil {
			result := agentFailure("internal_error", "credential generation failed", nil)
			return nil, &result
		}
		plans[index].configuration.Username = credentials.Username
		plans[index].configuration.Password = credentials.Password
		plans[index].generateCredentials = false
		if err := plans[index].configuration.Validate(); err != nil {
			result := agentFailure("internal_error", "credential generation failed", nil)
			return nil, &result
		}
	}
	return plans, nil
}

func applyAgentNodeDurations(item agentNodeDocument, configuration *node.Config) *agentEnvelope {
	values := []struct {
		name string
		raw  *string
		to   *time.Duration
	}{
		{name: "dial_timeout", raw: item.DialTimeout, to: &configuration.DialTimeout},
		{name: "handshake_timeout", raw: item.HandshakeTimeout, to: &configuration.HandshakeTimeout},
		{name: "tunnel_idle_timeout", raw: item.TunnelIdleTimeout, to: &configuration.TunnelIdleTimeout},
		{name: "udp_idle_timeout", raw: item.UDPIdleTimeout, to: &configuration.UDPIdleTimeout},
	}
	for _, value := range values {
		if value.raw == nil {
			continue
		}
		duration, err := time.ParseDuration(*value.raw)
		if err != nil {
			result := agentFailure("invalid_document", "invalid node timeout", map[string]any{"id": item.ID, "field": value.name})
			return &result
		}
		*value.to = duration
	}
	return nil
}

func preflightAgentNetwork(input *agentNetworkDocument) *agentEnvelope {
	if input == nil {
		return nil
	}
	if input.NAT64Prefix != nil {
		if _, err := parseNAT64Prefix(*input.NAT64Prefix); err != nil {
			result := agentFailure("invalid_document", "invalid NAT64 configuration", nil)
			return &result
		}
	}
	if input.Resolvers != nil {
		resolvers := agentResolversToConfig(*input.Resolvers)
		if validateResolvers(resolvers) != nil {
			result := agentFailure("invalid_document", "invalid resolver configuration", nil)
			return &result
		}
	}
	return nil
}

func (s *AgentService) applyResourceCreates(ctx context.Context, input *agentResourcesDocument, current ResourceSnapshot, settings config.Config, completed *[]string) *agentEnvelope {
	if input == nil {
		return nil
	}
	existingTemplates := make(map[string]struct{}, len(current.Templates))
	existingFixed := make(map[string]struct{}, len(current.Fixed))
	existingPools := make(map[string]struct{}, len(current.Pools))
	for _, item := range current.Templates {
		existingTemplates[item.Name] = struct{}{}
	}
	for _, item := range current.Fixed {
		existingFixed[item.Name] = struct{}{}
	}
	for _, item := range current.Pools {
		if item != nil {
			existingPools[item.Name] = struct{}{}
		}
	}
	for _, item := range input.Templates {
		if _, exists := existingTemplates[item.Name]; exists {
			continue
		}
		template, _ := ipv6resource.NewPrefixTemplate(item.Name, item.Prefix, item.Interface, item.Mode)
		if err := s.resources.CreateTemplate(ctx, template); err != nil {
			return agentOperationFailure("resource template creation failed", *completed)
		}
		*completed = append(*completed, "resources.template."+item.Name)
	}
	for _, item := range input.Fixed {
		if _, exists := existingFixed[item.Name]; exists {
			continue
		}
		var address *netip.Addr
		if item.Address != "" {
			parsed, _ := netip.ParseAddr(item.Address)
			address = &parsed
		}
		if _, err := s.resources.CreateFixedAddress(ctx, item.Name, item.Template, address); err != nil {
			return agentOperationFailure("fixed resource creation failed", *completed)
		}
		*completed = append(*completed, "resources.fixed."+item.Name)
	}
	for _, item := range input.Pools {
		if _, exists := existingPools[item.Name]; exists {
			continue
		}
		capacity := agentPoolCapacity(item, settings)
		if _, err := s.resources.CreatePool(ctx, item.Name, item.Kind, item.Template, capacity, item.Pinned); err != nil {
			return agentOperationFailure("resource pool creation failed", *completed)
		}
		*completed = append(*completed, "resources.pool."+item.Name)
	}
	return nil
}

func (s *AgentService) applyNetwork(ctx context.Context, input *agentNetworkDocument, completed *[]string) *agentEnvelope {
	if input == nil {
		return nil
	}
	if input.NAT64Prefix != nil {
		prefix, _ := parseNAT64Prefix(*input.NAT64Prefix)
		if _, err := s.operations.SetManualNAT64(ctx, prefix); err != nil {
			return agentOperationFailure("NAT64 update failed", *completed)
		}
		*completed = append(*completed, "network.nat64")
	}
	if input.Resolvers != nil {
		if err := s.operations.UpdateResolvers(ctx, agentResolversToConfig(*input.Resolvers)); err != nil {
			return agentOperationFailure("resolver update failed", *completed)
		}
		*completed = append(*completed, "network.resolvers")
	}
	return nil
}

func (s *AgentService) applyNodes(ctx context.Context, plans []agentNodePlan, completed *[]string) *agentEnvelope {
	for _, plan := range plans {
		current := node.Node{}
		var err error
		if plan.existing == nil {
			current, err = s.nodes.Create(ctx, plan.configuration, plan.confirm)
		} else if !reflect.DeepEqual(plan.existing.Config, plan.configuration) {
			current, err = s.nodes.Update(ctx, plan.configuration.ID, plan.configuration, plan.confirm)
		} else {
			current = *plan.existing
		}
		if err != nil && !errors.Is(err, node.ErrPreviousRuntimeCleanup) {
			return agentOperationFailure("node configuration failed", *completed)
		}
		if current.Status != plan.status {
			if plan.status == node.StatusRunning {
				_, err = s.nodes.Start(ctx, plan.configuration.ID)
			} else {
				_, err = s.nodes.Stop(ctx, plan.configuration.ID)
			}
			if err != nil {
				return agentOperationFailure("node status convergence failed", *completed)
			}
		}
		*completed = append(*completed, "nodes."+plan.configuration.ID)
	}
	return nil
}

func (s *AgentService) pruneNodes(ctx context.Context, desired []agentNodeDocument, completed *[]string) *agentEnvelope {
	keep := make(map[string]struct{}, len(desired))
	for _, item := range desired {
		keep[item.ID] = struct{}{}
	}
	current := s.nodes.List()
	sort.Slice(current, func(i, j int) bool { return current[i].Config.ID < current[j].Config.ID })
	for _, item := range current {
		if _, exists := keep[item.Config.ID]; exists {
			continue
		}
		if err := s.nodes.Delete(ctx, item.Config.ID); err != nil {
			return agentOperationFailure("node prune failed", *completed)
		}
		*completed = append(*completed, "nodes.delete."+item.Config.ID)
	}
	return nil
}

func (s *AgentService) pruneResources(ctx context.Context, desired agentResourcesDocument, current ResourceSnapshot, completed *[]string) *agentEnvelope {
	keepPools, keepFixed, keepTemplates := make(map[string]struct{}), make(map[string]struct{}), make(map[string]struct{})
	for _, item := range desired.Pools {
		keepPools[item.Name] = struct{}{}
	}
	for _, item := range desired.Fixed {
		keepFixed[item.Name] = struct{}{}
	}
	for _, item := range desired.Templates {
		keepTemplates[item.Name] = struct{}{}
	}
	pools := append([]*ipv6resource.Pool(nil), current.Pools...)
	sort.Slice(pools, func(i, j int) bool { return pools[i] != nil && (pools[j] == nil || pools[i].Name < pools[j].Name) })
	for _, item := range pools {
		if item == nil {
			continue
		}
		if _, exists := keepPools[item.Name]; exists {
			continue
		}
		if err := s.resources.DeletePool(ctx, item.Name); err != nil {
			return agentOperationFailure("resource pool prune failed", *completed)
		}
		*completed = append(*completed, "resources.pool.delete."+item.Name)
	}
	fixed := append([]ipv6resource.FixedAddress(nil), current.Fixed...)
	sort.Slice(fixed, func(i, j int) bool { return fixed[i].Name < fixed[j].Name })
	for _, item := range fixed {
		if _, exists := keepFixed[item.Name]; exists {
			continue
		}
		if err := s.resources.DeleteFixedAddress(ctx, item.Name); err != nil {
			return agentOperationFailure("fixed resource prune failed", *completed)
		}
		*completed = append(*completed, "resources.fixed.delete."+item.Name)
	}
	templates := append([]ipv6resource.PrefixTemplate(nil), current.Templates...)
	sort.Slice(templates, func(i, j int) bool { return templates[i].Name < templates[j].Name })
	for _, item := range templates {
		if _, exists := keepTemplates[item.Name]; exists {
			continue
		}
		if err := s.resources.DeleteTemplate(ctx, item.Name); err != nil {
			return agentOperationFailure("resource template prune failed", *completed)
		}
		*completed = append(*completed, "resources.template.delete."+item.Name)
	}
	return nil
}

func agentPoolCapacity(item agentPoolDocument, settings config.Config) int {
	if item.Capacity != nil {
		return *item.Capacity
	}
	switch item.Kind {
	case ipv6resource.PoolInbound:
		return settings.Pools.Inbound
	case ipv6resource.PoolSharedOutbound:
		return settings.Pools.SharedOutbound
	default:
		return settings.Pools.DedicatedOutbound
	}
}

func agentResolversToConfig(values []agentResolverDocument) []config.Resolver {
	result := make([]config.Resolver, 0, len(values))
	for _, item := range values {
		result = append(result, config.Resolver{Name: item.Name, Address: item.Address, Port: item.Port, ServerName: item.ServerName, Enabled: item.Enabled})
	}
	return result
}

func agentOperationFailure(message string, completed []string) *agentEnvelope {
	result := agentFailure("operation_failed", message, map[string]any{"completed": append([]string(nil), completed...)})
	return &result
}

func resourceCounts(snapshot ResourceSnapshot) map[string]int {
	return map[string]int{"templates": len(snapshot.Templates), "fixed": len(snapshot.Fixed), "pools": len(snapshot.Pools)}
}

func preflightAgentPrunedResourceDependencies(input agentResourcesDocument) *agentEnvelope {
	templates := make(map[string]struct{}, len(input.Templates))
	for _, item := range input.Templates {
		templates[item.Name] = struct{}{}
	}
	fixed := make(map[string]struct{}, len(input.Fixed))
	for _, item := range input.Fixed {
		if _, exists := templates[item.Template]; !exists {
			result := agentFailure("invalid_document", "pruned fixed resource requires its template", map[string]any{"name": item.Name, "template": item.Template})
			return &result
		}
		fixed[item.Name] = struct{}{}
	}
	for _, item := range input.Pools {
		if _, exists := templates[item.Template]; !exists {
			result := agentFailure("invalid_document", "pruned resource pool requires its template", map[string]any{"name": item.Name, "template": item.Template})
			return &result
		}
		for _, pinned := range item.Pinned {
			if _, exists := fixed[pinned]; !exists {
				result := agentFailure("invalid_document", "pruned resource pool requires its pinned fixed resource", map[string]any{"name": item.Name, "fixed": pinned})
				return &result
			}
		}
	}
	return nil
}

func preflightAgentResources(input *agentResourcesDocument, current ResourceSnapshot, settings config.Config) *agentEnvelope {
	if input == nil {
		return nil
	}
	templates := make(map[string]ipv6resource.PrefixTemplate, len(current.Templates)+len(input.Templates))
	for _, existing := range current.Templates {
		templates[existing.Name] = existing
	}
	seenTemplates := make(map[string]struct{}, len(input.Templates))
	for _, item := range input.Templates {
		if _, duplicate := seenTemplates[item.Name]; duplicate {
			result := agentFailure("invalid_document", "duplicate resource template", map[string]any{"name": item.Name})
			return &result
		}
		seenTemplates[item.Name] = struct{}{}
		candidate, err := ipv6resource.NewPrefixTemplate(item.Name, item.Prefix, item.Interface, item.Mode)
		if err != nil {
			result := agentFailure("invalid_document", "invalid resource template", map[string]any{"name": item.Name})
			return &result
		}
		if existing, exists := templates[candidate.Name]; exists && existing != candidate {
			result := agentFailure("conflict", "resource template differs from existing definition", map[string]any{"name": candidate.Name})
			return &result
		}
		templates[candidate.Name] = candidate
	}
	fixedNames := make(map[string]agentFixedDocument, len(current.Fixed)+len(input.Fixed))
	fixedByAddress := make(map[netip.Addr]string, len(current.Fixed))
	for _, existing := range current.Fixed {
		fixedNames[existing.Name] = agentFixedDocument{Name: existing.Name, Template: existing.Template, Address: existing.Address.String()}
		if _, duplicate := fixedByAddress[existing.Address]; duplicate {
			result := agentFailure("operation_failed", "resource state cannot be compared", nil)
			return &result
		}
		fixedByAddress[existing.Address] = existing.Name
	}
	seenFixed := make(map[string]struct{}, len(input.Fixed))
	for _, item := range input.Fixed {
		if _, duplicate := seenFixed[item.Name]; duplicate {
			result := agentFailure("invalid_document", "duplicate fixed resource", map[string]any{"name": item.Name})
			return &result
		}
		seenFixed[item.Name] = struct{}{}
		if strings.TrimSpace(item.Name) == "" || templates[item.Template].Name == "" {
			result := agentFailure("invalid_document", "invalid fixed resource", map[string]any{"name": item.Name})
			return &result
		}
		if item.Address != "" {
			address, err := netip.ParseAddr(item.Address)
			if err != nil || !address.Is6() || address.Is4In6() || !templates[item.Template].Prefix.Contains(address) {
				result := agentFailure("invalid_document", "invalid fixed resource address", map[string]any{"name": item.Name})
				return &result
			}
			item.Address = address.String()
		}
		if existing, exists := fixedNames[item.Name]; exists && existing != item {
			result := agentFailure("conflict", "fixed resource differs from existing definition", map[string]any{"name": item.Name})
			return &result
		}
		fixedNames[item.Name] = item
	}
	poolNames := make(map[string]agentPoolDocument, len(current.Pools)+len(input.Pools))
	for _, existing := range current.Pools {
		if existing == nil {
			result := agentFailure("operation_failed", "resource state cannot be compared", nil)
			return &result
		}
		capacity := existing.Capacity
		pinned := make([]string, 0, len(existing.Pinned))
		for _, address := range existing.Pinned {
			name, exists := fixedByAddress[address]
			if !exists {
				result := agentFailure("operation_failed", "resource state cannot be compared", nil)
				return &result
			}
			pinned = append(pinned, name)
		}
		sort.Strings(pinned)
		poolNames[existing.Name] = agentPoolDocument{
			Name: existing.Name, Kind: existing.Kind, Template: existing.Template, Capacity: &capacity, Pinned: pinned,
		}
	}
	seenPools := make(map[string]struct{}, len(input.Pools))
	for _, item := range input.Pools {
		if _, duplicate := seenPools[item.Name]; duplicate {
			result := agentFailure("invalid_document", "duplicate resource pool", map[string]any{"name": item.Name})
			return &result
		}
		seenPools[item.Name] = struct{}{}
		if strings.TrimSpace(item.Name) == "" || templates[item.Template].Name == "" || !validAgentPoolKind(item.Kind) {
			result := agentFailure("invalid_document", "invalid resource pool", map[string]any{"name": item.Name})
			return &result
		}
		capacity := agentPoolCapacity(item, settings)
		if capacity < 1 || capacity > ipv6resource.MaxPoolSize {
			result := agentFailure("invalid_document", "invalid resource pool capacity", map[string]any{"name": item.Name})
			return &result
		}
		seenPinned := make(map[string]struct{}, len(item.Pinned))
		for _, pinned := range item.Pinned {
			fixed, exists := fixedNames[pinned]
			if !exists {
				result := agentFailure("invalid_document", "resource pool references missing fixed address", map[string]any{"name": item.Name, "fixed": pinned})
				return &result
			}
			if fixed.Template != item.Template {
				result := agentFailure("invalid_document", "resource pool references a fixed address from another template", map[string]any{"name": item.Name, "fixed": pinned})
				return &result
			}
			if _, duplicate := seenPinned[pinned]; duplicate {
				result := agentFailure("invalid_document", "resource pool pins a fixed address more than once", map[string]any{"name": item.Name, "fixed": pinned})
				return &result
			}
			seenPinned[pinned] = struct{}{}
		}
		if len(item.Pinned) > capacity {
			result := agentFailure("invalid_document", "resource pool pinned addresses exceed capacity", map[string]any{"name": item.Name})
			return &result
		}
		if existing, exists := poolNames[item.Name]; exists {
			pinned := append([]string(nil), item.Pinned...)
			sort.Strings(pinned)
			if existing.Kind != item.Kind || existing.Template != item.Template || existing.Capacity == nil || *existing.Capacity != capacity || !slices.Equal(existing.Pinned, pinned) {
				result := agentFailure("conflict", "resource pool differs from existing definition", map[string]any{"name": item.Name})
				return &result
			}
		}
	}
	return nil
}

type agentResourceCatalog struct {
	fixed map[string]struct{}
	pools map[string]ipv6resource.PoolKind
}

func preflightAgentNodeResources(document agentDocument, plans []agentNodePlan, current ResourceSnapshot, prune bool, service NodeService) *agentEnvelope {
	if document.Resources == nil && document.Nodes == nil {
		return nil
	}
	catalog := agentResourceCatalog{fixed: make(map[string]struct{}), pools: make(map[string]ipv6resource.PoolKind)}
	if !prune || document.Resources == nil {
		for _, item := range current.Fixed {
			catalog.fixed[item.Name] = struct{}{}
		}
		for _, item := range current.Pools {
			if item != nil {
				catalog.pools[item.Name] = item.Kind
			}
		}
	}
	if document.Resources != nil {
		for _, item := range document.Resources.Fixed {
			catalog.fixed[item.Name] = struct{}{}
		}
		for _, item := range document.Resources.Pools {
			catalog.pools[item.Name] = item.Kind
		}
	}

	effectiveNodes := make(map[string]node.Config)
	if !prune || document.Nodes == nil {
		for _, item := range service.List() {
			effectiveNodes[item.Config.ID] = item.Config
		}
	}
	for _, plan := range plans {
		effectiveNodes[plan.configuration.ID] = plan.configuration
	}
	ids := make([]string, 0, len(effectiveNodes))
	for id := range effectiveNodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		configuration := effectiveNodes[id]
		if !catalog.hasOutbound(configuration.Outbound) {
			result := agentFailure("invalid_document", "node references an invalid outbound resource", map[string]any{"id": id, "resource": configuration.Outbound})
			return &result
		}
		if configuration.InboundMode == node.InboundIPv6 || configuration.InboundMode == node.InboundDual {
			if !catalog.hasInbound(configuration.InboundResource) {
				result := agentFailure("invalid_document", "node references an invalid inbound resource", map[string]any{"id": id, "resource": configuration.InboundResource})
				return &result
			}
		}
	}
	return nil
}

func (c agentResourceCatalog) hasOutbound(name string) bool {
	_, fixed := c.fixed[name]
	kind, pool := c.pools[name]
	pool = pool && kind != ipv6resource.PoolInbound
	return fixed != pool
}

func (c agentResourceCatalog) hasInbound(name string) bool {
	_, fixed := c.fixed[name]
	kind, pool := c.pools[name]
	pool = pool && kind == ipv6resource.PoolInbound
	return fixed != pool
}

func preflightAgentNodes(input *[]agentNodeDocument, service NodeService) *agentEnvelope {
	if input == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(*input))
	for _, item := range *input {
		if strings.TrimSpace(item.ID) == "" {
			result := agentFailure("invalid_document", "node ID is required", nil)
			return &result
		}
		if _, duplicate := seen[item.ID]; duplicate {
			result := agentFailure("invalid_document", "duplicate node ID", map[string]any{"id": item.ID})
			return &result
		}
		seen[item.ID] = struct{}{}
		if item.DesiredStatus != node.StatusRunning && item.DesiredStatus != node.StatusStopped {
			result := agentFailure("invalid_document", "invalid desired node status", map[string]any{"id": item.ID})
			return &result
		}
		existing, exists := service.Get(item.ID)
		switch item.Authentication.Action {
		case "preserve":
			if !exists || item.Authentication.Username != "" || item.Authentication.Password != "" {
				result := agentFailure("invalid_document", "credential preserve requires an existing node", map[string]any{"id": item.ID})
				return &result
			}
		case "set":
			if item.Authentication.Username == "" || item.Authentication.Password == "" {
				result := agentFailure("invalid_document", "credential set requires username and password", map[string]any{"id": item.ID})
				return &result
			}
		case "generate":
			if item.Authentication.Username != "" || item.Authentication.Password != "" {
				result := agentFailure("invalid_document", "credential generation cannot include values", map[string]any{"id": item.ID})
				return &result
			}
		case "none":
			if !item.ConfirmUnauthenticated || item.Authentication.Username != "" || item.Authentication.Password != "" {
				result := agentFailure("invalid_document", "unauthenticated node requires explicit confirmation", map[string]any{"id": item.ID})
				return &result
			}
		default:
			result := agentFailure("invalid_document", "invalid node authentication action", map[string]any{"id": item.ID})
			return &result
		}
		_ = existing
	}
	return nil
}

func exportAgentResources(snapshot ResourceSnapshot) (*agentResourcesDocument, error) {
	result := &agentResourcesDocument{
		Templates: make([]agentTemplateDocument, 0, len(snapshot.Templates)),
		Fixed:     make([]agentFixedDocument, 0, len(snapshot.Fixed)),
		Pools:     make([]agentPoolDocument, 0, len(snapshot.Pools)),
	}
	fixedByAddress := make(map[netip.Addr]string, len(snapshot.Fixed))
	for _, item := range snapshot.Templates {
		result.Templates = append(result.Templates, agentTemplateDocument{Name: item.Name, Prefix: item.Prefix.String(), Interface: item.Interface, Mode: item.Mode})
	}
	for _, item := range snapshot.Fixed {
		result.Fixed = append(result.Fixed, agentFixedDocument{Name: item.Name, Template: item.Template, Address: item.Address.String()})
		if _, duplicate := fixedByAddress[item.Address]; duplicate {
			return nil, errors.New("more than one fixed resource uses an address")
		}
		fixedByAddress[item.Address] = item.Name
	}
	for _, item := range snapshot.Pools {
		if item == nil {
			return nil, errors.New("resource snapshot contains a nil pool")
		}
		capacity := item.Capacity
		pool := agentPoolDocument{Name: item.Name, Kind: item.Kind, Template: item.Template, Capacity: &capacity}
		for _, address := range item.Pinned {
			name, exists := fixedByAddress[address]
			if !exists {
				return nil, fmt.Errorf("pinned address %s has no fixed resource", address)
			}
			pool.Pinned = append(pool.Pinned, name)
		}
		result.Pools = append(result.Pools, pool)
	}
	sort.Slice(result.Templates, func(i, j int) bool { return result.Templates[i].Name < result.Templates[j].Name })
	sort.Slice(result.Fixed, func(i, j int) bool { return result.Fixed[i].Name < result.Fixed[j].Name })
	sort.Slice(result.Pools, func(i, j int) bool { return result.Pools[i].Name < result.Pools[j].Name })
	return result, nil
}

func changedStartupFields(before, after config.Config) []string {
	fields := make([]string, 0, 6)
	if before.Management.Port != after.Management.Port {
		fields = append(fields, "settings.management.port")
	}
	if before.Ports.Min != after.Ports.Min {
		fields = append(fields, "settings.ports.min")
	}
	if before.Ports.Max != after.Ports.Max {
		fields = append(fields, "settings.ports.max")
	}
	if before.Limits.MaxNodes != after.Limits.MaxNodes {
		fields = append(fields, "settings.limits.max_nodes")
	}
	if before.Timeouts.Dial != after.Timeouts.Dial {
		fields = append(fields, "settings.timeouts.dial")
	}
	return fields
}

func decodeAgentJSON(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func marshalAgentEnvelope(response agentEnvelope) (json.RawMessage, error) {
	return json.Marshal(response)
}

func agentFailure(code, message string, details map[string]any) agentEnvelope {
	return agentEnvelope{OK: false, Error: &agentError{Code: code, Message: message, Details: details}}
}

func validAgentPoolKind(kind ipv6resource.PoolKind) bool {
	switch kind {
	case ipv6resource.PoolInbound, ipv6resource.PoolSharedOutbound, ipv6resource.PoolDedicatedOutbound:
		return true
	default:
		return false
	}
}

func cloneAgentConfig(value config.Config) config.Config {
	value.Resolvers = append([]config.Resolver(nil), value.Resolvers...)
	return value
}
