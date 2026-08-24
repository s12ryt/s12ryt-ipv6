package admin

import (
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/config"
	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/node"
	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
)

const agentDocumentSchemaVersion = 1

type agentDocument struct {
	SchemaVersion int                     `json:"schema_version" yaml:"schema_version"`
	Settings      *agentSettingsDocument  `json:"settings,omitempty" yaml:"settings,omitempty"`
	Resources     *agentResourcesDocument `json:"resources,omitempty" yaml:"resources,omitempty"`
	Nodes         *[]agentNodeDocument    `json:"nodes,omitempty" yaml:"nodes,omitempty"`
	Network       *agentNetworkDocument   `json:"network,omitempty" yaml:"network,omitempty"`
}

type agentSettingsDocument struct {
	Management *agentManagementSettings `json:"management,omitempty" yaml:"management,omitempty"`
	Ports      *agentPortSettings       `json:"ports,omitempty" yaml:"ports,omitempty"`
	Pools      *agentPoolDefaults       `json:"pools,omitempty" yaml:"pools,omitempty"`
	Limits     *agentLimitSettings      `json:"limits,omitempty" yaml:"limits,omitempty"`
	Timeouts   *agentTimeoutSettings    `json:"timeouts,omitempty" yaml:"timeouts,omitempty"`
	AllowULA   *bool                    `json:"allow_ula,omitempty" yaml:"allow_ula,omitempty"`
}

type agentManagementSettings struct {
	Port *uint16 `json:"port,omitempty" yaml:"port,omitempty"`
}

type agentPortSettings struct {
	Min *uint16 `json:"min,omitempty" yaml:"min,omitempty"`
	Max *uint16 `json:"max,omitempty" yaml:"max,omitempty"`
}

type agentPoolDefaults struct {
	Inbound           *int `json:"inbound,omitempty" yaml:"inbound,omitempty"`
	SharedOutbound    *int `json:"shared_outbound,omitempty" yaml:"shared_outbound,omitempty"`
	DedicatedOutbound *int `json:"dedicated_outbound,omitempty" yaml:"dedicated_outbound,omitempty"`
}

type agentLimitSettings struct {
	MaxNodes   *int `json:"max_nodes,omitempty" yaml:"max_nodes,omitempty"`
	TCPPerNode *int `json:"tcp_per_node,omitempty" yaml:"tcp_per_node,omitempty"`
	UDPPerNode *int `json:"udp_per_node,omitempty" yaml:"udp_per_node,omitempty"`
}

type agentTimeoutSettings struct {
	Dial       *string `json:"dial,omitempty" yaml:"dial,omitempty"`
	Handshake  *string `json:"handshake,omitempty" yaml:"handshake,omitempty"`
	TunnelIdle *string `json:"tunnel_idle,omitempty" yaml:"tunnel_idle,omitempty"`
	UDPIdle    *string `json:"udp_idle,omitempty" yaml:"udp_idle,omitempty"`
}

type agentResourcesDocument struct {
	Templates []agentTemplateDocument `json:"templates,omitempty" yaml:"templates,omitempty"`
	Fixed     []agentFixedDocument    `json:"fixed,omitempty" yaml:"fixed,omitempty"`
	Pools     []agentPoolDocument     `json:"pools,omitempty" yaml:"pools,omitempty"`
}

type agentTemplateDocument struct {
	Name      string                  `json:"name" yaml:"name"`
	Prefix    string                  `json:"prefix" yaml:"prefix"`
	Interface string                  `json:"interface" yaml:"interface"`
	Mode      ipv6resource.ConfigMode `json:"mode" yaml:"mode"`
}

type agentFixedDocument struct {
	Name     string `json:"name" yaml:"name"`
	Template string `json:"template" yaml:"template"`
	Address  string `json:"address,omitempty" yaml:"address,omitempty"`
}

type agentPoolDocument struct {
	Name     string                `json:"name" yaml:"name"`
	Kind     ipv6resource.PoolKind `json:"kind" yaml:"kind"`
	Template string                `json:"template" yaml:"template"`
	Capacity *int                  `json:"capacity,omitempty" yaml:"capacity,omitempty"`
	Pinned   []string              `json:"pinned,omitempty" yaml:"pinned,omitempty"`
}

type agentAuthenticationDocument struct {
	Action   string `json:"action" yaml:"action"`
	Username string `json:"username,omitempty" yaml:"username,omitempty"`
	Password string `json:"password,omitempty" yaml:"password,omitempty"`
}

