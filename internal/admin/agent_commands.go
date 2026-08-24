package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"sort"
	"strings"

	"github.com/s12ryt/s12ryt-ipv6/internal/eventlog"
	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/node"
)

func (s *AgentService) command(ctx context.Context, request agentRequest) agentEnvelope {
	if len(request.Input) != 0 || request.Prune || request.DryRun {
		return agentFailure("invalid_usage", "command received unsupported options", nil)
	}

	switch request.Command {
	case "resources.list":
		if response := validateAgentNoArgumentCommand(request, false, false); response != nil {
			return *response
		}
		return agentEnvelope{OK: true, Data: resourceSnapshotToDTO(s.resources.Snapshot())}
	case "resources.template.create":
		return s.createTemplate(ctx, request)
	case "resources.template.delete":
		return s.deleteTemplate(ctx, request)
	case "resources.fixed.create":
		return s.createFixed(ctx, request)
	case "resources.fixed.delete":
		return s.deleteFixed(ctx, request)
	case "resources.pool.create":
		return s.createPool(ctx, request)
	case "resources.pool.delete":
		return s.deletePool(ctx, request)
	case "resources.pool.refresh":
		return s.refreshPool(ctx, request)
	case "resources.pool.force-drain":
		return s.forceDrain(ctx, request)
	case "nodes.list":
		return s.listNodes(request)
	case "nodes.get":
		return s.getNode(request)
	case "nodes.create":
		return s.createNode(ctx, request)
	case "nodes.update":
		return s.updateNode(ctx, request)
	case "nodes.delete":
		return s.deleteNode(ctx, request)
	case "nodes.start":
		return s.nodeStatusCommand(ctx, request, node.StatusRunning)
	case "nodes.stop":
		return s.nodeStatusCommand(ctx, request, node.StatusStopped)
	case "nodes.batch-create":
		return s.createNodeBatch(ctx, request)
	case "nodes.move":
		return s.moveNode(ctx, request)
	case "folders.rename":
		return s.renameFolder(ctx, request)
	case "folders.start":
		return s.folderAction(ctx, request, "start")
	case "folders.stop":
		return s.folderAction(ctx, request, "stop")
	case "folders.delete":
		return s.folderAction(ctx, request, "delete")
	case "network.show":
		if response := validateAgentNoArgumentCommand(request, false, false); response != nil {
			return *response
		}
		return agentEnvelope{OK: true, Data: operationsSnapshotToDTO(s.operations.Overview())}
	case "network.test":
		return s.testNetwork(ctx, request)
	case "network.nat64.set":
		return s.setNAT64(ctx, request)
	case "network.nat64.clear":
		return s.clearNAT64(ctx, request)
	case "network.resolvers.replace":
		return s.replaceResolvers(ctx, request)
	case "logs.tail":
		return s.tailLogs(request)
	case "logs.clear":
		return s.clearLogs(request)
	case "stats.show":
		if response := validateAgentNoArgumentCommand(request, false, false); response != nil {
			return *response
		}
		return agentEnvelope{OK: true, Data: s.operations.Statistics()}
	case "stats.reset":
		return s.resetStatistics(request)
	default:
		return agentFailure("invalid_usage", "unsupported agent command", map[string]any{"command": request.Command})
	}
}

func (s *AgentService) createTemplate(ctx context.Context, request agentRequest) agentEnvelope {
	if response := validateAgentCommandOptions(request, false, false); response != nil {
		return *response
	}
	var input agentTemplateDocument
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	template, err := ipv6resource.NewPrefixTemplate(input.Name, input.Prefix, input.Interface, input.Mode)
	if err != nil {
		return agentFailure("invalid_usage", "invalid resource template", nil)
	}
	for _, current := range s.resources.Snapshot().Templates {
		if current.Name == template.Name {
			return agentFailure("conflict", "resource template already exists", nil)
		}
	}
	if err := s.resources.CreateTemplate(ctx, template); err != nil {
		return agentFailure("operation_failed", "resource template creation failed", nil)
	}
	return agentEnvelope{OK: true, Data: prefixTemplateToDTO(template)}
}

