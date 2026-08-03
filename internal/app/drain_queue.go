package app

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
)

type DrainedAddressCompleter interface {
	CompleteDrainedAddress(context.Context, string, netip.Addr) error
}

type drainedAddress struct {
	pool    string
	address netip.Addr
}

type DrainQueue struct {
	mu        sync.Mutex
	completer DrainedAddressCompleter
	report    func(error)
	pending   []drainedAddress
	wake      chan struct{}
}

func NewDrainQueue(completer DrainedAddressCompleter, report func(error)) (*DrainQueue, error) {
	if completer == nil {
		return nil, errors.New("drained address completer is required")
	}
	if report == nil {
		report = func(error) {}
	}
	return &DrainQueue{completer: completer, report: report, wake: make(chan struct{}, 1)}, nil
}

func (q *DrainQueue) Enqueue(pool string, address netip.Addr) {
	q.mu.Lock()
	q.pending = append(q.pending, drainedAddress{pool: pool, address: address.Unmap()})
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *DrainQueue) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-q.wake:
		}

		q.mu.Lock()
		batch := append([]drainedAddress(nil), q.pending...)
		q.pending = q.pending[:0]
		q.mu.Unlock()

		var failures []error
		for _, item := range batch {
			if err := q.completer.CompleteDrainedAddress(ctx, item.pool, item.address); err != nil {
				failures = append(failures, fmt.Errorf("complete drained address %s in pool %q: %w", item.address, item.pool, err))
			}
		}
		if err := errors.Join(failures...); err != nil {
			q.report(err)
		}
	}
}
