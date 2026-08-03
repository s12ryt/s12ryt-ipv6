package admin

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/secret"
)

type memoryPasswordStore struct {
	hash      string
	exists    bool
	loadError error
	saveError error
	saves     int
}

func (s *memoryPasswordStore) Load() (string, bool, error) {
	return s.hash, s.exists, s.loadError
}

func (s *memoryPasswordStore) Save(hash string) error {
	if s.saveError != nil {
		return s.saveError
	}
	s.hash = hash
	s.exists = true
	s.saves++
	return nil
}

type countingRevoker struct{ count int }

func (r *countingRevoker) Revoke() { r.count++ }

func testPasswordHasher() secret.PasswordHasher {
	return secret.PasswordHasher{
		Memory:     8 * 1024,
		Iterations: 1,
		Threads:    1,
		SaltLength: 8,
		KeyLength:  16,
	}
}

func TestPasswordManagerInitializesOnceAndAuthenticates(t *testing.T) {
	store := &memoryPasswordStore{}
	revoker := &countingRevoker{}
	manager, err := NewPasswordManager(store, testPasswordHasher(), revoker, bytes.NewReader(make([]byte, 256)))
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}

	password, created, err := manager.Initialize()
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !created || len(password) != 43 {
		t.Fatalf("Initialize() = (%q, %v), want a 32-byte base64url password", password, created)
	}
	if strings.Contains(store.hash, password) || !strings.HasPrefix(store.hash, "$argon2id$") {
		t.Fatalf("stored hash must be Argon2id and must not contain plaintext: %q", store.hash)
	}
	if revoker.count != 0 {
		t.Fatalf("initialization revoked %d sessions, want 0", revoker.count)
	}
	valid, err := manager.Authenticate(password)
	if err != nil || !valid {
		t.Fatalf("Authenticate(generated) = (%v, %v), want true, nil", valid, err)
	}
	valid, err = manager.Authenticate("definitely-the-wrong-password")
	if err != nil || valid {
		t.Fatalf("Authenticate(wrong) = (%v, %v), want false, nil", valid, err)
	}

	again, created, err := manager.Initialize()
	if err != nil || created || again != "" {
		t.Fatalf("second Initialize() = (%q, %v, %v), want empty, false, nil", again, created, err)
	}
	if store.saves != 1 {
		t.Fatalf("store saves = %d, want 1", store.saves)
	}
}

func TestPasswordManagerChangesAndResetsPassword(t *testing.T) {
	store := &memoryPasswordStore{}
	revoker := &countingRevoker{}
	manager, err := NewPasswordManager(store, testPasswordHasher(), revoker, bytes.NewReader(make([]byte, 512)))
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}
	initial, _, err := manager.Initialize()
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	if err := manager.Change("wrong-current-password", "new-password-value-1234"); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("Change(wrong current) error = %v, want ErrInvalidCurrentPassword", err)
	}
	if revoker.count != 0 {
		t.Fatalf("wrong password revoked sessions")
	}
	if err := manager.Change(initial, "too-short"); err == nil {
		t.Fatal("Change(short password) succeeded")
	}
	if err := manager.Change(initial, "new-password-value-1234"); err != nil {
		t.Fatalf("Change() error = %v", err)
	}
	if revoker.count != 1 {
		t.Fatalf("Change() revocations = %d, want 1", revoker.count)
	}
	valid, err := manager.Authenticate(initial)
	if err != nil || valid {
		t.Fatalf("old password Authenticate() = (%v, %v), want false, nil", valid, err)
	}

	generated, err := manager.Reset("")
	if err != nil {
		t.Fatalf("Reset(auto) error = %v", err)
	}
	if len(generated) != 43 {
		t.Fatalf("Reset(auto) password length = %d, want 43", len(generated))
	}
	if revoker.count != 2 {
		t.Fatalf("Reset(auto) revocations = %d, want 2", revoker.count)
	}
	valid, err = manager.Authenticate(generated)
	if err != nil || !valid {
		t.Fatalf("generated reset password Authenticate() = (%v, %v), want true, nil", valid, err)
	}

	returned, err := manager.Reset("manual-reset-password-1234")
	if err != nil || returned != "manual-reset-password-1234" {
		t.Fatalf("Reset(manual) = (%q, %v)", returned, err)
	}
	if revoker.count != 3 {
		t.Fatalf("Reset(manual) revocations = %d, want 3", revoker.count)
	}
}

func TestPasswordManagerDoesNotRevokeOrReplaceOnSaveFailure(t *testing.T) {
	store := &memoryPasswordStore{}
	revoker := &countingRevoker{}
	manager, err := NewPasswordManager(store, testPasswordHasher(), revoker, bytes.NewReader(make([]byte, 256)))
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}
	initial, _, err := manager.Initialize()
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	oldHash := store.hash
	store.saveError = errors.New("disk full")

	if err := manager.Change(initial, "new-password-value-1234"); err == nil {
		t.Fatal("Change() succeeded despite save failure")
	}
	if store.hash != oldHash || revoker.count != 0 {
		t.Fatalf("save failure changed state: hash changed=%v revocations=%d", store.hash != oldHash, revoker.count)
	}
}

func TestPasswordManagerRejectsInvalidDependencies(t *testing.T) {
	revoker := &countingRevoker{}
	if _, err := NewPasswordManager(nil, testPasswordHasher(), revoker, nil); err == nil {
		t.Fatal("NewPasswordManager(nil store) succeeded")
	}
	if _, err := NewPasswordManager(&memoryPasswordStore{}, testPasswordHasher(), nil, nil); err == nil {
		t.Fatal("NewPasswordManager(nil revoker) succeeded")
	}
}

func TestFilePasswordStoreRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "admin-password.yaml")
	store, err := NewFilePasswordStore(path)
	if err != nil {
		t.Fatalf("NewFilePasswordStore() error = %v", err)
	}
	if hash, exists, err := store.Load(); err != nil || exists || hash != "" {
		t.Fatalf("Load(missing) = (%q, %v, %v), want empty, false, nil", hash, exists, err)
	}

	const hash = "$argon2id$v=19$m=8192,t=1,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaA"
	if err := store.Save(hash); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, exists, err := store.Load()
	if err != nil || !exists || got != hash {
		t.Fatalf("Load() = (%q, %v, %v), want saved hash", got, exists, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%q) error = %v", path, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("file mode = %o, want 600", got)
		}
	}
}

func TestFilePasswordStoreRejectsMalformedDocuments(t *testing.T) {
	tests := map[string]string{
		"unknown field": "schema_version: 1\npassword_hash: value\nextra: true\n",
		"wrong schema":  "schema_version: 2\npassword_hash: value\n",
		"empty hash":    "schema_version: 1\npassword_hash: ''\n",
		"trailing doc":  "schema_version: 1\npassword_hash: value\n---\nschema_version: 1\npassword_hash: other\n",
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "admin-password.yaml")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatalf("write test file: %v", err)
			}
			store, err := NewFilePasswordStore(path)
			if err != nil {
				t.Fatalf("NewFilePasswordStore() error = %v", err)
			}
			if _, _, err := store.Load(); err == nil {
				t.Fatal("Load() malformed document succeeded")
			}
		})
	}
}
