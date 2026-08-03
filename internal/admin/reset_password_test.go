package admin

import (
	"context"
	"errors"
	"io"
	"testing"
)

type fakeControlPasswordClient struct {
	password string
	err      error
	calls    int
}

func (c *fakeControlPasswordClient) ResetPassword(context.Context, string, string) (string, error) {
	c.calls++
	return c.password, c.err
}

type fakeDirectPasswordResetter struct {
	password string
	err      error
	calls    int
}

func (r *fakeDirectPasswordResetter) Reset(string) (string, error) {
	r.calls++
	return r.password, r.err
}

type trackingCloser struct {
	closed bool
}

func (c *trackingCloser) Close() error {
	c.closed = true
	return nil
}

func TestPasswordResetWorkflowUsesRunningServiceWithoutLock(t *testing.T) {
	control := &fakeControlPasswordClient{password: "service-generated-password"}
	direct := &fakeDirectPasswordResetter{}
	lockCalls := 0
	workflow, err := NewPasswordResetWorkflow(PasswordResetWorkflowOptions{
		Control: control, Direct: direct,
		AcquireLock: func(string) (io.Closer, error) {
			lockCalls++
			return &trackingCloser{}, nil
		},
		ControlPath: "/run/control.sock", LockPath: "/run/service.lock",
	})
	if err != nil {
		t.Fatal(err)
	}

	password, err := workflow.Reset(context.Background(), "")
	if err != nil || password != "service-generated-password" || lockCalls != 0 || direct.calls != 0 {
		t.Fatalf("Reset() = %q, %v; lock calls=%d direct calls=%d", password, err, lockCalls, direct.calls)
	}
}

func TestPasswordResetWorkflowFallsBackUnderServiceLock(t *testing.T) {
	control := &fakeControlPasswordClient{err: ErrControlUnavailable}
	direct := &fakeDirectPasswordResetter{password: "offline-generated-password"}
	lock := &trackingCloser{}
	workflow, err := NewPasswordResetWorkflow(PasswordResetWorkflowOptions{
		Control: control, Direct: direct,
		AcquireLock: func(string) (io.Closer, error) { return lock, nil },
		ControlPath: "/run/control.sock", LockPath: "/run/service.lock",
	})
	if err != nil {
		t.Fatal(err)
	}

	password, err := workflow.Reset(context.Background(), "")
	if err != nil || password != "offline-generated-password" || direct.calls != 1 || !lock.closed {
		t.Fatalf("Reset() = %q, %v; direct calls=%d lock closed=%v", password, err, direct.calls, lock.closed)
	}
}

func TestPasswordResetWorkflowDoesNotBypassRunningServiceFailures(t *testing.T) {
	control := &fakeControlPasswordClient{err: errors.New("password reset failed")}
	direct := &fakeDirectPasswordResetter{}
	lockCalls := 0
	workflow, err := NewPasswordResetWorkflow(PasswordResetWorkflowOptions{
		Control: control, Direct: direct,
		AcquireLock: func(string) (io.Closer, error) {
			lockCalls++
			return &trackingCloser{}, nil
		},
		ControlPath: "/run/control.sock", LockPath: "/run/service.lock",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := workflow.Reset(context.Background(), ""); err == nil || lockCalls != 0 || direct.calls != 0 {
		t.Fatalf("Reset() error = %v; lock calls=%d direct calls=%d", err, lockCalls, direct.calls)
	}
}

func TestPasswordResetWorkflowRefusesDirectWriteWhenServiceLockIsHeld(t *testing.T) {
	control := &fakeControlPasswordClient{err: ErrControlUnavailable}
	direct := &fakeDirectPasswordResetter{}
	workflow, err := NewPasswordResetWorkflow(PasswordResetWorkflowOptions{
		Control: control, Direct: direct,
		AcquireLock: func(string) (io.Closer, error) { return nil, ErrServiceRunning },
		ControlPath: "/run/control.sock", LockPath: "/run/service.lock",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := workflow.Reset(context.Background(), ""); !errors.Is(err, ErrServiceRunning) || direct.calls != 0 {
		t.Fatalf("Reset() error = %v; direct calls=%d", err, direct.calls)
	}
}

func TestPasswordResetWorkflowValidatesOptions(t *testing.T) {
	if _, err := NewPasswordResetWorkflow(PasswordResetWorkflowOptions{}); err == nil {
		t.Fatal("NewPasswordResetWorkflow(empty) error = nil")
	}
}