type agentNodeDocument struct {
	ID                     string                      `json:"id" yaml:"id"`
	Name                   *string                     `json:"name,omitempty" yaml:"name,omitempty"`
	Folder                 *string                     `json:"folder,omitempty" yaml:"folder,omitempty"`
	Protocol               *node.Protocol              `json:"protocol,omitempty" yaml:"protocol,omitempty"`
	Authentication         agentAuthenticationDocument `json:"authentication" yaml:"authentication"`
	MaxTCP                 *int                        `json:"max_tcp,omitempty" yaml:"max_tcp,omitempty"`
	MaxUDP                 *int                        `json:"max_udp,omitempty" yaml:"max_udp,omitempty"`
	DialTimeout            *string                     `json:"dial_timeout,omitempty" yaml:"dial_timeout,omitempty"`
	HandshakeTimeout       *string                     `json:"handshake_timeout,omitempty" yaml:"handshake_timeout,omitempty"`
	TunnelIdleTimeout      *string                     `json:"tunnel_idle_timeout,omitempty" yaml:"tunnel_idle_timeout,omitempty"`
	UDPIdleTimeout         *string                     `json:"udp_idle_timeout,omitempty" yaml:"udp_idle_timeout,omitempty"`
	ULAOverride            *policy.ULAOverride         `json:"ula_override,omitempty" yaml:"ula_override,omitempty"`
	Outbound               *string                     `json:"outbound,omitempty" yaml:"outbound,omitempty"`
	DedicatedPool          *string                     `json:"dedicated_pool,omitempty" yaml:"dedicated_pool,omitempty"`
	Port                   *uint16                     `json:"port,omitempty" yaml:"port,omitempty"`
	InboundMode            *node.InboundMode           `json:"inbound_mode,omitempty" yaml:"inbound_mode,omitempty"`
	InboundResource        *string                     `json:"inbound_resource,omitempty" yaml:"inbound_resource,omitempty"`
	ConfirmUnauthenticated bool                        `json:"confirm_unauthenticated,omitempty" yaml:"confirm_unauthenticated,omitempty"`
	DesiredStatus          node.Status                 `json:"desired_status" yaml:"desired_status"`
}

type agentNetworkDocument struct {
	NAT64Prefix *string                  `json:"nat64_prefix,omitempty" yaml:"nat64_prefix,omitempty"`
	Resolvers   *[]agentResolverDocument `json:"resolvers,omitempty" yaml:"resolvers,omitempty"`
}

type agentResolverDocument struct {
	Name       string `json:"name" yaml:"name"`
	Address    string `json:"address" yaml:"address"`
	Port       uint16 `json:"port" yaml:"port"`
	ServerName string `json:"server_name" yaml:"server_name"`
	Enabled    bool   `json:"enabled" yaml:"enabled"`
}

func exportAgentSettings(value config.Config) *agentSettingsDocument {
	return &agentSettingsDocument{
		Management: &agentManagementSettings{Port: pointer(value.Management.Port)},
		Ports:      &agentPortSettings{Min: pointer(value.Ports.Min), Max: pointer(value.Ports.Max)},
		Pools: &agentPoolDefaults{
			Inbound: pointer(value.Pools.Inbound), SharedOutbound: pointer(value.Pools.SharedOutbound),
			DedicatedOutbound: pointer(value.Pools.DedicatedOutbound),
		},
		Limits: &agentLimitSettings{
			MaxNodes: pointer(value.Limits.MaxNodes), TCPPerNode: pointer(value.Limits.TCPPerNode), UDPPerNode: pointer(value.Limits.UDPPerNode),
		},
		Timeouts: &agentTimeoutSettings{
			Dial: pointer(value.Timeouts.Dial.String()), Handshake: pointer(value.Timeouts.Handshake.String()),
			TunnelIdle: pointer(value.Timeouts.TunnelIdle.String()), UDPIdle: pointer(value.Timeouts.UDPIdle.String()),
		},
		AllowULA: pointer(value.AllowULA),
	}
}

func exportAgentNetwork(value config.Config) *agentNetworkDocument {
	resolvers := make([]agentResolverDocument, 0, len(value.Resolvers))
	for _, resolver := range value.Resolvers {
		resolvers = append(resolvers, agentResolverDocument{
			Name: resolver.Name, Address: resolver.Address, Port: resolver.Port,
			ServerName: resolver.ServerName, Enabled: resolver.Enabled,
		})
	}
	prefix := value.NAT64Prefix
	return &agentNetworkDocument{NAT64Prefix: &prefix, Resolvers: &resolvers}
}

