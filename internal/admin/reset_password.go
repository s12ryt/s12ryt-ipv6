package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrServiceRunning = errors.New("service is running")

type ControlPasswordClient interface {
	ResetPassword(context.Context, string, string) (string, error)
}

type ServiceLockAcquirer func(string) (io.Closer, error)

type PasswordResetWorkflowOptions struct {
	Control     ControlPasswordClient
	Direct      PasswordResetter
	AcquireLock ServiceLockAcquirer
	ControlPath string
	LockPath    string
}

type PasswordResetWorkflow struct {
	control     ControlPasswordClient
	direct      PasswordResetter
	acquireLock ServiceLockAcquirer
	controlPath string
	lockPath    string
}

func NewPasswordResetWorkflow(options PasswordResetWorkflowOptions) (*PasswordResetWorkflow, error) {
	if options.Control == nil || options.Direct == nil || options.AcquireLock == nil {
		return nil, errors.New("password reset dependencies are required")
	}
	if strings.TrimSpace(options.ControlPath) == "" || strings.TrimSpace(options.LockPath) == "" {
		return nil, errors.New("control and lock paths are required")
	}
	return &PasswordResetWorkflow{
		control: options.Control, direct: options.Direct, acquireLock: options.AcquireLock,
		controlPath: options.ControlPath, lockPath: options.LockPath,
	}, nil
}

func (w *PasswordResetWorkflow) Reset(ctx context.Context, replacement string) (password string, err error) {
	password, err = w.control.ResetPassword(ctx, w.controlPath, replacement)
	if err == nil {
		return password, nil
	}
	if !errors.Is(err, ErrControlUnavailable) {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	lock, err := w.acquireLock(w.lockPath)
	if err != nil {
		return "", fmt.Errorf("acquire service lock: %w", err)
	}
	defer func() {
		err = errors.Join(err, lock.Close())
	}()
	password, err = w.direct.Reset(replacement)
	return password, err
}
