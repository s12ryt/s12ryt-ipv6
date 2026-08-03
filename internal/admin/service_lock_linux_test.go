//go:build linux

package admin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireServiceLockIsExclusiveAndReusable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "service.lock")
	first, err := AcquireServiceLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireServiceLock(path); !errors.Is(err, ErrServiceRunning) {
		t.Fatalf("second AcquireServiceLock() error = %v, want ErrServiceRunning", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode = %o, want 600", info.Mode().Perm())
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := AcquireServiceLock(path)
	if err != nil {
		t.Fatalf("AcquireServiceLock() after release error = %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireServiceLockValidatesPath(t *testing.T) {
	if _, err := AcquireServiceLock(""); err == nil {
		t.Fatal("AcquireServiceLock(empty) error = nil")
	}
}
