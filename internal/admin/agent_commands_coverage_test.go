package admin

import (
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/config"
	"github.com/s12ryt/s12ryt-ipv6/internal/node"
)

func TestAgentServiceCoversRemainingResourceCommands(t *testing.T) {
	resources := &fakeResourceService{}
	service := newTestAgentService(t, &fakeAgentSettingsStore{current: config.Default()}, resources, &fakeNodeService{nodes: make(map[string]node.Node)}, &fakeOperationsService{})

	requests := []map[string]any{
		{"command": "resources.template.create", "arguments": map[string]any{"name": "edge", "prefix": "2001:4860:1::/120", "interface": "eth0", "mode": "address"}},
		{"command": "resources.pool.create", "arguments": map[string]any{"name": "shared", "kind": "shared-outbound", "template": "edge"}},
		{"command": "resources.pool.refresh", "arguments": map[string]any{"name": "shared"}},
		{"command": "resources.pool.force-drain", "arguments": map[string]any{"name": "shared", "batch": "batch-1"}, "yes": true},
		{"command": "resources.fixed.delete", "arguments": map[string]any{"name": "fixed-out"}, "yes": true},
		{"command": "resources.pool.delete", "arguments": map[string]any{"name": "shared"}, "yes": true},
	}
	for _, request := range requests {
		response := callAgent(t, service, request)
		if response["ok"] != true {
			t.Fatalf("%s response = %#v", request["command"], response)
		}
	}
	if resources.template.Name != "edge" || resources.poolCapacity != config.Default().Pools.SharedOutbound || resources.drainBatch != "batch-1" || resources.fixedName != "fixed-out" {
		t.Fatalf("resource captures = %#v", resources)
	}
}

func TestAgentServiceCoversRemainingNodeAndFolderCommands(t *testing.T) {
	nodes := &fakeNodeService{nodes: make(map[string]node.Node)}
	service := newTestAgentService(t, &fakeAgentSettingsStore{current: config.Default()}, &fakeResourceService{}, nodes, &fakeOperationsService{})

	create := callAgent(t, service, map[string]any{
		"command": "nodes.create", "show_secrets": true,
		"arguments": map[string]any{
			"id": "node-1", "name": "one", "protocol": "mixed", "folder": "old",
			"authentication": map[string]any{"action": "set", "username": "agent-user", "password": "agent-password-value"},
			"outbound":       "fixed-out", "inbound_mode": "ipv4", "desired_status": "stopped",
		},
	})
	if create["ok"] != true || nodes.nodes["node-1"].Status != node.StatusStopped {
		t.Fatalf("nodes.create = %#v, nodes=%#v", create, nodes.nodes)
	}

	for _, request := range []map[string]any{
		{"command": "nodes.list"},
		{"command": "nodes.start", "arguments": map[string]any{"id": "node-1"}},
		{"command": "nodes.stop", "arguments": map[string]any{"id": "node-1"}},
		{"command": "nodes.move", "arguments": map[string]any{"id": "node-1", "folder": "source"}},
		{"command": "folders.rename", "arguments": map[string]any{"source": "source", "target": "target"}},
		{"command": "folders.stop", "arguments": map[string]any{"folder": "target"}},
	} {
		response := callAgent(t, service, request)
		if response["ok"] != true {
			t.Fatalf("%s response = %#v", request["command"], response)
		}
	}

	update := callAgent(t, service, map[string]any{
		"command": "nodes.update",
		"arguments": map[string]any{
			"id": "node-1", "name": "renamed", "authentication": map[string]any{"action": "preserve"}, "desired_status": "running",
		},
	})
	if update["ok"] != true || nodes.nodes["node-1"].Config.Name != "renamed" || nodes.nodes["node-1"].Status != node.StatusRunning {
		t.Fatalf("nodes.update = %#v, node=%#v", update, nodes.nodes["node-1"])
	}

	batch := callAgent(t, service, map[string]any{
		"command": "nodes.batch-create",
		"arguments": map[string]any{"nodes": []any{
			map[string]any{
				"id": "node-2", "name": "two", "protocol": "http",
				"authentication": map[string]any{"action": "set", "username": "batch-user", "password": "batch-password-value"},
				"outbound":       "fixed-out", "inbound_mode": "ipv4", "desired_status": "running",
			},
			map[string]any{
				"id": "node-3", "name": "three", "protocol": "http",
				"authentication": map[string]any{"action": "none"}, "confirm_unauthenticated": true,
				"outbound": "fixed-out", "inbound_mode": "ipv4", "desired_status": "stopped",
			},
		}},
	})
	if batch["ok"] != true || !nodes.lastConfirmed || nodes.nodes["node-3"].Status != node.StatusStopped {
		t.Fatalf("nodes.batch-create = %#v, confirmed=%v nodes=%#v", batch, nodes.lastConfirmed, nodes.nodes)
	}

	deletedFolder := callAgent(t, service, map[string]any{"command": "folders.delete", "arguments": map[string]any{"folder": "target"}, "yes": true})
	if deletedFolder["ok"] != true {
		t.Fatalf("folders.delete = %#v", deletedFolder)
	}
	deletedNode := callAgent(t, service, map[string]any{"command": "nodes.delete", "arguments": map[string]any{"id": "node-2"}, "yes": true})
	if deletedNode["ok"] != true {
		t.Fatalf("nodes.delete = %#v", deletedNode)
	}
}

