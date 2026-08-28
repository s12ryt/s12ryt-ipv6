package eventlog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoggerKeepsProxyEventsOutOfStdoutWhileRedactingSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	var stdout bytes.Buffer
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	logger, err := New(path, 1024*1024, 5, &stdout, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	logger.RegisterSecret("top-secret-password")

	err = logger.Write(Event{
		Kind: KindProxy, Action: "connect", Node: "edge-a", Protocol: "socks5", Success: false,
		SourceIP: "2001:db8::1", DestinationHost: "example.com", DestinationPort: 443,
		OutboundIP: "2001:db8::2", Error: "authentication top-secret-password rejected",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) == 0 {
		t.Fatal("proxy event missing from log file")
	}
	if bytes.Contains(contents, []byte("top-secret-password")) {
		t.Fatal("registered secret leaked into log")
	}
	if bytes.Contains(contents, []byte("url_path")) || bytes.Contains(contents, []byte("headers")) {
		t.Fatalf("forbidden HTTP detail appeared in log: %s", contents)
	}
	if stdout.Len() != 0 {
		t.Fatalf("proxy connection event leaked to stdout: %s", stdout.Bytes())
	}
	var event Event
	if err := json.Unmarshal(bytes.TrimSpace(contents), &event); err != nil {
		t.Fatalf("log is not valid JSONL: %v", err)
	}
	if event.Error != "authentication [REDACTED] rejected" || event.Time != now {
		t.Fatalf("event = %#v", event)
	}
}

func TestLoggerMirrorsSystemAndAuditEventsToStdout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	var stdout bytes.Buffer
	logger, err := New(path, 1024*1024, 5, &stdout, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	events := []Event{
		{Kind: KindProxy, Action: "connect", Node: "edge-a", Success: true, OutboundIP: "2001:db8::9"},
		{Kind: KindSystem, Action: "health", Success: true},
		{Kind: KindAudit, Action: "node.start", Actor: "admin", Node: "edge-a", Success: true},
		{Kind: KindProxy, Action: "associate", Node: "edge-a", Success: false},
	}
	for _, event := range events {
		if err := logger.Write(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	stdoutLines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(stdoutLines) != 2 {
		t.Fatalf("stdout should carry only system and audit events, got %d lines: %s", len(stdoutLines), stdout.Bytes())
	}
	if !strings.Contains(stdoutLines[0], `"kind":"system"`) || !strings.Contains(stdoutLines[1], `"kind":"audit"`) {
		t.Fatalf("stdout order or kinds wrong: %s", stdout.Bytes())
	}
	if strings.Contains(stdout.String(), "2001:db8::9") {
		t.Fatalf("proxy address leaked to stdout: %s", stdout.Bytes())
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(contents)), "\n"); len(lines) != 4 {
		t.Fatalf("file should keep all four events, got %d", len(lines))
	}
}

func TestLoggerRotatesAndKeepsConfiguredBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	logger, err := New(path, 180, 2, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		if err := logger.Write(Event{Kind: KindSystem, Action: "health", Error: strings.Repeat("x", 80)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, path + ".1", path + ".2"} {
		if _, err := os.Stat(candidate); err != nil {
			t.Fatalf("expected log file %s: %v", candidate, err)
		}
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("unexpected third backup: %v", err)
	}
}

func TestLoggerRecoversAfterRotationFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	logger, err := New(path, 1, 1, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	if err := logger.Write(Event{Kind: KindSystem, Action: "before-failure"}); err != nil {
		t.Fatal(err)
	}
	blockedBackup := path + ".1"
	if err := os.Mkdir(blockedBackup, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blockedBackup, "block"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := logger.Write(Event{Kind: KindSystem, Action: "rotation-fails"}); err == nil {
		t.Fatal("Write() error = nil while backup removal is blocked")
	}
	if err := os.RemoveAll(blockedBackup); err != nil {
		t.Fatal(err)
	}

	if err := logger.Write(Event{Kind: KindSystem, Action: "after-recovery"}); err != nil {
		t.Fatalf("Write() after rotation obstruction cleared = %v", err)
	}
	events, err := logger.Tail(Filter{Action: "after-recovery"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "after-recovery" {
		t.Fatalf("Tail() after recovery = %#v", events)
	}
}

func TestLoggerClearRemovesRotationsAndStartsWithAuditEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	logger, err := New(path, 160, 3, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if err := logger.Write(Event{Kind: KindSystem, Action: "before-clear", Error: strings.Repeat("x", 80)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := logger.Clear("admin"); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		if _, err := os.Stat(path + "." + string(rune('0'+i))); !os.IsNotExist(err) {
			t.Fatalf("backup %d remains after clear: %v", i, err)
		}
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("cleared log has no audit event")
	}
	var event Event
	if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event.Kind != KindAudit || event.Action != "log.clear" || event.Actor != "admin" {
		t.Fatalf("first event after clear = %#v", event)
	}
	if scanner.Scan() {
		t.Fatal("cleared log contains events after audit entry")
	}
}

func TestLoggerRecoversAfterClearFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	logger, err := New(path, 1024*1024, 1, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	if err := logger.Write(Event{Kind: KindSystem, Action: "before-failure"}); err != nil {
		t.Fatal(err)
	}
	blockedBackup := path + ".1"
	if err := os.Mkdir(blockedBackup, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blockedBackup, "block"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := logger.Clear("admin"); err == nil {
		t.Fatal("Clear() error = nil while backup removal is blocked")
	}
	if err := os.RemoveAll(blockedBackup); err != nil {
		t.Fatal(err)
	}

	if err := logger.Write(Event{Kind: KindSystem, Action: "after-recovery"}); err != nil {
		t.Fatalf("Write() after clear obstruction removed = %v", err)
	}
	events, err := logger.Tail(Filter{Action: "after-recovery"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "after-recovery" {
		t.Fatalf("Tail() after recovery = %#v", events)
	}
}

func TestLoggerTailReadsRotationsInOrderAndFilters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	logger, err := New(path, 180, 3, nil, func() time.Time {
		return time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	events := []Event{
		{Kind: KindProxy, Action: "connect", Node: "alpha", Success: true},
		{Kind: KindSystem, Action: "health", Success: false},
		{Kind: KindProxy, Action: "connect", Node: "beta", Success: false},
		{Kind: KindProxy, Action: "connect", Node: "alpha", Success: false},
	}
	for _, event := range events {
		if err := logger.Write(event); err != nil {
			t.Fatal(err)
		}
	}

	failed := false
	filtered, err := logger.Tail(Filter{Kind: KindProxy, Node: "alpha", Success: &failed}, 10)
	if err != nil {
		t.Fatalf("Tail(filtered) error = %v", err)
	}
	if len(filtered) != 1 || filtered[0].Node != "alpha" || filtered[0].Success {
		t.Fatalf("Tail(filtered) = %#v", filtered)
	}

	latest, err := logger.Tail(Filter{}, 2)
	if err != nil {
		t.Fatalf("Tail(latest) error = %v", err)
	}
	if len(latest) != 2 || latest[0].Node != "beta" || latest[1].Node != "alpha" {
		t.Fatalf("Tail(latest) = %#v", latest)
	}
}

func TestLoggerTailValidatesLimit(t *testing.T) {
	logger, err := New(filepath.Join(t.TempDir(), "events.jsonl"), 1024, 1, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	for _, limit := range []int{0, 1001} {
		if _, err := logger.Tail(Filter{}, limit); err == nil {
			t.Fatalf("Tail(limit=%d) error = nil", limit)
		}
	}
}
