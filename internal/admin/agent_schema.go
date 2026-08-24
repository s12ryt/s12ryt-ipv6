package admin

func agentDocumentSchema() map[string]any {
	stringValue := map[string]any{"type": "string"}
	positiveInteger := map[string]any{"type": "integer", "minimum": 1}
	port := map[string]any{"type": "integer", "minimum": 1, "maximum": 65535}
	duration := map[string]any{"type": "string", "minLength": 1}

	settings := strictAgentSchemaObject(map[string]any{
		"management": strictAgentSchemaObject(map[string]any{"port": port}),
		"ports":      strictAgentSchemaObject(map[string]any{"min": port, "max": port}),
		"pools": strictAgentSchemaObject(map[string]any{
			"inbound": positiveInteger, "shared_outbound": positiveInteger, "dedicated_outbound": positiveInteger,
		}),
		"limits": strictAgentSchemaObject(map[string]any{
			"max_nodes": positiveInteger, "tcp_per_node": positiveInteger, "udp_per_node": positiveInteger,
		}),
		"timeouts": strictAgentSchemaObject(map[string]any{
			"dial": duration, "handshake": duration, "tunnel_idle": duration, "udp_idle": duration,
		}),
		"allow_ula": map[string]any{"type": "boolean"},
	})

	template := strictAgentSchemaObject(map[string]any{
		"name":      stringValue,
		"prefix":    stringValue,
		"interface": stringValue,
		"mode":      map[string]any{"type": "string", "enum": []string{"address", "local-route-freebind", "external"}},
	}, "name", "prefix", "interface", "mode")
	fixed := strictAgentSchemaObject(map[string]any{
		"name": stringValue, "template": stringValue, "address": stringValue,
	}, "name", "template")
	pool := strictAgentSchemaObject(map[string]any{
		"name":     stringValue,
		"kind":     map[string]any{"type": "string", "enum": []string{"inbound", "shared-outbound", "dedicated-outbound"}},
		"template": stringValue,
		"capacity": map[string]any{"type": "integer", "minimum": 1, "maximum": 4096},
		"pinned":   map[string]any{"type": "array", "items": stringValue, "uniqueItems": true},
	}, "name", "kind", "template")
	resources := strictAgentSchemaObject(map[string]any{
		"templates": map[string]any{"type": "array", "items": template},
		"fixed":     map[string]any{"type": "array", "items": fixed},
		"pools":     map[string]any{"type": "array", "items": pool},
	})

	authentication := strictAgentSchemaObject(map[string]any{
		"action":   map[string]any{"type": "string", "enum": []string{"set", "generate", "preserve", "none"}},
		"username": stringValue,
		"password": stringValue,
	}, "action")
	node := strictAgentSchemaObject(map[string]any{
		"id":                      stringValue,
		"name":                    stringValue,
		"folder":                  stringValue,
		"protocol":                map[string]any{"type": "string", "enum": []string{"socks", "http", "mixed"}},
		"authentication":          authentication,
		"max_tcp":                 positiveInteger,
		"max_udp":                 positiveInteger,
		"dial_timeout":            duration,
		"handshake_timeout":       duration,
		"tunnel_idle_timeout":     duration,
		"udp_idle_timeout":        duration,
		"ula_override":            map[string]any{"type": "string", "enum": []string{"inherit", "allow", "deny"}},
		"outbound":                stringValue,
		"dedicated_pool":          stringValue,
		"port":                    port,
		"inbound_mode":            map[string]any{"type": "string", "enum": []string{"ipv4", "ipv6", "dual"}},
		"inbound_resource":        stringValue,
		"confirm_unauthenticated": map[string]any{"type": "boolean"},
		"desired_status":          map[string]any{"type": "string", "enum": []string{"running", "stopped"}},
	}, "id", "authentication", "desired_status")

	resolver := strictAgentSchemaObject(map[string]any{
		"name":        stringValue,
		"address":     stringValue,
		"port":        port,
		"server_name": stringValue,
		"enabled":     map[string]any{"type": "boolean"},
	}, "name", "address", "port", "server_name", "enabled")
	network := strictAgentSchemaObject(map[string]any{
		"nat64_prefix": stringValue,
		"resolvers":    map[string]any{"type": "array", "items": resolver},
	})

	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  "https://github.com/s12ryt/s12ryt-ipv6/schemas/agent-config-v1.json",
		"title":                "s12ryt-ipv6 agent configuration",
		"type":                 "object",
		"required":             []string{"schema_version"},
		"additionalProperties": false,
		"properties": map[string]any{
			"schema_version": map[string]any{"const": agentDocumentSchemaVersion},
			"settings":       settings,
			"resources":      resources,
			"nodes":          map[string]any{"type": "array", "items": node},
			"network":        network,
		},
	}
}

func strictAgentSchemaObject(properties map[string]any, required ...string) map[string]any {
	result := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
	}
	if len(required) != 0 {
		result["required"] = required
	}
	return result
}