func (s *AgentService) deleteTemplate(ctx context.Context, request agentRequest) agentEnvelope {
	var input struct {
		Name string `json:"name"`
	}
	if response := decodeConfirmedAgentCommand(request, &input); response != nil {
		return *response
	}
	if strings.TrimSpace(input.Name) == "" {
		return agentFailure("invalid_usage", "resource template name is required", nil)
	}
	if err := s.resources.DeleteTemplate(ctx, input.Name); err != nil {
		return agentFailure("operation_failed", "resource template deletion failed", nil)
	}
	return agentEnvelope{OK: true, Data: map[string]any{"name": input.Name}}
}

func (s *AgentService) createFixed(ctx context.Context, request agentRequest) agentEnvelope {
	if response := validateAgentCommandOptions(request, false, false); response != nil {
		return *response
	}
	var input agentFixedDocument
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Template) == "" {
		return agentFailure("invalid_usage", "fixed resource name and template are required", nil)
	}
	var rawAddress *string
	if input.Address != "" {
		rawAddress = &input.Address
	}
	address, err := parseOptionalResourceAddress(rawAddress)
	if err != nil {
		return agentFailure("invalid_usage", "invalid fixed resource address", nil)
	}
	fixed, err := s.resources.CreateFixedAddress(ctx, input.Name, input.Template, address)
	if err != nil {
		return agentFailure("operation_failed", "fixed resource creation failed", nil)
	}
	return agentEnvelope{OK: true, Data: fixedAddressToDTO(fixed)}
}

func (s *AgentService) deleteFixed(ctx context.Context, request agentRequest) agentEnvelope {
	var input struct {
		Name string `json:"name"`
	}
	if response := decodeConfirmedAgentCommand(request, &input); response != nil {
		return *response
	}
	if strings.TrimSpace(input.Name) == "" {
		return agentFailure("invalid_usage", "fixed resource name is required", nil)
	}
	if err := s.resources.DeleteFixedAddress(ctx, input.Name); err != nil {
		return agentFailure("operation_failed", "fixed resource deletion failed", nil)
	}
	return agentEnvelope{OK: true, Data: map[string]any{"name": input.Name}}
}

func (s *AgentService) createPool(ctx context.Context, request agentRequest) agentEnvelope {
	if response := validateAgentCommandOptions(request, false, false); response != nil {
		return *response
	}
	var input agentPoolDocument
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Template) == "" || !validAgentPoolKind(input.Kind) {
		return agentFailure("invalid_usage", "invalid resource pool", nil)
	}
	capacity := agentPoolCapacity(input, s.settings.Snapshot())
	if capacity < 1 || capacity > ipv6resource.MaxPoolSize {
		return agentFailure("invalid_usage", "invalid resource pool capacity", nil)
	}
	pool, err := s.resources.CreatePool(ctx, input.Name, input.Kind, input.Template, capacity, input.Pinned)
	if err != nil {
		return agentFailure("operation_failed", "resource pool creation failed", nil)
	}
	return agentEnvelope{OK: true, Data: poolToDTO(pool)}
}

func (s *AgentService) deletePool(ctx context.Context, request agentRequest) agentEnvelope {
	var input struct {
		Name string `json:"name"`
	}
	if response := decodeConfirmedAgentCommand(request, &input); response != nil {
		return *response
	}
	if strings.TrimSpace(input.Name) == "" {
		return agentFailure("invalid_usage", "resource pool name is required", nil)
	}
	if err := s.resources.DeletePool(ctx, input.Name); err != nil {
		return agentFailure("operation_failed", "resource pool deletion failed", nil)
	}
	return agentEnvelope{OK: true, Data: map[string]any{"name": input.Name}}
}

func (s *AgentService) refreshPool(ctx context.Context, request agentRequest) agentEnvelope {
	if response := validateAgentCommandOptions(request, false, false); response != nil {
		return *response
	}
	var input struct {
		Name string `json:"name"`
	}
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	if strings.TrimSpace(input.Name) == "" {
		return agentFailure("invalid_usage", "resource pool name is required", nil)
	}
	pool, err := s.resources.RefreshPool(ctx, input.Name)
	if err != nil {
		return agentFailure("operation_failed", "resource pool refresh failed", nil)
	}
	return agentEnvelope{OK: true, Data: poolToDTO(pool)}
}

