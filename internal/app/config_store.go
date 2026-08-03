package app

import (
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"strings"
	"sync"

	"github.com/s12ryt/s12ryt-ipv6/internal/config"
)

// ConfigStore serializes runtime configuration changes with their durable copy.
type ConfigStore struct {
	mu          sync.RWMutex
	path        string
	config      config.Config
	initialized bool
}

func NewConfigStore(path string) (*ConfigStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("config path is required")
	}
	return &ConfigStore{path: path}, nil
}

func (s *ConfigStore) LoadOrCreate() (config.Config, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized {
		return cloneConfig(s.config), false, nil
	}

	loaded, err := config.Load(s.path)
	created := false
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return config.Config{}, false, err
		}
		loaded = config.Default()
		if err := config.Save(s.path, loaded); err != nil {
			return config.Config{}, false, fmt.Errorf("create default config: %w", err)
		}
		created = true
	}
	s.config = cloneConfig(loaded)
	s.initialized = true
	return cloneConfig(s.config), created, nil
}

func (s *ConfigStore) Snapshot() config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.config)
}

func (s *ConfigStore) SaveNAT64(prefix netip.Prefix) error {
	return s.update(func(candidate *config.Config) {
		if prefix.IsValid() {
			candidate.NAT64Prefix = prefix.String()
		} else {
			candidate.NAT64Prefix = ""
		}
	})
}

func (s *ConfigStore) SaveResolvers(resolvers []config.Resolver) error {
	return s.update(func(candidate *config.Config) {
		candidate.Resolvers = append([]config.Resolver(nil), resolvers...)
	})
}

func (s *ConfigStore) SaveManagementPort(port uint16) error {
	return s.update(func(candidate *config.Config) {
		candidate.Management.Port = port
	})
}

func (s *ConfigStore) update(change func(*config.Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		return errors.New("config store is not initialized")
	}
	candidate := cloneConfig(s.config)
	change(&candidate)
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("validate config update: %w", err)
	}
	if err := config.Save(s.path, candidate); err != nil {
		return fmt.Errorf("save config update: %w", err)
	}
	s.config = candidate
	return nil
}

func cloneConfig(value config.Config) config.Config {
	value.Resolvers = append([]config.Resolver(nil), value.Resolvers...)
	return value
}
