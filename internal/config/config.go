package config

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const CurrentSchemaVersion = 1

type Config struct {
	SchemaVersion int              `yaml:"schema_version"`
	Management    ManagementConfig `yaml:"management"`
	Ports         PortRange        `yaml:"ports"`
	Pools         PoolDefaults     `yaml:"pools"`
	Limits        Limits           `yaml:"limits"`
	Timeouts      TimeoutConfig    `yaml:"timeouts"`
	AllowULA      bool             `yaml:"allow_ula"`
	NAT64Prefix   string           `yaml:"nat64_prefix,omitempty"`
	Resolvers     []Resolver       `yaml:"resolvers"`
}

type ManagementConfig struct {
	Port uint16 `yaml:"port"`
}

type PortRange struct {
	Min uint16 `yaml:"min"`
	Max uint16 `yaml:"max"`
}

type PoolDefaults struct {
	Inbound           int `yaml:"inbound"`
	SharedOutbound    int `yaml:"shared_outbound"`
	DedicatedOutbound int `yaml:"dedicated_outbound"`
}

type Limits struct {
	MaxNodes   int `yaml:"max_nodes"`
	TCPPerNode int `yaml:"tcp_per_node"`
	UDPPerNode int `yaml:"udp_per_node"`
}

type TimeoutConfig struct {
	Dial       time.Duration `yaml:"-"`
	Handshake  time.Duration `yaml:"-"`
	TunnelIdle time.Duration `yaml:"-"`
	UDPIdle    time.Duration `yaml:"-"`
}

type Resolver struct {
	Name       string `yaml:"name"`
	Address    string `yaml:"address"`
	Port       uint16 `yaml:"port"`
	ServerName string `yaml:"server_name"`
	Enabled    bool   `yaml:"enabled"`
}

func Default() Config {
	return Config{
		SchemaVersion: CurrentSchemaVersion,
		Management:    ManagementConfig{Port: 34466},
		Ports:         PortRange{Min: 49152, Max: 65535},
		Pools:         PoolDefaults{Inbound: 10, SharedOutbound: 100, DedicatedOutbound: 15},
		Limits:        Limits{MaxNodes: 1024, TCPPerNode: 4096, UDPPerNode: 1024},
		Timeouts:      TimeoutConfig{Dial: 10 * time.Second, Handshake: 30 * time.Second, UDPIdle: 5 * time.Minute},
		Resolvers: []Resolver{
			{Name: "Cloudflare 1", Address: "2606:4700:4700::64", Port: 853, ServerName: "cloudflare-dns.com", Enabled: true},
			{Name: "Cloudflare 2", Address: "2606:4700:4700::6400", Port: 853, ServerName: "cloudflare-dns.com", Enabled: true},
			{Name: "Google 1", Address: "2001:4860:4860::6464", Port: 853, ServerName: "dns.google", Enabled: true},
			{Name: "Google 2", Address: "2001:4860:4860::64", Port: 853, ServerName: "dns.google", Enabled: true},
		},
	}
}

func (c Config) Validate() error {
	if c.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported schema version %d", c.SchemaVersion)
	}
	if c.Management.Port == 0 {
		return errors.New("management port must be between 1 and 65535")
	}
	if c.Ports.Min == 0 || c.Ports.Max == 0 || c.Ports.Min > c.Ports.Max {
		return errors.New("automatic port range is invalid")
	}
	for name, value := range map[string]int{
		"inbound": c.Pools.Inbound, "shared outbound": c.Pools.SharedOutbound, "dedicated outbound": c.Pools.DedicatedOutbound,
	} {
		if value < 1 || value > 4096 {
			return fmt.Errorf("%s pool default must be between 1 and 4096", name)
		}
	}
	if c.Limits.MaxNodes < 1 || c.Limits.MaxNodes > 1024 {
		return errors.New("max nodes must be between 1 and 1024")
	}
	if c.Limits.TCPPerNode < 1 || c.Limits.UDPPerNode < 1 {
		return errors.New("per-node connection limits must be positive")
	}
	if c.Timeouts.Dial <= 0 || c.Timeouts.Handshake <= 0 || c.Timeouts.UDPIdle <= 0 || c.Timeouts.TunnelIdle < 0 {
		return errors.New("timeouts are invalid")
	}
	if c.NAT64Prefix != "" {
		prefix, err := netip.ParsePrefix(c.NAT64Prefix)
		if err != nil || !prefix.Addr().Is6() || prefix.Addr().Is4In6() || prefix.Bits() != 96 || prefix != prefix.Masked() {
			return errors.New("NAT64 prefix must be a canonical IPv6 /96 prefix")
		}
	}

	enabled := 0
	for i, resolver := range c.Resolvers {
		if !resolver.Enabled {
			continue
		}
		enabled++
		address, err := netip.ParseAddr(resolver.Address)
		if err != nil || !address.Is6() || address.Is4In6() {
			return fmt.Errorf("resolver %d address must be a literal IPv6 address", i)
		}
		if resolver.Port == 0 || resolver.ServerName == "" {
			return fmt.Errorf("resolver %d requires port and TLS server name", i)
		}
	}
	if enabled == 0 {
		return errors.New("at least one enabled resolver is required")
	}
	return nil
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, errors.New("config contains multiple YAML documents")
		}
		return Config{}, fmt.Errorf("decode trailing config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}
	return config, nil
}

func Save(path string, config Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	contents, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure temporary config: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

type timeoutYAML struct {
	Dial       string `yaml:"dial"`
	Handshake  string `yaml:"handshake"`
	TunnelIdle string `yaml:"tunnel_idle"`
	UDPIdle    string `yaml:"udp_idle"`
}

func (t TimeoutConfig) MarshalYAML() (any, error) {
	return timeoutYAML{
		Dial: t.Dial.String(), Handshake: t.Handshake.String(), TunnelIdle: t.TunnelIdle.String(), UDPIdle: t.UDPIdle.String(),
	}, nil
}

func (t *TimeoutConfig) UnmarshalYAML(node *yaml.Node) error {
	var raw timeoutYAML
	if err := node.Decode(&raw); err != nil {
		return err
	}
	values := []struct {
		name string
		raw  string
		to   *time.Duration
	}{
		{name: "dial", raw: raw.Dial, to: &t.Dial},
		{name: "handshake", raw: raw.Handshake, to: &t.Handshake},
		{name: "tunnel_idle", raw: raw.TunnelIdle, to: &t.TunnelIdle},
		{name: "udp_idle", raw: raw.UDPIdle, to: &t.UDPIdle},
	}
	for _, value := range values {
		duration, err := time.ParseDuration(value.raw)
		if err != nil {
			return fmt.Errorf("parse %s timeout: %w", value.name, err)
		}
		*value.to = duration
	}
	return nil
}
