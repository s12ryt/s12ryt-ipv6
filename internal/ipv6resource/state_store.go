package ipv6resource

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

const stateSchemaVersion = 1

type stateFile struct {
	SchemaVersion int                `yaml:"schema_version"`
	Templates     []PrefixTemplate   `yaml:"templates"`
	Fixed         []FixedAddress     `yaml:"fixed"`
	Addresses     []CanonicalAddress `yaml:"addresses"`
	Pools         []Pool             `yaml:"pools"`
	NextBatch     uint64             `yaml:"next_batch"`
}

type FileStateStore struct {
	path string
	mu   sync.Mutex
}

func NewFileStateStore(path string) (*FileStateStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("resource state path is required")
	}
	return &FileStateStore{path: path}, nil
}

func (s *FileStateStore) Load() (State, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, fmt.Errorf("open resource state: %w", err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var persisted stateFile
	if err := decoder.Decode(&persisted); err != nil {
		return State{}, false, fmt.Errorf("decode resource state: %w", err)
	}
	if persisted.SchemaVersion != stateSchemaVersion {
		return State{}, false, fmt.Errorf("resource state schema version %d is unsupported", persisted.SchemaVersion)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return State{}, false, errors.New("resource state contains trailing YAML documents")
		}
		return State{}, false, fmt.Errorf("decode trailing resource state: %w", err)
	}
	state := stateFromFile(persisted)
	validated, err := NewStoreFromState(state)
	if err != nil {
		return State{}, false, fmt.Errorf("validate resource state: %w", err)
	}
	return validated.State(), true, nil
}

func (s *FileStateStore) Save(state State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	validated, err := NewStoreFromState(state)
	if err != nil {
		return fmt.Errorf("validate resource state: %w", err)
	}
	state = validated.State()
	data, err := yaml.Marshal(stateToFile(state))
	if err != nil {
		return fmt.Errorf("encode resource state: %w", err)
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create resource state directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".resources-*")
	if err != nil {
		return fmt.Errorf("create temporary resource state: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("set resource state permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write resource state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync resource state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close resource state: %w", err)
	}
	if err := os.Rename(temporaryName, s.path); err != nil {
		return fmt.Errorf("replace resource state: %w", err)
	}
	return nil
}

func stateToFile(state State) stateFile {
	return stateFile{
		SchemaVersion: stateSchemaVersion,
		Templates:     state.Templates, Fixed: state.Fixed, Addresses: state.Addresses,
		Pools: state.Pools, NextBatch: state.NextBatch,
	}
}

func stateFromFile(file stateFile) State {
	return State{
		Templates: file.Templates, Fixed: file.Fixed, Addresses: file.Addresses,
		Pools: file.Pools, NextBatch: file.NextBatch,
	}
}