func (s *AgentService) forceDrain(ctx context.Context, request agentRequest) agentEnvelope {
	var input struct {
		Name  string `json:"name"`
		Batch string `json:"batch"`
	}
	if response := decodeConfirmedAgentCommand(request, &input); response != nil {
		return *response
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Batch) == "" {
		return agentFailure("invalid_usage", "resource pool and drain batch are required", nil)
	}
	if err := s.resources.ForceDrain(ctx, input.Name, input.Batch); err != nil {
		return agentFailure("operation_failed", "resource pool drain failed", nil)
	}
	return agentEnvelope{OK: true, Data: map[string]any{"name": input.Name, "batch": input.Batch}}
}

func (s *AgentService) listNodes(request agentRequest) agentEnvelope {
	if response := validateAgentNoArgumentCommand(request, true, false); response != nil {
		return *response
	}
	values := s.nodes.List()
	sort.Slice(values, func(i, j int) bool { return values[i].Config.ID < values[j].Config.ID })
	result := make([]agentNodeDocument, 0, len(values))
	for _, current := range values {
		result = append(result, exportAgentNode(current, request.ShowSecrets))
	}
	return agentEnvelope{OK: true, Data: result}
}

func (s *AgentService) getNode(request agentRequest) agentEnvelope {
	if response := validateAgentCommandOptions(request, true, false); response != nil {
		return *response
	}
	var input struct {
		ID string `json:"id"`
	}
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	current, exists := s.nodes.Get(input.ID)
	if !exists {
		return agentFailure("not_found", "node not found", nil)
	}
	return agentEnvelope{OK: true, Data: exportAgentNode(current, request.ShowSecrets)}
}

func (s *AgentService) createNode(ctx context.Context, request agentRequest) agentEnvelope {
	if response := validateAgentCommandOptions(request, true, false); response != nil {
		return *response
	}
	var input agentNodeDocument
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	if _, exists := s.nodes.Get(input.ID); exists {
		return agentFailure("conflict", "node already exists", nil)
	}
	return s.writeNode(ctx, request, input, false)
}

func (s *AgentService) updateNode(ctx context.Context, request agentRequest) agentEnvelope {
	if response := validateAgentCommandOptions(request, true, false); response != nil {
		return *response
	}
	var input agentNodeDocument
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	if _, exists := s.nodes.Get(input.ID); !exists {
		return agentFailure("not_found", "node not found", nil)
	}
	return s.writeNode(ctx, request, input, true)
}

func (s *AgentService) writeNode(ctx context.Context, request agentRequest, input agentNodeDocument, update bool) agentEnvelope {
	items := []agentNodeDocument{input}
	if response := preflightAgentNodes(&items, s.nodes); response != nil {
		return commandValidationFailure(*response)
	}
	plans, response := buildAgentNodePlans(&items, s.nodes, s.settings.Snapshot())
	if response != nil {
		return commandValidationFailure(*response)
	}
	plans, response = s.materializeAgentNodeCredentials(plans)
	if response != nil {
		return *response
	}
	plan := plans[0]
	var current node.Node
	var err error
	if update {
		current, err = s.nodes.Update(ctx, input.ID, plan.configuration, plan.confirm)
	} else {
		current, err = s.nodes.Create(ctx, plan.configuration, plan.confirm)
	}
	if err != nil && !errors.Is(err, node.ErrPreviousRuntimeCleanup) {
		return agentNodeCommandFailure(err)
	}
	if current.Status != plan.status {
		if plan.status == node.StatusRunning {
			current, err = s.nodes.Start(ctx, input.ID)
		} else {
			current, err = s.nodes.Stop(ctx, input.ID)
		}
		if err != nil {
			return agentNodeCommandFailure(err)
		}
	}
	return agentEnvelope{OK: true, Data: exportAgentNode(current, request.ShowSecrets)}
}

func (s *AgentService) deleteNode(ctx context.Context, request agentRequest) agentEnvelope {
	var input struct {
		ID string `json:"id"`
	}
	if response := decodeConfirmedAgentCommand(request, &input); response != nil {
		return *response
	}
	if strings.TrimSpace(input.ID) == "" {
		return agentFailure("invalid_usage", "node ID is required", nil)
	}
	if err := s.nodes.Delete(ctx, input.ID); err != nil {
		return agentNodeCommandFailure(err)
	}
	return agentEnvelope{OK: true, Data: map[string]any{"id": input.ID}}
}

