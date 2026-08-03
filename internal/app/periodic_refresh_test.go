package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunPeriodicRefreshReportsFailuresAndRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	refreshErr := errors.New("refresh failed")
	var calls atomic.Int32
	failures := make(chan error, 1)
	recovered := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- RunPeriodicRefresh(ctx, 5*time.Millisecond, func() error {
			if calls.Add(1) == 1 {
				return refreshErr
			}
			return nil
		}, func(err error) {
			failures <- err
		}, func() {
			recovered <- struct{}{}
			cancel()
		})
	}()

	select {
	case err := <-failures:
		if !errors.Is(err, refreshErr) {
			t.Fatalf("reported error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("refresh failure was not reported")
	}
	select {
	case <-recovered:
	case <-time.After(time.Second):
		t.Fatal("refresh recovery was not reported")
	}
	if err := <-done; err != nil {
		t.Fatalf("RunPeriodicRefresh() error = %v", err)
	}
}

func TestRunPeriodicRefreshValidatesOptions(t *testing.T) {
	refresh := func() error { return nil }
	report := func(error) {}
	recover := func() {}
	if err := RunPeriodicRefresh(nil, time.Second, refresh, report, recover); err == nil {
		t.Fatal("nil context accepted")
	}
	if err := RunPeriodicRefresh(context.Background(), 0, refresh, report, recover); err == nil {
		t.Fatal("zero interval accepted")
	}
	if err := RunPeriodicRefresh(context.Background(), time.Second, nil, report, recover); err == nil {
		t.Fatal("nil refresh accepted")
	}
	if err := RunPeriodicRefresh(context.Background(), time.Second, refresh, nil, recover); err == nil {
		t.Fatal("nil report accepted")
	}
	if err := RunPeriodicRefresh(context.Background(), time.Second, refresh, report, nil); err == nil {
		t.Fatal("nil recovery accepted")
	}
}
