//go:build linux

package admin

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

type serviceFileLock struct {
	file *os.File
	once sync.Once
}

func AcquireServiceLock(path string) (io.Closer, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("service lock path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create service lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open service lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure service lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrServiceRunning
		}
		return nil, fmt.Errorf("lock service file: %w", err)
	}
	return &serviceFileLock{file: file}, nil
}

func (l *serviceFileLock) Close() error {
	var result error
	l.once.Do(func() {
		result = errors.Join(unix.Flock(int(l.file.Fd()), unix.LOCK_UN), l.file.Close())
	})
	return result
}
