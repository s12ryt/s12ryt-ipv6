package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultIsValidAndMatchesContract(t *testing.T) {
	cfg := Default()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Default().Validate() error = %v", err)
	}
	if cfg.Management.Port != 34466 {
		t.Errorf("management port = %d, want 34466", cfg.Management.Port)
	}
	if cfg.Ports.Min != 49152 || cfg.Ports.Max != 65535 {
		t.Errorf("port range = %d-%d, want 49152-65535", cfg.Ports.Min, cfg.Ports.Max)
	}
	if cfg.Pools.Inbound != 10 || cfg.Pools.SharedOutbound != 100 || cfg.Pools.DedicatedOutbound != 15 {
		t.Errorf("pool defaults = %#v", cfg.Pools)
	}
	if cfg.Limits.MaxNodes != 1024 || cfg.Limits.TCPPerNode != 4096 || cfg.Limits.UDPPerNode != 1024 {
		t.Errorf("limits = %#v", cfg.Limits)
	}
	if cfg.Timeouts.Dial != 10*time.Second || cfg.Timeouts.Handshake != 30*time.Second || cfg.Timeouts.UDPIdle != 5*time.Minute {
		t.Errorf("timeouts = %#v", cfg.Timeouts)
	}
	if len(cfg.Resolvers) != 4 || cfg.Resolvers[0].ServerName != "cloudflare-dns.com" {
		t.Errorf("resolvers = %#v", cfg.Resolvers)
	}
	if cfg.NAT64Prefix != "" {
		t.Errorf("default NAT64 prefix = %q, want automatic discovery", cfg.NAT64Prefix)
	}
	wantResolverAddresses := []string{
		"2606:4700:4700::64",
		"2606:4700:4700::6400",
		"2001:4860:4860::6464",
		"2001:4860:4860::64",
	}
	for i, want := range wantResolverAddresses {
		if cfg.Resolvers[i].Address != want {
			t.Errorf("resolver %d address = %q, want DNS64 address %q", i, cfg.Resolvers[i].Address, want)
		}
	}
}

func TestValidateRejectsUnsafeBounds(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "management port", mutate: func(c *Config) { c.Management.Port = 0 }},
		{name: "port range order", mutate: func(c *Config) { c.Ports.Min, c.Ports.Max = 60000, 50000 }},
		{name: "pool too large", mutate: func(c *Config) { c.Pools.SharedOutbound = 4097 }},
		{name: "too many nodes", mutate: func(c *Config) { c.Limits.MaxNodes = 1025 }},
		{name: "no resolver", mutate: func(c *Config) { c.Resolvers = nil }},
		{name: "resolver is IPv4", mutate: func(c *Config) { c.Resolvers[0].Address = "1.1.1.1" }},
		{name: "resolver missing TLS name", mutate: func(c *Config) { c.Resolvers[0].ServerName = "" }},
		{name: "NAT64 prefix is not /96", mutate: func(c *Config) { c.NAT64Prefix = "64:ff9b::/64" }},
		{name: "NAT64 prefix is not canonical", mutate: func(c *Config) { c.NAT64Prefix = "64:ff9b::1/96" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want validation error")
			}
		})
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	want := Default()
	want.Management.Port = 35555
	want.AllowULA = true
	want.NAT64Prefix = "64:ff9b::/96"
	want.Timeouts.TunnelIdle = 2 * time.Minute

	if err := Save(path, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Management.Port != want.Management.Port || got.AllowULA != want.AllowULA || got.NAT64Prefix != want.NAT64Prefix || got.Timeouts.TunnelIdle != want.Timeouts.TunnelIdle {
		t.Fatalf("Load() = %#v, want selected fields from %#v", got, want)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := []byte("schema_version: 1\nunknown_setting: true\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want unknown field error")
	}
}
