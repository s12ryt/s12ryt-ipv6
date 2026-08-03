package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const sessionSecretBytes = 32

type Session struct {
	Token     string
	CSRFToken string
	CreatedAt time.Time
}

type SessionInfo struct {
	CreatedAt time.Time
	LastSeen  time.Time
}

type activeSession struct {
	tokenHash [sha256.Size]byte
	csrfHash  [sha256.Size]byte
	createdAt time.Time
	lastSeen  time.Time
}

type SessionManager struct {
	mu       sync.Mutex
	now      func() time.Time
	entropy  io.Reader
	idle     time.Duration
	absolute time.Duration
	active   *activeSession
}

func NewSessionManager(now func() time.Time, entropy io.Reader, idle, absolute time.Duration) *SessionManager {
	if now == nil {
		now = time.Now
	}
	if entropy == nil {
		entropy = rand.Reader
	}
	return &SessionManager{now: now, entropy: entropy, idle: idle, absolute: absolute}
}

func (m *SessionManager) Create() (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	token, err := readToken(m.entropy)
	if err != nil {
		return Session{}, fmt.Errorf("generate session token: %w", err)
	}
	csrf, err := readToken(m.entropy)
	if err != nil {
		return Session{}, fmt.Errorf("generate CSRF token: %w", err)
	}
	now := m.now()
	m.active = &activeSession{
		tokenHash: sha256.Sum256([]byte(token)),
		csrfHash:  sha256.Sum256([]byte(csrf)),
		createdAt: now,
		lastSeen:  now,
	}
	return Session{Token: token, CSRFToken: csrf, CreatedAt: now}, nil
}

func (m *SessionManager) Validate(token string) (SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.validateLocked(token, true)
}

func (m *SessionManager) ValidateCSRF(token, csrf string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.validateLocked(token, true); err != nil {
		return err
	}
	csrfHash := sha256.Sum256([]byte(csrf))
	if subtle.ConstantTimeCompare(csrfHash[:], m.active.csrfHash[:]) != 1 {
		return errors.New("invalid CSRF token")
	}
	return nil
}

func (m *SessionManager) RotateCSRF(token string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.validateLocked(token, true); err != nil {
		return "", err
	}
	csrf, err := readToken(m.entropy)
	if err != nil {
		return "", fmt.Errorf("generate CSRF token: %w", err)
	}
	m.active.csrfHash = sha256.Sum256([]byte(csrf))
	return csrf, nil
}

func (m *SessionManager) Revoke() {
	m.mu.Lock()
	m.active = nil
	m.mu.Unlock()
}

func (m *SessionManager) validateLocked(token string, touch bool) (SessionInfo, error) {
	if m.active == nil {
		return SessionInfo{}, errors.New("session is not active")
	}
	now := m.now()
	if now.Sub(m.active.lastSeen) > m.idle || now.Sub(m.active.createdAt) > m.absolute {
		m.active = nil
		return SessionInfo{}, errors.New("session expired")
	}
	tokenHash := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(tokenHash[:], m.active.tokenHash[:]) != 1 {
		return SessionInfo{}, errors.New("invalid session token")
	}
	if touch {
		m.active.lastSeen = now
	}
	return SessionInfo{CreatedAt: m.active.createdAt, LastSeen: m.active.lastSeen}, nil
}

func readToken(reader io.Reader) (string, error) {
	bytes := make([]byte, sessionSecretBytes)
	if _, err := io.ReadFull(reader, bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
