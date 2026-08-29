package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/config"
	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/node"
	"github.com/s12ryt/s12ryt-ipv6/internal/secret"
)

type fakeAgentSettingsStore struct {
	current      config.Config
	replaceCount int
}

func (s *fakeAgentSettingsStore) Snapshot() config.Config {
	result := s.current
	result.Resolvers = append([]config.Resolver(nil), result.Resolvers...)
	return result
}

func (s *fakeAgentSettingsStore) Replace(candidate config.Config) error {
	s.replaceCount++
	s.current = candidate
	s.current.Resolvers = append([]config.Resolver(nil), candidate.Resolvers...)
	return nil
}

func newTestAgentService(t *testing.T, settings *fakeAgentSettingsStore, resources *fakeResourceService, nodes *fakeNodeService, operations *fakeOperationsService) *AgentService {
	t.Helper()
	service, err := NewAgentService(AgentServiceOptions{
		Settings: settings, ActiveSettings: settings.Snapshot(), Resources: resources,
		Nodes: nodes, Operations: operations, Health: func() HealthState { return HealthHealthy },
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func validAgentResourceSnapshot(t *testing.T) ResourceSnapshot {
	t.Helper()
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:4860:1::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	return ResourceSnapshot{
		Templates: []ipv6resource.PrefixTemplate{template},
		Pools: []*ipv6resource.Pool{
			{Name: "pool-in", Kind: ipv6resource.PoolInbound, Template: "edge", Capacity: 2},
			{Name: "shared-out", Kind: ipv6resource.PoolSharedOutbound, Template: "edge", Capacity: 2},
		},
	}
}

func callAgent(t *testing.T, service *AgentService, request any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.HandleAgent(context.Background(), encoded)
	if err != nil {
		t.Fatalf("HandleAgent() error = %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(response, &result); err != nil {
		t.Fatalf("decode response: %v; response=%s", err, response)
	}
	return result
}

func agentErrorCode(t *testing.T, response map[string]any) string {
	t.Helper()
	errorValue, ok := response["error"].(map[string]any)
	if !ok {
		t.Fatalf("response error = %#v", response["error"])
	}
	code, _ := errorValue["code"].(string)
	return code
}

func TestAgentServicePublishesDraft202012SchemaAndRejectsUnknownRequestFields(t *testing.T) {
	settings := &fakeAgentSettingsStore{current: config.Default()}
	service := newTestAgentService(t, settings, &fakeResourceService{}, &fakeNodeService{nodes: make(map[string]node.Node)}, &fakeOperationsService{})

	schema := callAgent(t, service, map[string]any{"command": "schema"})
	if schema["ok"] != true {
		t.Fatalf("schema response = %#v", schema)
	}
	data, ok := schema["data"].(map[string]any)
	if !ok || data["$schema"] != "https://json-schema.org/draft/2020-12/schema" || data["additionalProperties"] != false {
		t.Fatalf("schema data = %#v", schema["data"])
	}
	properties, ok := data["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v", data["properties"])
	}
	settingsSchema, _ := properties["settings"].(map[string]any)
	resourcesSchema, _ := properties["resources"].(map[string]any)
	nodesSchema, _ := properties["nodes"].(map[string]any)
	if settingsSchema["additionalProperties"] != false || resourcesSchema["additionalProperties"] != false {
		t.Fatalf("nested schemas are not strict: settings=%#v resources=%#v", settingsSchema, resourcesSchema)
	}
	nodeItems, _ := nodesSchema["items"].(map[string]any)
	nodeProperties, _ := nodeItems["properties"].(map[string]any)
	authentication, _ := nodeProperties["authentication"].(map[string]any)
	if nodeItems["additionalProperties"] != false || authentication["additionalProperties"] != false {
		t.Fatalf("node schema is not strict: node=%#v authentication=%#v", nodeItems, authentication)
	}

	invalid := callAgent(t, service, map[string]any{"command": "schema", "unknown": true})
	if invalid["ok"] != false || agentErrorCode(t, invalid) != "invalid_usage" {
		t.Fatalf("unknown request field response = %#v", invalid)
	}
}

func TestAgentServiceExportMasksSecretsAndCanRoundTripThroughDryRun(t *testing.T) {
	settings := &fakeAgentSettingsStore{current: config.Default()}
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:4860:1::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	resources := &fakeResourceService{snapshot: ResourceSnapshot{
		Templates: []ipv6resource.PrefixTemplate{template},
		Fixed:     []ipv6resource.FixedAddress{{Name: "fixed-out", Template: "edge", Address: netip.MustParseAddr("2001:4860:1::10"), Ownership: ipv6resource.OwnershipAddress}},
		Pools:     []*ipv6resource.Pool{{Name: "pool-in", Kind: ipv6resource.PoolInbound, Template: "edge", Capacity: 2}},
	}}
	nodeConfig := validAdminNodeConfig("node-1", "one")
	nodeConfig.Outbound = "fixed-out"
	nodeConfig.Username, nodeConfig.Password = "agent-user", "agent-password-value"
	nodes := &fakeNodeService{nodes: map[string]node.Node{"node-1": {Config: nodeConfig, Status: node.StatusRunning}}}
	service := newTestAgentService(t, settings, resources, nodes, &fakeOperationsService{})

	masked := callAgent(t, service, map[string]any{"command": "export"})
	maskedJSON, _ := json.Marshal(masked["data"])
	if strings.Contains(string(maskedJSON), "agent-user") || strings.Contains(string(maskedJSON), "agent-password-value") ||
		!strings.Contains(string(maskedJSON), `"action":"preserve"`) {
		t.Fatalf("masked export = %s", maskedJSON)
	}

	dryRun := callAgent(t, service, map[string]any{"command": "apply", "input": masked["data"], "dry_run": true})
	if dryRun["ok"] != true || settings.replaceCount != 0 || len(nodes.lastConfig.ID) != 0 {
		t.Fatalf("dry-run response=%#v settings replacements=%d node mutation=%#v", dryRun, settings.replaceCount, nodes.lastConfig)
	}

	visible := callAgent(t, service, map[string]any{"command": "export", "show_secrets": true})
	visibleJSON, _ := json.Marshal(visible["data"])
	if !strings.Contains(string(visibleJSON), "agent-user") || !strings.Contains(string(visibleJSON), "agent-password-value") ||
		!strings.Contains(string(visibleJSON), `"action":"set"`) {
		t.Fatalf("secret export = %s", visibleJSON)
	}
}

func TestAgentServiceApplyMergesSettingsAndReportsRestartRequiredFields(t *testing.T) {
	current := config.Default()
	settings := &fakeAgentSettingsStore{current: current}
	service := newTestAgentService(t, settings, &fakeResourceService{}, &fakeNodeService{nodes: make(map[string]node.Node)}, &fakeOperationsService{})

	response := callAgent(t, service, map[string]any{
		"command": "apply",
		"input": map[string]any{
			"schema_version": 1,
			"settings": map[string]any{
				"management": map[string]any{"port": 45555},
				"limits":     map[string]any{"tcp_per_node": 2048},
				"allow_ula":  true,
			},
		},
	})
	if response["ok"] != true || settings.replaceCount != 1 {
		t.Fatalf("apply response=%#v replaceCount=%d", response, settings.replaceCount)
	}
	got := settings.Snapshot()
	if got.Management.Port != 45555 || got.Limits.TCPPerNode != 2048 || !got.AllowULA || got.Ports != current.Ports {
		t.Fatalf("merged settings = %#v", got)
	}
	data, ok := response["data"].(map[string]any)
	if !ok || data["restart_required"] != true {
		t.Fatalf("apply data = %#v", response["data"])
	}
	fields, _ := data["restart_fields"].([]any)
	if len(fields) != 1 || fields[0] != "settings.management.port" {
		t.Fatalf("restart fields = %#v", fields)
	}
}

func TestAgentServicePreflightsWholeDocumentBeforeSettingsMutation(t *testing.T) {
	settings := &fakeAgentSettingsStore{current: config.Default()}
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:4860:1::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	resources := &fakeResourceService{snapshot: ResourceSnapshot{Templates: []ipv6resource.PrefixTemplate{template}}}
	service := newTestAgentService(t, settings, resources, &fakeNodeService{nodes: make(map[string]node.Node)}, &fakeOperationsService{})

	response := callAgent(t, service, map[string]any{
		"command": "apply",
		"input": map[string]any{
			"schema_version": 1,
			"settings":       map[string]any{"allow_ula": true},
			"resources": map[string]any{"templates": []any{
				map[string]any{"name": "edge", "prefix": "2001:4860:2::/120", "interface": "eth0", "mode": "address"},
			}},
		},
	})
	if response["ok"] != false || agentErrorCode(t, response) != "conflict" || settings.replaceCount != 0 {
		t.Fatalf("preflight response=%#v replaceCount=%d", response, settings.replaceCount)
	}
}

func TestAgentServiceRejectsUnknownDocumentFields(t *testing.T) {
	settings := &fakeAgentSettingsStore{current: config.Default()}
	service := newTestAgentService(t, settings, &fakeResourceService{}, &fakeNodeService{nodes: make(map[string]node.Node)}, &fakeOperationsService{})
	response := callAgent(t, service, map[string]any{
		"command": "apply",
		"input":   map[string]any{"schema_version": 1, "unknown": true},
	})
	if response["ok"] != false || agentErrorCode(t, response) != "invalid_document" || settings.replaceCount != 0 {
		t.Fatalf("invalid document response=%#v replaceCount=%d", response, settings.replaceCount)
	}
}

func TestAgentServiceApplyCreatesResourcesUpdatesNetworkAndConvergesNodes(t *testing.T) {
	settings := &fakeAgentSettingsStore{current: config.Default()}
	resources := &fakeResourceService{}
	nodes := &fakeNodeService{nodes: make(map[string]node.Node)}
	operations := &fakeOperationsService{}
	service := newTestAgentService(t, settings, resources, nodes, operations)

	response := callAgent(t, service, map[string]any{
		"command": "apply",
		"input": map[string]any{
			"schema_version": 1,
			"resources": map[string]any{
				"templates": []any{map[string]any{"name": "edge", "prefix": "2001:4860:1::/120", "interface": "eth0", "mode": "address"}},
				"fixed":     []any{map[string]any{"name": "fixed-out", "template": "edge", "address": "2001:4860:1::10"}},
				"pools":     []any{map[string]any{"name": "pool-in", "kind": "inbound", "template": "edge", "capacity": 2, "pinned": []any{"fixed-out"}}},
			},
			"network": map[string]any{
				"nat64_prefix": "64:ff9b::/96",
				"resolvers": []any{map[string]any{
					"name": "Google", "address": "2001:4860:4860::6464", "port": 853, "server_name": "dns.google", "enabled": true,
				}},
			},
			"nodes": []any{map[string]any{
				"id": "node-1", "name": "one", "protocol": "mixed",
				"authentication": map[string]any{"action": "set", "username": "agent-user", "password": "agent-password-value"},
				"outbound":       "fixed-out", "inbound_mode": "ipv6", "inbound_resource": "pool-in",
				"desired_status": "stopped",
			}},
		},
	})
	if response["ok"] != true {
		t.Fatalf("apply response = %#v", response)
	}
	if resources.template.Name != "edge" || resources.fixedName != "fixed-out" || resources.poolName != "pool-in" || resources.poolCapacity != 2 {
		t.Fatalf("resource mutations = %#v", resources)
	}
	if operations.manualPrefix.String() != "64:ff9b::/96" || len(operations.resolvers) != 1 || operations.resolvers[0].Name != "Google" {
		t.Fatalf("network mutations = prefix %s, resolvers %#v", operations.manualPrefix, operations.resolvers)
	}
	created, exists := nodes.nodes["node-1"]
	if !exists || created.Status != node.StatusStopped || created.Config.Username != "agent-user" || created.Config.MaxTCP != config.Default().Limits.TCPPerNode {
		t.Fatalf("created node = %#v", created)
	}
}

func TestAgentServiceApplyPreservesCredentialsAndConvergesExistingNode(t *testing.T) {
	settings := &fakeAgentSettingsStore{current: config.Default()}
	current := validAdminNodeConfig("node-1", "old")
	current.Username, current.Password = "keep-user", "keep-password-value"
	nodes := &fakeNodeService{nodes: map[string]node.Node{"node-1": {Config: current, Status: node.StatusRunning}}}
	service := newTestAgentService(t, settings, &fakeResourceService{snapshot: validAgentResourceSnapshot(t)}, nodes, &fakeOperationsService{})

	response := callAgent(t, service, map[string]any{
		"command": "apply",
		"input": map[string]any{
			"schema_version": 1,
			"nodes": []any{map[string]any{
				"id": "node-1", "name": "renamed", "authentication": map[string]any{"action": "preserve"}, "desired_status": "stopped",
			}},
		},
	})
	if response["ok"] != true {
		t.Fatalf("apply response = %#v", response)
	}
	updated := nodes.nodes["node-1"]
	if updated.Config.Name != "renamed" || updated.Config.Username != "keep-user" || updated.Config.Password != "keep-password-value" || updated.Status != node.StatusStopped {
		t.Fatalf("updated node = %#v", updated)
	}
}

func TestAgentServiceApplyPruneRequiresConfirmationAndOnlyTouchesExplicitSections(t *testing.T) {
	settings := &fakeAgentSettingsStore{current: config.Default()}
	first := validAdminNodeConfig("node-1", "one")
	second := validAdminNodeConfig("node-2", "two")
	nodes := &fakeNodeService{nodes: map[string]node.Node{
		"node-1": {Config: first, Status: node.StatusRunning},
		"node-2": {Config: second, Status: node.StatusRunning},
	}}
	resources := &fakeResourceService{snapshot: validAgentResourceSnapshot(t)}
	service := newTestAgentService(t, settings, resources, nodes, &fakeOperationsService{})
	document := map[string]any{
		"schema_version": 1,
		"nodes": []any{map[string]any{
			"id": "node-1", "authentication": map[string]any{"action": "preserve"}, "desired_status": "running",
		}},
	}

	rejected := callAgent(t, service, map[string]any{"command": "apply", "input": document, "prune": true})
	if rejected["ok"] != false || agentErrorCode(t, rejected) != "confirmation_required" || len(nodes.nodes) != 2 {
		t.Fatalf("unconfirmed prune = %#v, nodes=%#v", rejected, nodes.nodes)
	}
	applied := callAgent(t, service, map[string]any{"command": "apply", "input": document, "prune": true, "yes": true})
	if applied["ok"] != true || len(nodes.nodes) != 1 {
		t.Fatalf("confirmed prune = %#v, nodes=%#v", applied, nodes.nodes)
	}
	if resources.template.Name != "" || resources.fixedName != "" || resources.poolName != "" {
		t.Fatalf("omitted resources section was mutated: %#v", resources)
	}
}

func TestAgentServiceApplyStopsAtFirstExecutionFailureAndReportsCompletedItems(t *testing.T) {
	settings := &fakeAgentSettingsStore{current: config.Default()}
	resources := &fakeResourceService{operationErr: errors.New("sensitive kernel failure")}
	service := newTestAgentService(t, settings, resources, &fakeNodeService{nodes: make(map[string]node.Node)}, &fakeOperationsService{})
	response := callAgent(t, service, map[string]any{
		"command": "apply",
		"input": map[string]any{
			"schema_version": 1,
			"settings":       map[string]any{"allow_ula": true},
			"resources": map[string]any{"templates": []any{
				map[string]any{"name": "edge", "prefix": "2001:4860:1::/120", "interface": "eth0", "mode": "address"},
			}},
		},
	})
	if response["ok"] != false || agentErrorCode(t, response) != "operation_failed" || settings.replaceCount != 1 {
		t.Fatalf("failed apply = %#v, replacements=%d", response, settings.replaceCount)
	}
	errorValue := response["error"].(map[string]any)
	details := errorValue["details"].(map[string]any)
	completed, _ := details["completed"].([]any)
	if len(completed) != 1 || completed[0] != "settings" || strings.Contains(string(mustJSON(t, response)), "sensitive kernel failure") {
		t.Fatalf("failure details = %#v", details)
	}
}

func TestAgentServiceStatusReportsConfiguredAndActiveSettings(t *testing.T) {
	active := config.Default()
	configured := active
	configured.Management.Port = 45555
	settings := &fakeAgentSettingsStore{current: configured}
	service, err := NewAgentService(AgentServiceOptions{
		Settings: settings, ActiveSettings: active, Resources: &fakeResourceService{},
		Nodes: &fakeNodeService{nodes: make(map[string]node.Node)}, Operations: &fakeOperationsService{},
		Health: func() HealthState { return HealthDegraded },
	})
	if err != nil {
		t.Fatal(err)
	}
	response := callAgent(t, service, map[string]any{"command": "status"})
	if response["ok"] != true {
		t.Fatalf("status response = %#v", response)
	}
	data := response["data"].(map[string]any)
	if data["health"] != "degraded" || data["restart_required"] != true {
		t.Fatalf("status data = %#v", data)
	}
}

func TestAgentServiceApplyRejectsInvalidNodeResourceReferencesBeforeMutation(t *testing.T) {
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:4860:1::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	resources := &fakeResourceService{snapshot: ResourceSnapshot{
		Templates: []ipv6resource.PrefixTemplate{template},
		Pools: []*ipv6resource.Pool{
			{Name: "pool-in", Kind: ipv6resource.PoolInbound, Template: "edge", Capacity: 2},
			{Name: "shared-out", Kind: ipv6resource.PoolSharedOutbound, Template: "edge", Capacity: 2},
		},
	}}

	tests := []struct {
		name            string
		outbound        string
		inboundResource string
	}{
		{name: "missing outbound", outbound: "missing", inboundResource: "pool-in"},
		{name: "inbound pool used for outbound", outbound: "pool-in", inboundResource: "pool-in"},
		{name: "outbound pool used for inbound", outbound: "shared-out", inboundResource: "shared-out"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := &fakeAgentSettingsStore{current: config.Default()}
			nodes := &fakeNodeService{nodes: make(map[string]node.Node)}
			service := newTestAgentService(t, settings, resources, nodes, &fakeOperationsService{})
			response := callAgent(t, service, map[string]any{
				"command": "apply",
				"input": map[string]any{
					"schema_version": 1,
					"settings":       map[string]any{"allow_ula": true},
					"nodes": []any{map[string]any{
						"id": "node-1", "name": "one", "protocol": "mixed",
						"authentication":   map[string]any{"action": "set", "username": "agent-user", "password": "agent-password-value"},
						"outbound":         test.outbound,
						"inbound_mode":     "ipv6",
						"inbound_resource": test.inboundResource,
						"desired_status":   "stopped",
					}},
				},
			})
			if response["ok"] != false || agentErrorCode(t, response) != "invalid_document" || settings.replaceCount != 0 || len(nodes.nodes) != 0 {
				t.Fatalf("response=%#v replacements=%d nodes=%#v", response, settings.replaceCount, nodes.nodes)
			}
		})
	}
}

func TestAgentServiceApplyPruneProtectsResourcesUsedByRetainedNodes(t *testing.T) {
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:4860:1::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	resources := &fakeResourceService{snapshot: ResourceSnapshot{
		Templates: []ipv6resource.PrefixTemplate{template},
		Pools: []*ipv6resource.Pool{
			{Name: "pool-in", Kind: ipv6resource.PoolInbound, Template: "edge", Capacity: 2},
			{Name: "shared-out", Kind: ipv6resource.PoolSharedOutbound, Template: "edge", Capacity: 2},
		},
	}}
	configuration := validAdminNodeConfig("node-1", "one")
	nodes := &fakeNodeService{nodes: map[string]node.Node{"node-1": {Config: configuration, Status: node.StatusRunning}}}
	settings := &fakeAgentSettingsStore{current: config.Default()}
	service := newTestAgentService(t, settings, resources, nodes, &fakeOperationsService{})

	response := callAgent(t, service, map[string]any{
		"command": "apply", "prune": true, "yes": true,
		"input": map[string]any{
			"schema_version": 1,
			"resources": map[string]any{
				"templates": []any{map[string]any{"name": "edge", "prefix": "2001:4860:1::/120", "interface": "eth0", "mode": "address"}},
			},
		},
	})
	if response["ok"] != false || agentErrorCode(t, response) != "invalid_document" || settings.replaceCount != 0 || resources.poolName != "" || resources.template.Name != "" {
		t.Fatalf("response=%#v replacements=%d resources=%#v", response, settings.replaceCount, resources)
	}
}

// statefulAgentResourceService 模擬真實 ResourceCoordinator 的動態存在性：
// 已移除的池從快照消失，對它們呼叫 DeletePool 回與 ipv6resource.Store 相同的
// "does not exist" 錯誤。
type statefulAgentResourceService struct {
	fakeResourceService
	removedPools map[string]struct{}
}

func (s *statefulAgentResourceService) Snapshot() ResourceSnapshot {
	snapshot := s.fakeResourceService.Snapshot()
	pools := make([]*ipv6resource.Pool, 0, len(snapshot.Pools))
	for _, pool := range snapshot.Pools {
		if _, removed := s.removedPools[pool.Name]; removed {
			continue
		}
		pools = append(pools, pool)
	}
	snapshot.Pools = pools
	return snapshot
}

func (s *statefulAgentResourceService) DeletePool(_ context.Context, name string) error {
	if _, removed := s.removedPools[name]; removed {
		return fmt.Errorf("pool %q does not exist", name)
	}
	return s.fakeResourceService.DeletePool(context.Background(), name)
}

// dedicatedPoolNodeService 模擬 production node.Manager.Delete：刪除節點後
// 連帶清理該節點的專用出站池。
type dedicatedPoolNodeService struct {
	fakeNodeService
	deleteHook func(id string)
}

func (s *dedicatedPoolNodeService) Delete(ctx context.Context, id string) error {
	if err := s.fakeNodeService.Delete(ctx, id); err != nil {
		return err
	}
	if s.deleteHook != nil {
		s.deleteHook(id)
	}
	return nil
}

func TestAgentServiceApplyPruneToleratesPoolRemovedWithDedicatedNode(t *testing.T) {
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:4860:1::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	resources := &statefulAgentResourceService{
		fakeResourceService: fakeResourceService{snapshot: ResourceSnapshot{
			Templates: []ipv6resource.PrefixTemplate{template},
			Pools: []*ipv6resource.Pool{
				{Name: "node-1-outbound", Kind: ipv6resource.PoolDedicatedOutbound, Template: "edge", Capacity: 2},
				{Name: "shared-out", Kind: ipv6resource.PoolSharedOutbound, Template: "edge", Capacity: 2},
			},
		}},
		removedPools: make(map[string]struct{}),
	}
	configuration := validAdminNodeConfig("node-1", "one")
	configuration.Outbound = "node-1-outbound"
	configuration.DedicatedPool = "node-1-outbound"
	nodes := &dedicatedPoolNodeService{fakeNodeService: fakeNodeService{nodes: map[string]node.Node{
		"node-1": {Config: configuration, Status: node.StatusRunning},
	}}}
	nodes.deleteHook = func(string) {
		resources.removedPools["node-1-outbound"] = struct{}{}
	}
	settings := &fakeAgentSettingsStore{current: config.Default()}
	service, err := NewAgentService(AgentServiceOptions{
		Settings: settings, ActiveSettings: settings.Snapshot(), Resources: resources,
		Nodes: nodes, Operations: &fakeOperationsService{},
		Health: func() HealthState { return HealthHealthy },
	})
	if err != nil {
		t.Fatal(err)
	}

	response := callAgent(t, service, map[string]any{
		"command": "apply", "prune": true, "yes": true,
		"input": map[string]any{
			"schema_version": 1,
			"resources": map[string]any{
				"templates": []any{map[string]any{"name": "edge", "prefix": "2001:4860:1::/120", "interface": "eth0", "mode": "address"}},
				"pools":     []any{map[string]any{"name": "shared-out", "kind": "shared-outbound", "template": "edge", "capacity": 2}},
			},
			"nodes": []any{},
		},
	})
	if response["ok"] != true {
		t.Fatalf("apply prune response = %#v", response)
	}
	data := response["data"].(map[string]any)
	completed, _ := data["completed"].([]any)
	foundNodeDelete := false
	for _, item := range completed {
		if item == "nodes.delete.node-1" {
			foundNodeDelete = true
		}
	}
	if !foundNodeDelete {
		t.Fatalf("completed items missing node deletion: %#v", completed)
	}
	if resources.poolName == "shared-out" {
		t.Fatal("retained pool was deleted by prune")
	}
}

func TestAgentServiceApplyPruneRequiresCompleteResourceDependencyGraph(t *testing.T) {
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:4860:1::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := ResourceSnapshot{
		Templates: []ipv6resource.PrefixTemplate{template},
		Fixed: []ipv6resource.FixedAddress{{
			Name: "fixed-one", Template: "edge", Address: netip.MustParseAddr("2001:4860:1::10"),
		}},
		Pools: []*ipv6resource.Pool{{
			Name: "shared-out", Kind: ipv6resource.PoolSharedOutbound, Template: "edge", Capacity: 1,
			Pinned: []netip.Addr{netip.MustParseAddr("2001:4860:1::10")},
		}},
	}
	tests := []struct {
		name      string
		resources map[string]any
	}{
		{
			name: "fixed resource omits its template",
			resources: map[string]any{"fixed": []any{
				map[string]any{"name": "fixed-one", "template": "edge", "address": "2001:4860:1::10"},
			}},
		},
		{
			name: "pool omits its pinned fixed resource",
			resources: map[string]any{
				"templates": []any{map[string]any{"name": "edge", "prefix": "2001:4860:1::/120", "interface": "eth0", "mode": "address"}},
				"pools": []any{map[string]any{
					"name": "shared-out", "kind": "shared-outbound", "template": "edge", "capacity": 1, "pinned": []any{"fixed-one"},
				}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := &fakeAgentSettingsStore{current: config.Default()}
			resources := &fakeResourceService{snapshot: snapshot}
			service := newTestAgentService(t, settings, resources, &fakeNodeService{nodes: make(map[string]node.Node)}, &fakeOperationsService{})
			response := callAgent(t, service, map[string]any{
				"command": "apply", "prune": true, "yes": true,
				"input": map[string]any{
					"schema_version": 1,
					"settings":       map[string]any{"allow_ula": true},
					"resources":      test.resources,
				},
			})
			if response["ok"] != false || agentErrorCode(t, response) != "invalid_document" || settings.replaceCount != 0 || resources.template.Name != "" || resources.fixedName != "" || resources.poolName != "" {
				t.Fatalf("response=%#v settings=%d resources=%#v", response, settings.replaceCount, resources)
			}
		})
	}
}

func TestAgentServiceApplyComparesExistingPoolDefaultsAndPinnedResources(t *testing.T) {
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:4860:1::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	baseSnapshot := ResourceSnapshot{
		Templates: []ipv6resource.PrefixTemplate{template},
		Fixed: []ipv6resource.FixedAddress{
			{Name: "fixed-one", Template: "edge", Address: netip.MustParseAddr("2001:4860:1::10")},
			{Name: "fixed-two", Template: "edge", Address: netip.MustParseAddr("2001:4860:1::11")},
		},
		Pools: []*ipv6resource.Pool{{
			Name: "shared-out", Kind: ipv6resource.PoolSharedOutbound, Template: "edge",
			Capacity: config.Default().Pools.SharedOutbound, Pinned: []netip.Addr{netip.MustParseAddr("2001:4860:1::10")},
		}},
	}
	resourceDocument := func(pinned string) map[string]any {
		return map[string]any{
			"templates": []any{map[string]any{"name": "edge", "prefix": "2001:4860:1::/120", "interface": "eth0", "mode": "address"}},
			"fixed": []any{
				map[string]any{"name": "fixed-one", "template": "edge", "address": "2001:4860:1::10"},
				map[string]any{"name": "fixed-two", "template": "edge", "address": "2001:4860:1::11"},
			},
			"pools": []any{map[string]any{"name": "shared-out", "kind": "shared-outbound", "template": "edge", "pinned": []any{pinned}}},
		}
	}

	t.Run("omitted capacity uses candidate default", func(t *testing.T) {
		service := newTestAgentService(t, &fakeAgentSettingsStore{current: config.Default()}, &fakeResourceService{snapshot: baseSnapshot}, &fakeNodeService{nodes: make(map[string]node.Node)}, &fakeOperationsService{})
		response := callAgent(t, service, map[string]any{
			"command": "apply", "dry_run": true,
			"input": map[string]any{"schema_version": 1, "resources": resourceDocument("fixed-one")},
		})
		if response["ok"] != true {
			t.Fatalf("response = %#v", response)
		}
	})

	t.Run("different pinned resource conflicts", func(t *testing.T) {
		service := newTestAgentService(t, &fakeAgentSettingsStore{current: config.Default()}, &fakeResourceService{snapshot: baseSnapshot}, &fakeNodeService{nodes: make(map[string]node.Node)}, &fakeOperationsService{})
		response := callAgent(t, service, map[string]any{
			"command": "apply", "dry_run": true,
			"input": map[string]any{"schema_version": 1, "resources": resourceDocument("fixed-two")},
		})
		if response["ok"] != false || agentErrorCode(t, response) != "conflict" {
			t.Fatalf("response = %#v", response)
		}
	})
}

func TestAgentServiceApplyDefersCredentialGenerationUntilAfterDryRun(t *testing.T) {
	settings := &fakeAgentSettingsStore{current: config.Default()}
	resources := &fakeResourceService{snapshot: validAgentResourceSnapshot(t)}
	nodes := &fakeNodeService{nodes: make(map[string]node.Node)}
	generated := 0
	service, err := NewAgentService(AgentServiceOptions{
		Settings: settings, ActiveSettings: settings.Snapshot(), Resources: resources,
		Nodes: nodes, Operations: &fakeOperationsService{}, Health: func() HealthState { return HealthHealthy },
		GenerateCredentials: func() (secret.ProxyCredentials, error) {
			generated++
			return secret.ProxyCredentials{Username: "generated-user", Password: "generated-password-value"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	document := map[string]any{
		"schema_version": 1,
		"nodes": []any{map[string]any{
			"id": "node-1", "name": "one", "protocol": "mixed",
			"authentication": map[string]any{"action": "generate"},
			"outbound":       "shared-out", "inbound_mode": "ipv6", "inbound_resource": "pool-in",
			"desired_status": "stopped",
		}},
	}

	dryRun := callAgent(t, service, map[string]any{"command": "apply", "input": document, "dry_run": true})
	if dryRun["ok"] != true || generated != 0 || len(nodes.nodes) != 0 {
		t.Fatalf("dry-run response=%#v generated=%d nodes=%#v", dryRun, generated, nodes.nodes)
	}
	applied := callAgent(t, service, map[string]any{"command": "apply", "input": document})
	created := nodes.nodes["node-1"]
	if applied["ok"] != true || generated != 1 || created.Config.Username != "generated-user" || created.Config.Password != "generated-password-value" {
		t.Fatalf("apply response=%#v generated=%d node=%#v", applied, generated, created)
	}
}

func TestAgentServiceApplyCredentialGenerationFailureIsAtomic(t *testing.T) {
	settings := &fakeAgentSettingsStore{current: config.Default()}
	resources := &fakeResourceService{snapshot: validAgentResourceSnapshot(t)}
	nodes := &fakeNodeService{nodes: make(map[string]node.Node)}
	service, err := NewAgentService(AgentServiceOptions{
		Settings: settings, ActiveSettings: settings.Snapshot(), Resources: resources,
		Nodes: nodes, Operations: &fakeOperationsService{}, Health: func() HealthState { return HealthHealthy },
		GenerateCredentials: func() (secret.ProxyCredentials, error) {
			return secret.ProxyCredentials{}, errors.New("sensitive entropy failure")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := callAgent(t, service, map[string]any{
		"command": "apply",
		"input": map[string]any{
			"schema_version": 1,
			"settings":       map[string]any{"allow_ula": true},
			"nodes": []any{map[string]any{
				"id": "node-1", "name": "one", "protocol": "mixed",
				"authentication": map[string]any{"action": "generate"},
				"outbound":       "shared-out", "inbound_mode": "ipv6", "inbound_resource": "pool-in",
				"desired_status": "stopped",
			}},
		},
	})
	if response["ok"] != false || agentErrorCode(t, response) != "internal_error" || settings.replaceCount != 0 || len(nodes.nodes) != 0 || strings.Contains(string(mustJSON(t, response)), "sensitive entropy failure") {
		t.Fatalf("response=%#v replacements=%d nodes=%#v", response, settings.replaceCount, nodes.nodes)
	}
}

func TestAgentServiceResourceCommandsAndConfirmation(t *testing.T) {
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:4860:1::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	resources := &fakeResourceService{snapshot: ResourceSnapshot{Templates: []ipv6resource.PrefixTemplate{template}}}
	service := newTestAgentService(t, &fakeAgentSettingsStore{current: config.Default()}, resources, &fakeNodeService{nodes: make(map[string]node.Node)}, &fakeOperationsService{})

	listed := callAgent(t, service, map[string]any{"command": "resources.list"})
	if listed["ok"] != true || !strings.Contains(string(mustJSON(t, listed["data"])), `"name":"edge"`) {
		t.Fatalf("resources list = %#v", listed)
	}
	created := callAgent(t, service, map[string]any{
		"command":   "resources.fixed.create",
		"arguments": map[string]any{"name": "fixed-out", "template": "edge", "address": "2001:4860:1::10"},
	})
	if created["ok"] != true || resources.fixedName != "fixed-out" || resources.fixedAddress == nil || resources.fixedAddress.String() != "2001:4860:1::10" {
		t.Fatalf("fixed create = %#v, captured=%#v", created, resources)
	}

	rejected := callAgent(t, service, map[string]any{"command": "resources.template.delete", "arguments": map[string]any{"name": "edge"}})
	if rejected["ok"] != false || agentErrorCode(t, rejected) != "confirmation_required" {
		t.Fatalf("unconfirmed template delete = %#v", rejected)
	}
	deleted := callAgent(t, service, map[string]any{"command": "resources.template.delete", "arguments": map[string]any{"name": "edge"}, "yes": true})
	if deleted["ok"] != true || resources.template.Name != "edge" {
		t.Fatalf("confirmed template delete = %#v, captured=%#v", deleted, resources.template)
	}
}

func TestAgentServiceNodeCommandsMaskSecretsAndReportFolderFailures(t *testing.T) {
	first := validAdminNodeConfig("node-1", "one")
	first.Folder, first.Username, first.Password = "group", "agent-user", "agent-password-value"
	second := validAdminNodeConfig("node-2", "two")
	second.Folder = "group"
	nodes := &fakeNodeService{
		nodes: map[string]node.Node{
			"node-1": {Config: first, Status: node.StatusStopped},
			"node-2": {Config: second, Status: node.StatusStopped},
		},
		startErrors: map[string]error{"node-2": errors.New("sensitive runtime failure")},
	}
	service := newTestAgentService(t, &fakeAgentSettingsStore{current: config.Default()}, &fakeResourceService{}, nodes, &fakeOperationsService{})

	masked := callAgent(t, service, map[string]any{"command": "nodes.get", "arguments": map[string]any{"id": "node-1"}})
	maskedJSON := string(mustJSON(t, masked))
	if masked["ok"] != true || strings.Contains(maskedJSON, "agent-user") || strings.Contains(maskedJSON, "agent-password-value") || !strings.Contains(maskedJSON, `"action":"preserve"`) {
		t.Fatalf("masked node = %s", maskedJSON)
	}
	visible := callAgent(t, service, map[string]any{"command": "nodes.get", "arguments": map[string]any{"id": "node-1"}, "show_secrets": true})
	visibleJSON := string(mustJSON(t, visible))
	if visible["ok"] != true || !strings.Contains(visibleJSON, "agent-user") || !strings.Contains(visibleJSON, "agent-password-value") {
		t.Fatalf("visible node = %s", visibleJSON)
	}

	started := callAgent(t, service, map[string]any{"command": "folders.start", "arguments": map[string]any{"folder": "group"}})
	startedJSON := string(mustJSON(t, started))
	if started["ok"] != true || nodes.nodes["node-1"].Status != node.StatusRunning || !strings.Contains(startedJSON, `"node-2"`) || strings.Contains(startedJSON, "sensitive runtime failure") {
		t.Fatalf("folder start = %s, nodes=%#v", startedJSON, nodes.nodes)
	}

	rejected := callAgent(t, service, map[string]any{"command": "nodes.delete", "arguments": map[string]any{"id": "node-1"}})
	if rejected["ok"] != false || agentErrorCode(t, rejected) != "confirmation_required" {
		t.Fatalf("unconfirmed node delete = %#v", rejected)
	}
}

func TestAgentServiceOperationsCommandsAndDestructiveConfirmation(t *testing.T) {
	operations := &fakeOperationsService{connectivity: []ConnectivityCheck{{Name: "native", Kind: "ipv6", Success: true}}}
	service := newTestAgentService(t, &fakeAgentSettingsStore{current: config.Default()}, &fakeResourceService{}, &fakeNodeService{nodes: make(map[string]node.Node)}, operations)

	for _, command := range []string{"network.show", "network.test", "stats.show"} {
		response := callAgent(t, service, map[string]any{"command": command})
		if response["ok"] != true {
			t.Fatalf("%s response = %#v", command, response)
		}
	}
	logs := callAgent(t, service, map[string]any{
		"command":   "logs.tail",
		"arguments": map[string]any{"kind": "proxy", "node": "node-1", "action": "connect", "success": false, "limit": 25},
	})
	if logs["ok"] != true || operations.logFilter.Kind != "proxy" || operations.logFilter.Node != "node-1" || operations.logFilter.Success == nil || *operations.logFilter.Success || operations.logLimit != 25 {
		t.Fatalf("logs tail = %#v, filter=%#v limit=%d", logs, operations.logFilter, operations.logLimit)
	}

	for _, request := range []map[string]any{
		{"command": "logs.clear"},
		{"command": "stats.reset", "arguments": map[string]any{"node": "node-1"}},
		{"command": "resources.pool.force-drain", "arguments": map[string]any{"name": "pool", "batch": "batch-1"}},
	} {
		response := callAgent(t, service, request)
		if response["ok"] != false || agentErrorCode(t, response) != "confirmation_required" {
			t.Fatalf("unconfirmed destructive command = %#v", response)
		}
	}
	cleared := callAgent(t, service, map[string]any{"command": "logs.clear", "yes": true})
	reset := callAgent(t, service, map[string]any{"command": "stats.reset", "arguments": map[string]any{"node": "node-1"}, "yes": true})
	if cleared["ok"] != true || reset["ok"] != true || operations.clearedBy != "agent" || operations.resetNode != "node-1" {
		t.Fatalf("destructive operations clear=%#v reset=%#v captured=%#v", cleared, reset, operations)
	}
}

func TestAgentServiceCommandArgumentsAreStrictAndErrorsAreSanitized(t *testing.T) {
	operations := &fakeOperationsService{operationErr: errors.New("sensitive resolver failure")}
	service := newTestAgentService(t, &fakeAgentSettingsStore{current: config.Default()}, &fakeResourceService{}, &fakeNodeService{nodes: make(map[string]node.Node)}, operations)

	invalid := callAgent(t, service, map[string]any{
		"command":   "network.resolvers.replace",
		"arguments": map[string]any{"resolvers": []any{}, "unknown": true},
	})
	if invalid["ok"] != false || agentErrorCode(t, invalid) != "invalid_usage" {
		t.Fatalf("unknown argument response = %#v", invalid)
	}
	failed := callAgent(t, service, map[string]any{
		"command": "network.resolvers.replace",
		"arguments": map[string]any{"resolvers": []any{map[string]any{
			"name": "Google", "address": "2001:4860:4860::6464", "port": 853, "server_name": "dns.google", "enabled": true,
		}}},
	})
	if failed["ok"] != false || agentErrorCode(t, failed) != "operation_failed" || strings.Contains(string(mustJSON(t, failed)), "sensitive resolver failure") {
		t.Fatalf("sanitized operation failure = %#v", failed)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestNewAgentServiceValidatesDependencies(t *testing.T) {
	defaults := config.Default()
	valid := AgentServiceOptions{
		Settings: &fakeAgentSettingsStore{current: defaults}, ActiveSettings: defaults,
		Resources: &fakeResourceService{}, Nodes: &fakeNodeService{nodes: make(map[string]node.Node)},
		Operations: &fakeOperationsService{}, Health: func() HealthState { return HealthHealthy },
	}
	for name, mutate := range map[string]func(*AgentServiceOptions){
		"settings":   func(options *AgentServiceOptions) { options.Settings = nil },
		"resources":  func(options *AgentServiceOptions) { options.Resources = nil },
		"nodes":      func(options *AgentServiceOptions) { options.Nodes = nil },
		"operations": func(options *AgentServiceOptions) { options.Operations = nil },
		"health":     func(options *AgentServiceOptions) { options.Health = nil },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if _, err := NewAgentService(candidate); err == nil {
				t.Fatal("NewAgentService() error = nil")
			}
		})
	}
}