func (s *AgentService) nodeStatusCommand(ctx context.Context, request agentRequest, status node.Status) agentEnvelope {
	if response := validateAgentCommandOptions(request, false, false); response != nil {
		return *response
	}
	var input struct {
		ID string `json:"id"`
	}
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	var current node.Node
	var err error
	if status == node.StatusRunning {
		current, err = s.nodes.Start(ctx, input.ID)
	} else {
		current, err = s.nodes.Stop(ctx, input.ID)
	}
	if err != nil {
		return agentNodeCommandFailure(err)
	}
	return agentEnvelope{OK: true, Data: exportAgentNode(current, false)}
}

func (s *AgentService) createNodeBatch(ctx context.Context, request agentRequest) agentEnvelope {
	if response := validateAgentCommandOptions(request, true, false); response != nil {
		return *response
	}
	var input struct {
		Nodes []agentNodeDocument `json:"nodes"`
	}
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	if len(input.Nodes) == 0 || len(input.Nodes) > node.MaxBatchCreate {
		return agentFailure("invalid_usage", "invalid node batch size", nil)
	}
	for _, item := range input.Nodes {
		if _, exists := s.nodes.Get(item.ID); exists {
			return agentFailure("conflict", "node already exists", nil)
		}
	}
	if response := preflightAgentNodes(&input.Nodes, s.nodes); response != nil {
		return commandValidationFailure(*response)
	}
	plans, response := buildAgentNodePlans(&input.Nodes, s.nodes, s.settings.Snapshot())
	if response != nil {
		return commandValidationFailure(*response)
	}
	plans, response = s.materializeAgentNodeCredentials(plans)
	if response != nil {
		return *response
	}
	configs := make([]node.Config, 0, len(plans))
	confirm := false
	for _, plan := range plans {
		configs = append(configs, plan.configuration)
		confirm = confirm || plan.confirm
	}
	created, err := s.nodes.CreateBatch(ctx, configs, confirm)
	if err != nil {
		return agentNodeCommandFailure(err)
	}
	byID := make(map[string]node.Node, len(created))
	for _, current := range created {
		byID[current.Config.ID] = current
	}
	for _, plan := range plans {
		current := byID[plan.configuration.ID]
		if current.Status == plan.status {
			continue
		}
		if plan.status == node.StatusRunning {
			current, err = s.nodes.Start(ctx, plan.configuration.ID)
		} else {
			current, err = s.nodes.Stop(ctx, plan.configuration.ID)
		}
		if err != nil {
			return agentNodeCommandFailure(err)
		}
		byID[plan.configuration.ID] = current
	}
	result := make([]agentNodeDocument, 0, len(plans))
	for _, plan := range plans {
		result = append(result, exportAgentNode(byID[plan.configuration.ID], request.ShowSecrets))
	}
	return agentEnvelope{OK: true, Data: result}
}

func (s *AgentService) moveNode(ctx context.Context, request agentRequest) agentEnvelope {
	if response := validateAgentCommandOptions(request, false, false); response != nil {
		return *response
	}
	var input struct {
		ID     string `json:"id"`
		Folder string `json:"folder"`
	}
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	folder, err := node.NormalizeFolderName(input.Folder)
	if err != nil || strings.TrimSpace(input.ID) == "" {
		return agentFailure("invalid_usage", "invalid node folder", nil)
	}
	current, err := s.nodes.MoveToFolder(ctx, input.ID, folder)
	if err != nil {
		return agentNodeCommandFailure(err)
	}
	return agentEnvelope{OK: true, Data: exportAgentNode(current, false)}
}

func (s *AgentService) renameFolder(ctx context.Context, request agentRequest) agentEnvelope {
	if response := validateAgentCommandOptions(request, false, false); response != nil {
		return *response
	}
	var input folderRenameDTO
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	renamed, err := s.nodes.RenameFolder(ctx, input.Source, input.Target)
	if err != nil {
		return agentNodeCommandFailure(err)
	}
	sort.Slice(renamed, func(i, j int) bool { return renamed[i].Config.ID < renamed[j].Config.ID })
	result := make([]agentNodeDocument, 0, len(renamed))
	for _, current := range renamed {
		result = append(result, exportAgentNode(current, false))
	}
	return agentEnvelope{OK: true, Data: result}
}

