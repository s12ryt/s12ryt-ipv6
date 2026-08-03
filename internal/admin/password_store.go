package admin

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const passwordStoreSchemaVersion = 1

type passwordDocument struct {
	SchemaVersion int    `yaml:"schema_version"`
	PasswordHash  string `yaml:"password_hash"`
}

type FilePasswordStore struct {
	path string
}

func NewFilePasswordStore(path string) (*FilePasswordStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("admin password store path is required")
	}
	return &FilePasswordStore{path: path}, nil
}

func (s *FilePasswordStore) Load() (string, bool, error) {
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("open admin password store: %w", err)
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var document passwordDocument
	if err := decoder.Decode(&document); err != nil {
		return "", false, fmt.Errorf("decode admin password store: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("multiple YAML documents are not allowed")
		}
		return "", false, fmt.Errorf("decode admin password store trailer: %w", err)
	}
	if document.SchemaVersion != passwordStoreSchemaVersion {
		return "", false, fmt.Errorf("unsupported admin password store schema version %d", document.SchemaVersion)
	}
	if strings.TrimSpace(document.PasswordHash) == "" {
		return "", false, errors.New("admin password hash is empty")
	}
	return document.PasswordHash, true, nil
}

func (s *FilePasswordStore) Save(hash string) error {
	if strings.TrimSpace(hash) == "" {
		return errors.New("admin password hash is empty")
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create admin password store directory: %w", err)
	}
	contents, err := yaml.Marshal(passwordDocument{SchemaVersion: passwordStoreSchemaVersion, PasswordHash: hash})
	if err != nil {
		return fmt.Errorf("encode admin password store: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".admin-password-*")
	if err != nil {
		return fmt.Errorf("create temporary admin password store: %w", err)
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
		return fmt.Errorf("set admin password store permissions: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write admin password store: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync admin password store: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close admin password store: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace admin password store: %w", err)
	}
	committed = true
	return nil
}
