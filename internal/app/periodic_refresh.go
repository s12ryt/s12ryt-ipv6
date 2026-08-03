package app

import (
	"context"
	"errors"
	"time"
)

func RunPeriodicRefresh(
	ctx context.Context,
	interval time.Duration,
	refresh func() error,
	report func(error),
	recover func(),
) error {
	if ctx == nil || interval <= 0 || refresh == nil || report == nil || recover == nil {
		return errors.New("periodic refresh options are incomplete")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := refresh(); err != nil {
				report(err)
				continue
			}
			recover()
		}
	}
}
