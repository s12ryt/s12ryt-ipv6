package secret

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

type PasswordHasher struct {
	Memory     uint32
	Iterations uint32
	Threads    uint8
	SaltLength int
	KeyLength  uint32
}

func DefaultPasswordHasher() PasswordHasher {
	return PasswordHasher{Memory: 64 * 1024, Iterations: 3, Threads: 2, SaltLength: 16, KeyLength: 32}
}

func (h PasswordHasher) Hash(password string, entropy io.Reader) (string, error) {
	if err := ValidateAdminPassword(password); err != nil {
		return "", err
	}
	if err := h.validate(); err != nil {
		return "", err
	}
	salt := make([]byte, h.SaltLength)
	if _, err := io.ReadFull(entropy, salt); err != nil {
		return "", fmt.Errorf("read password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, h.Iterations, h.Memory, h.Threads, h.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		h.Memory,
		h.Iterations,
		h.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func (h PasswordHasher) Verify(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("invalid Argon2id PHC string")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("unsupported Argon2id version")
	}
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false, errors.New("invalid Argon2id parameters")
	}
	if memory < 8*1024 || memory > 1024*1024 || iterations < 1 || iterations > 10 || threads < 1 || threads > 16 {
		return false, errors.New("unsafe Argon2id parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false, errors.New("invalid Argon2id salt")
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) < 16 || len(want) > 64 {
		return false, errors.New("invalid Argon2id hash")
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func (h PasswordHasher) validate() error {
	if h.Memory < 8*1024 || h.Memory > 1024*1024 || h.Iterations < 1 || h.Iterations > 10 || h.Threads < 1 || h.Threads > 16 {
		return errors.New("unsafe Argon2id parameters")
	}
	if h.SaltLength < 8 || h.SaltLength > 64 || h.KeyLength < 16 || h.KeyLength > 64 {
		return errors.New("invalid Argon2id salt or key length")
	}
	return nil
}
