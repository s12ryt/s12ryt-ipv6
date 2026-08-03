package secret

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVaultEncryptsAndDetectsTampering(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, MasterKeySize)
	vault, err := NewVault(key, bytes.NewReader(bytes.Repeat([]byte{0x24}, 64)))
	if err != nil {
		t.Fatal(err)
	}

	ciphertext, err := vault.Encrypt([]byte("proxy-password"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if strings.Contains(ciphertext, "proxy-password") {
		t.Fatal("ciphertext contains plaintext")
	}
	plaintext, err := vault.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if got := string(plaintext); got != "proxy-password" {
		t.Fatalf("plaintext = %q", got)
	}

	replacement := "A"
	if ciphertext[len(ciphertext)-1:] == replacement {
		replacement = "B"
	}
	tampered := ciphertext[:len(ciphertext)-1] + replacement
	if _, err := vault.Decrypt(tampered); err == nil {
		t.Fatal("Decrypt(tampered) error = nil")
	}
}

func TestLoadOrCreateMasterKeyIsStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	firstEntropy := bytes.NewReader(bytes.Repeat([]byte{0x31}, MasterKeySize))
	first, created, err := LoadOrCreateMasterKey(path, firstEntropy)
	if err != nil {
		t.Fatal(err)
	}
	if !created || len(first) != MasterKeySize {
		t.Fatalf("created = %v, key length = %d", created, len(first))
	}
	second, created, err := LoadOrCreateMasterKey(path, bytes.NewReader(bytes.Repeat([]byte{0x99}, MasterKeySize)))
	if err != nil {
		t.Fatal(err)
	}
	if created || !bytes.Equal(first, second) {
		t.Fatal("existing master key was not reused")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("master key mode = %o, want 600", got)
		}
	}
}

func TestProxyCredentialsGenerateMissingValuesAndValidateInput(t *testing.T) {
	entropy := bytes.NewReader(bytes.Repeat([]byte{0x5a}, 128))
	credentials, err := NewProxyCredentials("", "", entropy)
	if err != nil {
		t.Fatalf("NewProxyCredentials() error = %v", err)
	}
	if len(credentials.Username) < 1 || len(credentials.Username) > 64 {
		t.Errorf("generated username length = %d", len(credentials.Username))
	}
	if len(credentials.Password) < 12 || len(credentials.Password) > 128 {
		t.Errorf("generated password length = %d", len(credentials.Password))
	}
	if !isPrintableASCII(credentials.Username) || !isPrintableASCII(credentials.Password) {
		t.Fatalf("generated credentials are not printable ASCII: %#v", credentials)
	}

	if _, err := NewProxyCredentials("user\n", "valid-password", entropy); err == nil {
		t.Fatal("control character in username was accepted")
	}
	if _, err := NewProxyCredentials("user", "short", entropy); err == nil {
		t.Fatal("short password was accepted")
	}
}

func TestAdminPasswordGenerationAndArgon2idVerification(t *testing.T) {
	password, err := GenerateAdminPassword(bytes.NewReader(bytes.Repeat([]byte{0xab}, AdminPasswordBytes)))
	if err != nil {
		t.Fatal(err)
	}
	if len(password) != 43 {
		t.Fatalf("generated password length = %d, want 43", len(password))
	}
	if err := ValidateAdminPassword(password); err != nil {
		t.Fatalf("ValidateAdminPassword() error = %v", err)
	}

	hasher := DefaultPasswordHasher()
	hash, err := hasher.Hash(password, bytes.NewReader(bytes.Repeat([]byte{0xcd}, hasher.SaltLength)))
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash = %q, want Argon2id PHC", hash)
	}
	if ok, err := hasher.Verify(password, hash); err != nil || !ok {
		t.Fatalf("Verify(correct) = %v, %v", ok, err)
	}
	if ok, err := hasher.Verify("not-the-password", hash); err != nil || ok {
		t.Fatalf("Verify(wrong) = %v, %v", ok, err)
	}
}

func TestValidateAdminPasswordUsesCharactersForMinimumAndBytesForMaximum(t *testing.T) {
	if err := ValidateAdminPassword("十五個字元abcdefgh"); err == nil {
		t.Fatal("password shorter than 16 characters was accepted")
	}
	if err := ValidateAdminPassword(strings.Repeat("界", 86)); err == nil {
		t.Fatal("password longer than 256 UTF-8 bytes was accepted")
	}
	if err := ValidateAdminPassword(strings.Repeat("界", 16)); err != nil {
		t.Fatalf("valid UTF-8 password was rejected: %v", err)
	}
}
