package admin

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/s12ryt/s12ryt-ipv6/internal/secret"
)

var ErrInvalidCurrentPassword = errors.New("invalid current admin password")

type PasswordHashStore interface {
	Load() (hash string, exists bool, err error)
	Save(hash string) error
}

type SessionRevoker interface {
	Revoke()
}

type PasswordManager struct {
	mu      sync.Mutex
	store   PasswordHashStore
	hasher  secret.PasswordHasher
	revoker SessionRevoker
	entropy io.Reader
}

func NewPasswordManager(store PasswordHashStore, hasher secret.PasswordHasher, revoker SessionRevoker, entropy io.Reader) (*PasswordManager, error) {
	if store == nil {
		return nil, errors.New("password hash store is required")
	}
	if revoker == nil {
		return nil, errors.New("session revoker is required")
	}
	if entropy == nil {
		entropy = rand.Reader
	}
	return &PasswordManager{store: store, hasher: hasher, revoker: revoker, entropy: entropy}, nil
}

func (m *PasswordManager) Initialize() (password string, created bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists, err := m.store.Load()
	if err != nil {
		return "", false, fmt.Errorf("load admin password hash: %w", err)
	}
	if exists {
		return "", false, nil
	}
	password, err = secret.GenerateAdminPassword(m.entropy)
	if err != nil {
		return "", false, fmt.Errorf("generate admin password: %w", err)
	}
	if err := m.savePassword(password); err != nil {
		return "", false, err
	}
	return password, true, nil
}

func (m *PasswordManager) Authenticate(password string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	hash, exists, err := m.store.Load()
	if err != nil {
		return false, fmt.Errorf("load admin password hash: %w", err)
	}
	if !exists {
		return false, errors.New("admin password is not initialized")
	}
	valid, err := m.hasher.Verify(password, hash)
	if err != nil {
		return false, fmt.Errorf("verify admin password: %w", err)
	}
	return valid, nil
}

func (m *PasswordManager) Change(current, replacement string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	hash, exists, err := m.store.Load()
	if err != nil {
		return fmt.Errorf("load admin password hash: %w", err)
	}
	if !exists {
		return errors.New("admin password is not initialized")
	}
	valid, err := m.hasher.Verify(current, hash)
	if err != nil {
		return fmt.Errorf("verify current admin password: %w", err)
	}
	if !valid {
		return ErrInvalidCurrentPassword
	}
	if err := m.savePassword(replacement); err != nil {
		return err
	}
	m.revoker.Revoke()
	return nil
}

func (m *PasswordManager) Reset(replacement string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if replacement == "" {
		var err error
		replacement, err = secret.GenerateAdminPassword(m.entropy)
		if err != nil {
			return "", fmt.Errorf("generate admin password: %w", err)
		}
	}
	if err := m.savePassword(replacement); err != nil {
		return "", err
	}
	m.revoker.Revoke()
	return replacement, nil
}

func (m *PasswordManager) savePassword(password string) error {
	hash, err := m.hasher.Hash(password, m.entropy)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	if err := m.store.Save(hash); err != nil {
		return fmt.Errorf("save admin password hash: %w", err)
	}
	return nil
}
