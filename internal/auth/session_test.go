package auth

import (
	"bytes"
	"net/netip"
	"testing"
	"time"
)

func TestSessionManagerAllowsOnlyNewestSession(t *testing.T) {
	now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	entropy := append(bytes.Repeat([]byte{0x12}, 64), bytes.Repeat([]byte{0x13}, 64)...)
	manager := NewSessionManager(clock, bytes.NewReader(entropy), 30*time.Minute, 12*time.Hour)

	first, err := manager.Create()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Validate(first.Token); err != nil {
		t.Fatalf("Validate(first) error = %v", err)
	}
	second, err := manager.Create()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Validate(first.Token); err == nil {
		t.Fatal("first session remained valid after second login")
	}
	if got, err := manager.Validate(second.Token); err != nil || got.CreatedAt != second.CreatedAt {
		t.Fatalf("Validate(second) = %#v, %v", got, err)
	}
}

func TestSessionManagerEnforcesIdleAndAbsoluteExpiry(t *testing.T) {
	now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	manager := NewSessionManager(clock, bytes.NewReader(bytes.Repeat([]byte{0x23}, 256)), 30*time.Minute, 12*time.Hour)

	idle, _ := manager.Create()
	now = now.Add(31 * time.Minute)
	if _, err := manager.Validate(idle.Token); err == nil {
		t.Fatal("idle session was accepted")
	}

	now = time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	absolute, _ := manager.Create()
	for range 24 {
		now = now.Add(29 * time.Minute)
		if _, err := manager.Validate(absolute.Token); err != nil {
			t.Fatalf("session expired before absolute limit: %v", err)
		}
	}
	now = time.Date(2026, 8, 3, 21, 0, 1, 0, time.UTC)
	if _, err := manager.Validate(absolute.Token); err == nil {
		t.Fatal("session survived absolute expiry")
	}
}

func TestSessionManagerValidatesCSRFAndRevoke(t *testing.T) {
	now := time.Now()
	manager := NewSessionManager(func() time.Time { return now }, bytes.NewReader(bytes.Repeat([]byte{0x34}, 256)), 30*time.Minute, 12*time.Hour)
	session, _ := manager.Create()

	if err := manager.ValidateCSRF(session.Token, session.CSRFToken); err != nil {
		t.Fatalf("ValidateCSRF(correct) error = %v", err)
	}
	if err := manager.ValidateCSRF(session.Token, "wrong"); err == nil {
		t.Fatal("ValidateCSRF(wrong) error = nil")
	}
	manager.Revoke()
	if _, err := manager.Validate(session.Token); err == nil {
		t.Fatal("revoked session was accepted")
	}
}

func TestSessionManagerRotatesCSRFForValidatedSession(t *testing.T) {
	now := time.Now()
	entropy := append(bytes.Repeat([]byte{0x41}, 64), bytes.Repeat([]byte{0x42}, 32)...)
	manager := NewSessionManager(func() time.Time { return now }, bytes.NewReader(entropy), 30*time.Minute, 12*time.Hour)
	session, err := manager.Create()
	if err != nil {
		t.Fatal(err)
	}

	replacement, err := manager.RotateCSRF(session.Token)
	if err != nil {
		t.Fatalf("RotateCSRF() error = %v", err)
	}
	if replacement == "" || replacement == session.CSRFToken {
		t.Fatalf("replacement CSRF token = %q", replacement)
	}
	if err := manager.ValidateCSRF(session.Token, session.CSRFToken); err == nil {
		t.Fatal("previous CSRF token remained valid after rotation")
	}
	if err := manager.ValidateCSRF(session.Token, replacement); err != nil {
		t.Fatalf("ValidateCSRF(replacement) error = %v", err)
	}
	if _, err := manager.RotateCSRF("invalid-session-token"); err == nil {
		t.Fatal("RotateCSRF(invalid session) error = nil")
	}
}

func TestLoginLimiterUsesIPv6Slash64AndGlobalLimit(t *testing.T) {
	now := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	limiter := NewLoginLimiter(func() time.Time { return now }, 5, 7, 15*time.Minute)
	first := netip.MustParseAddr("2001:db8:1:2::1")
	same64 := netip.MustParseAddr("2001:db8:1:2::ffff")
	other := netip.MustParseAddr("2001:db8:1:3::1")

	for range 5 {
		if !limiter.Allow(first) {
			t.Fatal("source blocked before five recorded failures")
		}
		limiter.RecordFailure(first)
	}
	if limiter.Allow(same64) {
		t.Fatal("address in same IPv6 /64 bypassed source limit")
	}
	for range 2 {
		if !limiter.Allow(other) {
			t.Fatal("global limiter blocked too early")
		}
		limiter.RecordFailure(other)
	}
	if limiter.Allow(netip.MustParseAddr("2001:db8:1:4::1")) {
		t.Fatal("global failure limit was not enforced")
	}

	now = now.Add(15*time.Minute + time.Second)
	if !limiter.Allow(first) {
		t.Fatal("expired failure window was not pruned")
	}
}

func TestLoginLimiterKeepsIPv4SourcesDistinctAndResetsSuccessfulSource(t *testing.T) {
	now := time.Now()
	limiter := NewLoginLimiter(func() time.Time { return now }, 2, 100, 15*time.Minute)
	first := netip.MustParseAddr("198.51.100.1")
	second := netip.MustParseAddr("198.51.100.2")

	limiter.RecordFailure(first)
	limiter.RecordFailure(first)
	if limiter.Allow(first) {
		t.Fatal("failed IPv4 source was not blocked")
	}
	if !limiter.Allow(second) {
		t.Fatal("distinct IPv4 source was blocked")
	}
	limiter.Reset(first)
	if !limiter.Allow(first) {
		t.Fatal("successful source reset did not clear per-source failures")
	}
}
