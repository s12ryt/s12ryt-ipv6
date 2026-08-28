package eventlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Kind string

const (
	KindProxy  Kind = "proxy"
	KindSystem Kind = "system"
	KindAudit  Kind = "audit"
)

type Event struct {
	Time            time.Time `json:"time"`
	Kind            Kind      `json:"kind"`
	Action          string    `json:"action"`
	Actor           string    `json:"actor,omitempty"`
	Node            string    `json:"node,omitempty"`
	Protocol        string    `json:"protocol,omitempty"`
	Success         bool      `json:"success"`
	SourceIP        string    `json:"source_ip,omitempty"`
	DestinationHost string    `json:"destination_host,omitempty"`
	DestinationPort uint16    `json:"destination_port,omitempty"`
	OutboundIP      string    `json:"outbound_ip,omitempty"`
	Error           string    `json:"error,omitempty"`
}

type Filter struct {
	Kind    Kind
	Node    string
	Action  string
	Success *bool
}

type Logger struct {
	mu           sync.Mutex
	path         string
	maxBytes     int64
	backups      int
	stdout       io.Writer
	now          func() time.Time
	file         *os.File
	size         int64
	secrets      []string
	secretCounts map[string]int
	closed       bool
}

func New(path string, maxBytes int64, backups int, stdout io.Writer, now func() time.Time) (*Logger, error) {
	if path == "" {
		return nil, errors.New("log path is required")
	}
	if maxBytes <= 0 {
		return nil, errors.New("maxBytes must be positive")
	}
	if backups < 0 {
		return nil, errors.New("backups cannot be negative")
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("stat log: %w", err)
	}
	return &Logger{
		path: path, maxBytes: maxBytes, backups: backups,
		stdout: stdout, now: now, file: file, size: info.Size(),
		secretCounts: make(map[string]int),
	}, nil
}

func (l *Logger) RegisterSecret(secret string) {
	if secret == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, existing := range l.secrets {
		if existing == secret {
			l.secretCounts[secret]++
			return
		}
	}
	l.secrets = append(l.secrets, secret)
	l.secretCounts[secret] = 1
}

// UnregisterSecret releases one RegisterSecret reference for the value. A
// secret stops being redacted only when its last reference is released, so
// distinct live nodes that share a credential keep their redaction intact.
// Unknown or empty values are ignored.
func (l *Logger) UnregisterSecret(secret string) {
	if secret == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	count, known := l.secretCounts[secret]
	if !known {
		return
	}
	if count > 1 {
		l.secretCounts[secret] = count - 1
		return
	}
	delete(l.secretCounts, secret)
	for index, existing := range l.secrets {
		if existing == secret {
			l.secrets = append(l.secrets[:index], l.secrets[index+1:]...)
			return
		}
	}
}

func (l *Logger) Write(event Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.writeLocked(event)
}

func (l *Logger) Tail(filter Filter, limit int) ([]Event, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("log tail limit must be between 1 and 1000")
	}
	// Open every segment file under a short lock, then release the mutex
	// before decoding so concurrent Write calls are not blocked for the
	// duration of the scan. Rotated segments are immutable; the current
	// segment is read up to the size snapshot taken under the lock, so a
	// concurrent append can only be partially visible. If the log rotates or
	// is cleared while decoding, the result is a consistent-enough snapshot
	// per file descriptor (old segment content stays readable), though a few
	// events may be repeated or missing across the rotation boundary.
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, errors.New("logger is closed")
	}
	type segment struct {
		file *os.File
		size int64
	}
	segments := make([]segment, 0, l.backups+1)
	for i := l.backups; i >= 0; i-- {
		path := l.path
		if i > 0 {
			path = fmt.Sprintf("%s.%d", l.path, i)
		}
		file, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			l.mu.Unlock()
			for _, opened := range segments {
				opened.file.Close()
			}
			return nil, fmt.Errorf("open log segment: %w", err)
		}
		if i == 0 {
			segments = append(segments, segment{file: file, size: l.size})
		} else {
			segments = append(segments, segment{file: file})
		}
	}
	l.mu.Unlock()

	defer func() {
		for _, opened := range segments {
			opened.file.Close()
		}
	}()

	result := make([]Event, 0, limit)
	for _, seg := range segments {
		var reader io.Reader = seg.file
		if seg.size > 0 {
			reader = io.LimitReader(seg.file, seg.size)
		}
		decoder := json.NewDecoder(reader)
		for {
			var event Event
			err := decoder.Decode(&event)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("decode log segment: %w", err)
			}
			if !filter.matches(event) {
				continue
			}
			if len(result) == limit {
				copy(result, result[1:])
				result[len(result)-1] = event
			} else {
				result = append(result, event)
			}
		}
	}
	return result, nil
}

