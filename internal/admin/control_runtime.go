package admin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

type controlService interface {
	Serve(context.Context, net.Listener) error
}

type ControlListenFunc func(string) (net.Listener, error)

type ControlRuntimeOptions struct {
	Service     controlService
	AcquireLock ServiceLockAcquirer
	Listen      ControlListenFunc
	LockPath    string
	SocketPath  string
}

type ControlRuntime struct {
	service     controlService
	acquireLock ServiceLockAcquirer
	listen      ControlListenFunc
	lockPath    string
	socketPath  string
}

func NewControlRuntime(options ControlRuntimeOptions) (*ControlRuntime, error) {
	if options.Service == nil || options.AcquireLock == nil || options.Listen == nil {
		return nil, errors.New("control runtime dependencies are required")
	}
	if strings.TrimSpace(options.LockPath) == "" || strings.TrimSpace(options.SocketPath) == "" {
		return nil, errors.New("control runtime paths are required")
	}
	return &ControlRuntime{
		service: options.Service, acquireLock: options.AcquireLock, listen: options.Listen,
		lockPath: options.LockPath, socketPath: options.SocketPath,
	}, nil
}

func (r *ControlRuntime) Serve(ctx context.Context) (err error) {
	lock, err := r.acquireLock(r.lockPath)
	if err != nil {
		return fmt.Errorf("acquire service lock: %w", err)
	}
	defer func() {
		err = errors.Join(err, lock.Close())
	}()
	listener, err := r.listen(r.socketPath)
	if err != nil {
		return fmt.Errorf("listen on control socket: %w", err)
	}
	defer func() {
		err = errors.Join(err, listener.Close())
	}()
	return r.service.Serve(ctx, listener)
}
