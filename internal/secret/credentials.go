package secret

import (
	cryptorand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const AdminPasswordBytes = 32

type ProxyCredentials struct {
	Username string
	Password string
}

func NewProxyCredentials(username, password string, entropy io.Reader) (ProxyCredentials, error) {
	if entropy == nil {
		entropy = cryptorand.Reader
	}
	var err error
	if username == "" {
		username, err = randomBase64URL(entropy, 12)
		if err != nil {
			return ProxyCredentials{}, fmt.Errorf("generate proxy username: %w", err)
		}
	}
	if password == "" {
		password, err = randomBase64URL(entropy, 24)
		if err != nil {
			return ProxyCredentials{}, fmt.Errorf("generate proxy password: %w", err)
		}
	}
	if len(username) < 1 || len(username) > 64 || !isPrintableASCII(username) {
		return ProxyCredentials{}, errors.New("proxy username must be 1-64 printable ASCII characters")
	}
	if len(password) < 12 || len(password) > 128 || !isPrintableASCII(password) {
		return ProxyCredentials{}, errors.New("proxy password must be 12-128 printable ASCII characters")
	}
	return ProxyCredentials{Username: username, Password: password}, nil
}

func GenerateAdminPassword(entropy io.Reader) (string, error) {
	if entropy == nil {
		entropy = cryptorand.Reader
	}
	return randomBase64URL(entropy, AdminPasswordBytes)
}

func ValidateAdminPassword(password string) error {
	if !utf8.ValidString(password) {
		return errors.New("admin password must be valid UTF-8")
	}
	if utf8.RuneCountInString(password) < 16 {
		return errors.New("admin password must contain at least 16 characters")
	}
	if len(password) > 256 {
		return errors.New("admin password must not exceed 256 UTF-8 bytes")
	}
	return nil
}

func isPrintableASCII(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

func randomBase64URL(entropy io.Reader, size int) (string, error) {
	value := make([]byte, size)
	if _, err := io.ReadFull(entropy, value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