func TestAgentServiceCoversRemainingNetworkCommands(t *testing.T) {
	operations := &fakeOperationsService{}
	service := newTestAgentService(t, &fakeAgentSettingsStore{current: config.Default()}, &fakeResourceService{}, &fakeNodeService{nodes: make(map[string]node.Node)}, operations)

	requests := []map[string]any{
		{"command": "network.nat64.set", "arguments": map[string]any{"prefix": "64:ff9b::/96"}},
		{"command": "network.nat64.clear"},
		{"command": "network.resolvers.replace", "arguments": map[string]any{"resolvers": []any{map[string]any{
			"name": "Google", "address": "2001:4860:4860::6464", "port": 853, "server_name": "dns.google", "enabled": true,
		}}}},
	}
	for _, request := range requests {
		response := callAgent(t, service, request)
		if response["ok"] != true {
			t.Fatalf("%s response = %#v", request["command"], response)
		}
	}
	if operations.manualPrefix.IsValid() || len(operations.resolvers) != 1 || operations.resolvers[0].Name != "Google" {
		t.Fatalf("network captures = prefix %s resolvers %#v", operations.manualPrefix, operations.resolvers)
	}
}

func TestAgentServiceNoArgumentCommandsRejectUnexpectedArguments(t *testing.T) {
	service := newTestAgentService(t, &fakeAgentSettingsStore{current: config.Default()}, &fakeResourceService{}, &fakeNodeService{nodes: make(map[string]node.Node)}, &fakeOperationsService{})
	for _, request := range []map[string]any{
		{"command": "resources.list", "arguments": map[string]any{"unexpected": true}},
		{"command": "nodes.list", "arguments": map[string]any{"unexpected": true}},
		{"command": "network.show", "arguments": map[string]any{"unexpected": true}},
		{"command": "network.test", "arguments": map[string]any{"unexpected": true}},
		{"command": "network.nat64.clear", "arguments": map[string]any{"unexpected": true}},
		{"command": "logs.clear", "arguments": map[string]any{"unexpected": true}, "yes": true},
		{"command": "stats.show", "arguments": map[string]any{"unexpected": true}},
	} {
		response := callAgent(t, service, request)
		if response["ok"] != false || agentErrorCode(t, response) != "invalid_usage" {
			t.Fatalf("%s accepted unexpected arguments: %#v", request["command"], response)
		}
	}
}