func exportAgentNode(current node.Node, showSecrets bool) agentNodeDocument {
	configuration := current.Config
	authentication := agentAuthenticationDocument{Action: "preserve"}
	if configuration.Username == "" {
		authentication.Action = "preserve"
	} else if showSecrets {
		authentication = agentAuthenticationDocument{Action: "set", Username: configuration.Username, Password: configuration.Password}
	}
	return agentNodeDocument{
		ID: configuration.ID, Name: pointer(configuration.Name), Folder: pointer(configuration.Folder),
		Protocol: pointer(configuration.Protocol), Authentication: authentication,
		MaxTCP: pointer(configuration.MaxTCP), MaxUDP: pointer(configuration.MaxUDP),
		DialTimeout: pointer(configuration.DialTimeout.String()), HandshakeTimeout: pointer(configuration.HandshakeTimeout.String()),
		TunnelIdleTimeout: pointer(configuration.TunnelIdleTimeout.String()), UDPIdleTimeout: pointer(configuration.UDPIdleTimeout.String()),
		ULAOverride: pointer(configuration.ULAOverride), Outbound: pointer(configuration.Outbound),
		DedicatedPool: pointer(configuration.DedicatedPool), Port: pointer(configuration.Port),
		InboundMode: pointer(configuration.InboundMode), InboundResource: pointer(configuration.InboundResource),
		ConfirmUnauthenticated: configuration.Username == "", DesiredStatus: current.Status,
	}
}

func mergeAgentSettings(current config.Config, input *agentSettingsDocument) (config.Config, error) {
	result := current
	result.Resolvers = append([]config.Resolver(nil), current.Resolvers...)
	if input == nil {
		return result, nil
	}
	if input.Management != nil && input.Management.Port != nil {
		result.Management.Port = *input.Management.Port
	}
	if input.Ports != nil {
		if input.Ports.Min != nil {
			result.Ports.Min = *input.Ports.Min
		}
		if input.Ports.Max != nil {
			result.Ports.Max = *input.Ports.Max
		}
	}
	if input.Pools != nil {
		if input.Pools.Inbound != nil {
			result.Pools.Inbound = *input.Pools.Inbound
		}
		if input.Pools.SharedOutbound != nil {
			result.Pools.SharedOutbound = *input.Pools.SharedOutbound
		}
		if input.Pools.DedicatedOutbound != nil {
			result.Pools.DedicatedOutbound = *input.Pools.DedicatedOutbound
		}
	}
	if input.Limits != nil {
		if input.Limits.MaxNodes != nil {
			result.Limits.MaxNodes = *input.Limits.MaxNodes
		}
		if input.Limits.TCPPerNode != nil {
			result.Limits.TCPPerNode = *input.Limits.TCPPerNode
		}
		if input.Limits.UDPPerNode != nil {
			result.Limits.UDPPerNode = *input.Limits.UDPPerNode
		}
	}
	if input.Timeouts != nil {
		values := []struct {
			name string
			raw  *string
			to   *time.Duration
		}{
			{name: "dial", raw: input.Timeouts.Dial, to: &result.Timeouts.Dial},
			{name: "handshake", raw: input.Timeouts.Handshake, to: &result.Timeouts.Handshake},
			{name: "tunnel_idle", raw: input.Timeouts.TunnelIdle, to: &result.Timeouts.TunnelIdle},
			{name: "udp_idle", raw: input.Timeouts.UDPIdle, to: &result.Timeouts.UDPIdle},
		}
		for _, value := range values {
			if value.raw == nil {
				continue
			}
			duration, err := time.ParseDuration(*value.raw)
			if err != nil {
				return config.Config{}, &agentValidationError{message: "invalid settings timeout", details: map[string]any{"field": "settings.timeouts." + value.name}}
			}
			*value.to = duration
		}
	}
	if input.AllowULA != nil {
		result.AllowULA = *input.AllowULA
	}
	if err := result.Validate(); err != nil {
		return config.Config{}, &agentValidationError{message: "invalid settings", details: map[string]any{"reason": err.Error()}}
	}
	return result, nil
}

func pointer[T any](value T) *T { return &value }
