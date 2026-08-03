//go:build linux

package admin

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
)

type ControlListener struct {
	*net.UnixListener
	path string
	once sync.Once
}

func ListenControlSocket(path string) (net.Listener, error) {
	if path == "" {
		return nil, errors.New("control socket path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create control socket directory: %w", err)
	}
	if _, err := os.Lstat(path); err == nil {
		return nil, errors.New("control socket path already exists")
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect control socket path: %w", err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen on control socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		os.Remove(path)
		return nil, fmt.Errorf("secure control socket: %w", err)
	}
	return &ControlListener{UnixListener: listener, path: path}, nil
}

func (l *ControlListener) Close() error {
	var result error
	l.once.Do(func() {
		result = errors.Join(l.UnixListener.Close(), removeControlSocket(l.path))
	})
	return result
}

func removeControlSocket(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove control socket: %w", err)
	}
	return nil
}
