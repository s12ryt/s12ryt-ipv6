package secret

import (
	"crypto/aes"
	"crypto/cipher"
	cryptorand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	MasterKeySize = 32
	vaultVersion  = "v1"
)

type Vault struct {
	aead    cipher.AEAD
	entropy io.Reader
}

func NewVault(key []byte, entropy io.Reader) (*Vault, error) {
	if len(key) != MasterKeySize {
		return nil, fmt.Errorf("master key must be %d bytes", MasterKeySize)
	}
	block, err := aes.NewCipher(append([]byte(nil), key...))
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create AES-GCM: %w", err)
	}
	if entropy == nil {
		entropy = cryptorand.Reader
	}
	return &Vault{aead: aead, entropy: entropy}, nil
}

func (v *Vault) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := io.ReadFull(v.entropy, nonce); err != nil {
		return "", fmt.Errorf("read encryption nonce: %w", err)
	}
	sealed := v.aead.Seal(nil, nonce, plaintext, []byte(vaultVersion))
	payload := append(nonce, sealed...)
	return vaultVersion + "." + base64.RawURLEncoding.EncodeToString(payload), nil
}

func (v *Vault) Decrypt(encoded string) ([]byte, error) {
	version, payloadText, found := strings.Cut(encoded, ".")
	if !found || version != vaultVersion {
		return nil, errors.New("unsupported encrypted value format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadText)
	if err != nil {
		return nil, errors.New("encrypted value is not valid base64url")
	}
	if len(payload) < v.aead.NonceSize()+v.aead.Overhead() {
		return nil, errors.New("encrypted value is truncated")
	}
	nonce := payload[:v.aead.NonceSize()]
	ciphertext := payload[v.aead.NonceSize():]
	plaintext, err := v.aead.Open(nil, nonce, ciphertext, []byte(vaultVersion))
	if err != nil {
		return nil, errors.New("encrypted value authentication failed")
	}
	return plaintext, nil
}

func LoadOrCreateMasterKey(path string, entropy io.Reader) ([]byte, bool, error) {
	if entropy == nil {
		entropy = cryptorand.Reader
	}
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != MasterKeySize {
			return nil, false, fmt.Errorf("master key has %d bytes, want %d", len(key), MasterKeySize)
		}
		return append([]byte(nil), key...), false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, false, fmt.Errorf("read master key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, fmt.Errorf("create master key directory: %w", err)
	}
	key = make([]byte, MasterKeySize)
	if _, err := io.ReadFull(entropy, key); err != nil {
		return nil, false, fmt.Errorf("read master key entropy: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		existing, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, false, fmt.Errorf("read concurrently created master key: %w", readErr)
		}
		if len(existing) != MasterKeySize {
			return nil, false, fmt.Errorf("master key has %d bytes, want %d", len(existing), MasterKeySize)
		}
		return existing, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("create master key: %w", err)
	}
	if _, err := file.Write(key); err != nil {
		file.Close()
		os.Remove(path)
		return nil, false, fmt.Errorf("write master key: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(path)
		return nil, false, fmt.Errorf("sync master key: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return nil, false, fmt.Errorf("close master key: %w", err)
	}
	return key, true, nil
}