func (f Filter) matches(event Event) bool {
	if f.Kind != "" && event.Kind != f.Kind {
		return false
	}
	if f.Node != "" && event.Node != f.Node {
		return false
	}
	if f.Action != "" && event.Action != f.Action {
		return false
	}
	return f.Success == nil || event.Success == *f.Success
}

func (l *Logger) writeLocked(event Event) error {
	if l.closed {
		return errors.New("logger is closed")
	}
	if event.Time.IsZero() {
		event.Time = l.now()
	}
	event = l.redact(event)
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	line = append(line, '\n')
	if l.size > 0 && l.size+int64(len(line)) > l.maxBytes {
		if err := l.rotateLocked(); err != nil {
			return err
		}
	}
	if _, err := l.file.Write(line); err != nil {
		return fmt.Errorf("write log: %w", err)
	}
	l.size += int64(len(line))
	// Proxy connection events stay in the file only: they are high-volume and
	// carry per-connection addresses, so they must not flood stdout/journal.
	if l.stdout != nil && event.Kind != KindProxy {
		if _, err := l.stdout.Write(line); err != nil {
			return fmt.Errorf("write stdout log: %w", err)
		}
	}
	return nil
}

func (l *Logger) Clear(actor string) (resultErr error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return errors.New("logger is closed")
	}
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("close log before clear: %w", err)
	}
	needsRecovery := true
	defer func() {
		if resultErr == nil || !needsRecovery {
			return
		}
		if err := l.reopenCurrentLocked(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("recover current log after clear failure: %w", err))
		}
	}()
	for i := 0; i <= l.backups; i++ {
		candidate := l.path
		if i > 0 {
			candidate = fmt.Sprintf("%s.%d", l.path, i)
		}
		if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove log %s: %w", candidate, err)
		}
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("reopen cleared log: %w", err)
	}
	l.file = file
	l.size = 0
	needsRecovery = false
	return l.writeLocked(Event{Kind: KindAudit, Action: "log.clear", Actor: actor, Success: true})
}

func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.file == nil {
		return nil
	}
	return l.file.Close()
}

func (l *Logger) rotateLocked() (resultErr error) {
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("close log before rotation: %w", err)
	}
	defer func() {
		if resultErr == nil {
			return
		}
		if err := l.reopenCurrentLocked(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("recover current log after rotation failure: %w", err))
		}
	}()
	if l.backups == 0 {
		if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove rotated log: %w", err)
		}
	} else {
		oldest := fmt.Sprintf("%s.%d", l.path, l.backups)
		if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove oldest log backup: %w", err)
		}
		for i := l.backups - 1; i >= 1; i-- {
			source := fmt.Sprintf("%s.%d", l.path, i)
			target := fmt.Sprintf("%s.%d", l.path, i+1)
			if err := os.Rename(source, target); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("rotate log backup: %w", err)
			}
		}
		if err := os.Rename(l.path, l.path+".1"); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rotate current log: %w", err)
		}
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open new log after rotation: %w", err)
	}
	l.file = file
	l.size = 0
	return nil
}

func (l *Logger) reopenCurrentLocked() error {
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	l.file = file
	l.size = info.Size()
	return nil
}

func (l *Logger) redact(event Event) Event {
	redact := func(value string) string {
		for _, secret := range l.secrets {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
		return value
	}
	event.Action = redact(event.Action)
	event.Actor = redact(event.Actor)
	event.Node = redact(event.Node)
	event.Protocol = redact(event.Protocol)
	event.SourceIP = redact(event.SourceIP)
	event.DestinationHost = redact(event.DestinationHost)
	event.OutboundIP = redact(event.OutboundIP)
	event.Error = redact(event.Error)
	return event
}