func (s *AgentService) folderAction(ctx context.Context, request agentRequest, action string) agentEnvelope {
	if response := validateAgentCommandOptions(request, false, action == "delete"); response != nil {
		return *response
	}
	if action == "delete" && !request.Yes {
		return agentFailure("confirmation_required", "folder deletion requires --yes", nil)
	}
	var input struct {
		Folder string `json:"folder"`
	}
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	folder, err := node.NormalizeFolderName(input.Folder)
	if err != nil || folder == "" {
		return agentFailure("invalid_usage", "invalid node folder", nil)
	}
	members := make([]node.Node, 0)
	for _, current := range s.nodes.List() {
		if current.Config.Folder == folder {
			members = append(members, current)
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Config.ID < members[j].Config.ID })
	if len(members) == 0 {
		return agentFailure("not_found", "node folder not found", nil)
	}
	result := folderActionResponse{Succeeded: make([]string, 0), Failed: make([]folderActionFailure, 0)}
	for _, current := range members {
		var actionErr error
		switch action {
		case "start":
			_, actionErr = s.nodes.Start(ctx, current.Config.ID)
		case "stop":
			_, actionErr = s.nodes.Stop(ctx, current.Config.ID)
		case "delete":
			actionErr = s.nodes.Delete(ctx, current.Config.ID)
		}
		if actionErr != nil {
			result.Failed = append(result.Failed, folderActionFailure{ID: current.Config.ID, Error: "node operation failed"})
		} else {
			result.Succeeded = append(result.Succeeded, current.Config.ID)
		}
	}
	return agentEnvelope{OK: true, Data: result}
}

func (s *AgentService) testNetwork(ctx context.Context, request agentRequest) agentEnvelope {
	if response := validateAgentNoArgumentCommand(request, false, false); response != nil {
		return *response
	}
	checks, err := s.operations.TestConnectivity(ctx)
	if err != nil {
		return agentFailure("operation_failed", "network connectivity test failed", nil)
	}
	return agentEnvelope{OK: true, Data: checks}
}

func (s *AgentService) setNAT64(ctx context.Context, request agentRequest) agentEnvelope {
	if response := validateAgentCommandOptions(request, false, false); response != nil {
		return *response
	}
	var input struct {
		Prefix string `json:"prefix"`
	}
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	prefix, err := parseNAT64Prefix(input.Prefix)
	if err != nil || !prefix.IsValid() {
		return agentFailure("invalid_usage", "invalid NAT64 configuration", nil)
	}
	status, err := s.operations.SetManualNAT64(ctx, prefix)
	if err != nil {
		return agentFailure("operation_failed", "NAT64 update failed", nil)
	}
	return agentEnvelope{OK: true, Data: nat64StatusToDTO(status)}
}

func (s *AgentService) clearNAT64(ctx context.Context, request agentRequest) agentEnvelope {
	if response := validateAgentNoArgumentCommand(request, false, false); response != nil {
		return *response
	}
	status, err := s.operations.SetManualNAT64(ctx, netip.Prefix{})
	if err != nil {
		return agentFailure("operation_failed", "NAT64 update failed", nil)
	}
	return agentEnvelope{OK: true, Data: nat64StatusToDTO(status)}
}

func (s *AgentService) replaceResolvers(ctx context.Context, request agentRequest) agentEnvelope {
	if response := validateAgentCommandOptions(request, false, false); response != nil {
		return *response
	}
	var input struct {
		Resolvers []agentResolverDocument `json:"resolvers"`
	}
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	resolvers := agentResolversToConfig(input.Resolvers)
	if validateResolvers(resolvers) != nil {
		return agentFailure("invalid_usage", "invalid resolver configuration", nil)
	}
	if err := s.operations.UpdateResolvers(ctx, resolvers); err != nil {
		return agentFailure("operation_failed", "resolver update failed", nil)
	}
	return agentEnvelope{OK: true, Data: map[string]any{"resolvers": input.Resolvers}}
}

func (s *AgentService) tailLogs(request agentRequest) agentEnvelope {
	if response := validateAgentCommandOptions(request, false, false); response != nil {
		return *response
	}
	var input struct {
		Kind    eventlog.Kind `json:"kind,omitempty"`
		Node    string        `json:"node,omitempty"`
		Action  string        `json:"action,omitempty"`
		Success *bool         `json:"success,omitempty"`
		Limit   int           `json:"limit,omitempty"`
	}
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	if len(input.Node) > 128 || len(input.Action) > 128 {
		return agentFailure("invalid_usage", "invalid log filter", nil)
	}
	switch input.Kind {
	case "", eventlog.KindProxy, eventlog.KindSystem, eventlog.KindAudit:
	default:
		return agentFailure("invalid_usage", "invalid log filter", nil)
	}
	if input.Limit == 0 {
		input.Limit = 200
	}
	if input.Limit < 1 || input.Limit > 1000 {
		return agentFailure("invalid_usage", "invalid log limit", nil)
	}
	events, err := s.operations.TailLogs(eventlog.Filter{Kind: input.Kind, Node: input.Node, Action: input.Action, Success: input.Success}, input.Limit)
	if err != nil {
		return agentFailure("operation_failed", "log query failed", nil)
	}
	return agentEnvelope{OK: true, Data: events}
}

