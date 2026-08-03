package network

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const ownershipSchemaVersion = 1

type ownershipFile struct {
	SchemaVersion int          `yaml:"schema_version"`
	Addresses     []AddressRef `yaml:"addresses"`
	Routes        []RouteRef   `yaml:"routes"`
}

type FileOwnershipStore struct {
	path string
	mu   sync.Mutex
}

func NewFileOwnershipStore(path string) (*FileOwnershipStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("ownership path is required")
	}
	return &FileOwnershipStore{path: path}, nil
}

func (s *FileOwnershipStore) Load() (Ownership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Ownership{}, nil
	}
	if err != nil {
		return Ownership{}, fmt.Errorf("open ownership file: %w", err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var persisted ownershipFile
	if err := decoder.Decode(&persisted); err != nil {
		return Ownership{}, fmt.Errorf("decode ownership file: %w", err)
	}
	if persisted.SchemaVersion != ownershipSchemaVersion {
		return Ownership{}, fmt.Errorf("ownership schema version %d is unsupported", persisted.SchemaVersion)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Ownership{}, errors.New("ownership file contains trailing YAML documents")
		}
		return Ownership{}, fmt.Errorf("decode trailing ownership data: %w", err)
	}
	state := Ownership{Addresses: persisted.Addresses, Routes: persisted.Routes}
	if err := validateOwnership(state); err != nil {
		return Ownership{}, fmt.Errorf("validate ownership file: %w", err)
	}
	return cloneOwnership(state), nil
}

func (s *FileOwnershipStore) Save(state Ownership) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validateOwnership(state); err != nil {
		return fmt.Errorf("validate ownership: %w", err)
	}
	data, err := yaml.Marshal(ownershipFile{
		SchemaVersion: ownershipSchemaVersion,
		Addresses:     state.Addresses,
		Routes:        state.Routes,
	})
	if err != nil {
		return fmt.Errorf("encode ownership: %w", err)
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create ownership directory: %w", err)
	}
	temp, err := os.CreateTemp(directory, ".network-ownership-*")
	if err != nil {
		return fmt.Errorf("create temporary ownership file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("set ownership permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write ownership: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync ownership: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close ownership: %w", err)
	}
	if err := os.Rename(tempName, s.path); err != nil {
		return fmt.Errorf("replace ownership: %w", err)
	}
	return nil
}

func validateOwnership(state Ownership) error {
	addresses := make(map[AddressRef]struct{}, len(state.Addresses))
	for _, ref := range state.Addresses {
		if strings.TrimSpace(ref.Interface) == "" || !ref.Address.Is6() || ref.Address.Is4In6() {
			return fmt.Errorf("invalid owned address %s on %q", ref.Address, ref.Interface)
		}
		if _, exists := addresses[ref]; exists {
			return fmt.Errorf("duplicate owned address %s on %s", ref.Address, ref.Interface)
		}
		addresses[ref] = struct{}{}
	}
	routes := make(map[RouteRef]struct{}, len(state.Routes))
	for _, ref := range state.Routes {
		if strings.TrimSpace(ref.Interface) == "" || !ref.Prefix.IsValid() || !ref.Prefix.Addr().Is6() || ref.Prefix != ref.Prefix.Masked() {
			return fmt.Errorf("invalid owned route %s on %q", ref.Prefix, ref.Interface)
		}
		if _, exists := routes[ref]; exists {
			return fmt.Errorf("duplicate owned route %s on %s", ref.Prefix, ref.Interface)
		}
		routes[ref] = struct{}{}
	}
	return nil
}
