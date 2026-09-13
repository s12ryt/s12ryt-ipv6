package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const watchdogRestartStateSchemaVersion = 1

type watchdogRestartStateDocument struct {
	SchemaVersion int       `json:"schema_version"`
	Node          string    `json:"node"`
	Address       string    `json:"address"`
	Protocol      string    `json:"protocol"`
	RestartedAt   time.Time `json:"restarted_at"`
}

// FileWatchdogRestartStateStore atomically persists the watchdog's
// one-restart guard without proxy credentials.
type FileWatchdogRestartStateStore struct {
	path string
	mu   sync.Mutex
}

func NewFileWatchdogRestartStateStore(path string) (*FileWatchdogRestartStateStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("watchdog restart state path is required")
	}
	return &FileWatchdogRestartStateStore{path: path}, nil
}

func (s *FileWatchdogRestartStateStore) Load() (WatchdogRestartState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return WatchdogRestartState{}, false, nil
	}
	if err != nil {
		return WatchdogRestartState{}, false, fmt.Errorf("open watchdog restart state: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var document watchdogRestartStateDocument
	if err := decoder.Decode(&document); err != nil {
		return WatchdogRestartState{}, false, fmt.Errorf("decode watchdog restart state: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values are not allowed")
		}
		return WatchdogRestartState{}, false, fmt.Errorf("decode watchdog restart state trailer: %w", err)
	}
	if document.SchemaVersion != watchdogRestartStateSchemaVersion {
		return WatchdogRestartState{}, false, fmt.Errorf("watchdog restart state has unsupported schema version %d", document.SchemaVersion)
	}
	state := WatchdogRestartState{
		Node:        document.Node,
		Address:     document.Address,
		Protocol:    document.Protocol,
		RestartedAt: document.RestartedAt,
	}
	if err := validateWatchdogRestartState(state); err != nil {
		return WatchdogRestartState{}, false, fmt.Errorf("validate watchdog restart state: %w", err)
	}
	return state, true, nil
}

func (s *FileWatchdogRestartStateStore) Save(state WatchdogRestartState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validateWatchdogRestartState(state); err != nil {
		return fmt.Errorf("validate watchdog restart state: %w", err)
	}
	document := watchdogRestartStateDocument{
		SchemaVersion: watchdogRestartStateSchemaVersion,
		Node:          state.Node,
		Address:       state.Address,
		Protocol:      state.Protocol,
		RestartedAt:   state.RestartedAt,
	}
	contents, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode watchdog restart state: %w", err)
	}
	contents = append(contents, '\n')

	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create watchdog restart state directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".watchdog-restart-*")
	if err != nil {
		return fmt.Errorf("create temporary watchdog restart state: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set watchdog restart state permissions: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write watchdog restart state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync watchdog restart state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close watchdog restart state: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace watchdog restart state: %w", err)
	}
	committed = true
	return nil
}

func (s *FileWatchdogRestartStateStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear watchdog restart state: %w", err)
	}
	return nil
}

func validateWatchdogRestartState(state WatchdogRestartState) error {
	if strings.TrimSpace(state.Node) == "" {
		return errors.New("node is required")
	}
	if strings.TrimSpace(state.Address) == "" {
		return errors.New("address is required")
	}
	if strings.TrimSpace(state.Protocol) == "" {
		return errors.New("protocol is required")
	}
	if state.RestartedAt.IsZero() {
		return errors.New("restart time is required")
	}
	return nil
}