func (s *AgentService) clearLogs(request agentRequest) agentEnvelope {
	if response := validateAgentNoArgumentCommand(request, false, true); response != nil {
		return *response
	}
	if !request.Yes {
		return agentFailure("confirmation_required", "log clearing requires --yes", nil)
	}
	if err := s.operations.ClearLogs("agent"); err != nil {
		return agentFailure("operation_failed", "log clearing failed", nil)
	}
	return agentEnvelope{OK: true, Data: map[string]any{"cleared": true}}
}

func (s *AgentService) resetStatistics(request agentRequest) agentEnvelope {
	if response := validateAgentCommandOptions(request, false, true); response != nil {
		return *response
	}
	if !request.Yes {
		return agentFailure("confirmation_required", "statistics reset requires --yes", nil)
	}
	var input struct {
		Node string `json:"node,omitempty"`
	}
	if response := decodeAgentCommandArguments(request.Arguments, &input); response != nil {
		return *response
	}
	if len(input.Node) > 128 {
		return agentFailure("invalid_usage", "invalid statistics node", nil)
	}
	if err := s.operations.ResetStatistics(input.Node); err != nil {
		return agentFailure("operation_failed", "statistics reset failed", nil)
	}
	return agentEnvelope{OK: true, Data: map[string]any{"node": input.Node}}
}

func validateAgentCommandOptions(request agentRequest, allowShowSecrets, allowYes bool) *agentEnvelope {
	if request.ShowSecrets && !allowShowSecrets {
		result := agentFailure("invalid_usage", "command does not support show_secrets", nil)
		return &result
	}
	if request.Yes && !allowYes {
		result := agentFailure("invalid_usage", "command does not support confirmation", nil)
		return &result
	}
	return nil
}

func validateAgentNoArgumentCommand(request agentRequest, allowShowSecrets, allowYes bool) *agentEnvelope {
	if response := validateAgentCommandOptions(request, allowShowSecrets, allowYes); response != nil {
		return response
	}
	if len(request.Arguments) != 0 {
		result := agentFailure("invalid_usage", "command does not accept arguments", nil)
		return &result
	}
	return nil
}

func decodeAgentCommandArguments(payload json.RawMessage, destination any) *agentEnvelope {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if err := decodeAgentJSON(payload, destination); err != nil {
		result := agentFailure("invalid_usage", "invalid command arguments", nil)
		return &result
	}
	return nil
}

func decodeConfirmedAgentCommand(request agentRequest, destination any) *agentEnvelope {
	if response := validateAgentCommandOptions(request, false, true); response != nil {
		return response
	}
	if !request.Yes {
		result := agentFailure("confirmation_required", "destructive command requires --yes", nil)
		return &result
	}
	return decodeAgentCommandArguments(request.Arguments, destination)
}

func commandValidationFailure(response agentEnvelope) agentEnvelope {
	if response.Error != nil && response.Error.Code == "invalid_document" {
		response.Error.Code = "invalid_usage"
	}
	return response
}

func agentNodeCommandFailure(err error) agentEnvelope {
	switch {
	case errors.Is(err, node.ErrNodeNotFound), errors.Is(err, node.ErrFolderNotFound):
		return agentFailure("not_found", "node or folder not found", nil)
	case errors.Is(err, node.ErrNodeExists), errors.Is(err, node.ErrNodeLimit), errors.Is(err, node.ErrFolderExists):
		return agentFailure("conflict", "node conflicts with existing state", nil)
	case errors.Is(err, node.ErrBatchSize):
		return agentFailure("invalid_usage", "invalid node batch", nil)
	case errors.Is(err, node.ErrUnauthenticatedRiskConfirmation):
		return agentFailure("confirmation_required", "unauthenticated proxy risk confirmation required", nil)
	default:
		return agentFailure("operation_failed", "node operation failed", nil)
	}
}
