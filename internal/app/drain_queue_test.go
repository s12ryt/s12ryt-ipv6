package app

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"sync"
	"testing"
	"time"
)

type drainBatchCompletion struct {
	pool      string
	addresses []netip.Addr
}

type recordingDrainCompleter struct {
	mu          sync.Mutex
	completions []drainBatchCompletion
	err         error
}

func (c *recordingDrainCompleter) CompleteDrainedAddresses(_ context.Context, pool string, addresses []netip.Addr) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.completions = append(c.completions, drainBatchCompletion{
		pool:      pool,
		addresses: append([]netip.Addr(nil), addresses...),
	})
	return c.err
}

func (c *recordingDrainCompleter) snapshot() []drainBatchCompletion {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]drainBatchCompletion(nil), c.completions...)
}

func (c *recordingDrainCompleter) completedAddresses() int {
	total := 0
	for _, batch := range c.snapshot() {
		total += len(batch.addresses)
	}
	return total
}

func TestDrainQueueProcessesEveryEnqueuedAddressInOrder(t *testing.T) {
	completer := &recordingDrainCompleter{}
	queue, err := NewDrainQueue(completer, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := netip.MustParseAddr("2001:4860:1::1")
	second := netip.MustParseAddr("2001:4860:1::2")
	queue.Enqueue("shared", first)
	queue.Enqueue("shared", second)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- queue.Run(ctx) }()
	waitForDrainAddresses(t, completer, 2)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() = %v", err)
	}
	want := []drainBatchCompletion{{pool: "shared", addresses: []netip.Addr{first, second}}}
	if got := completer.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("completions = %#v, want %#v", got, want)
	}
}

func TestDrainQueueGroupsCompletionsByPool(t *testing.T) {
	completer := &recordingDrainCompleter{}
	queue, err := NewDrainQueue(completer, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := netip.MustParseAddr("2001:4860:1::1")
	second := netip.MustParseAddr("2001:4860:1::2")
	third := netip.MustParseAddr("2001:4860:1::3")
	dedicated := netip.MustParseAddr("2001:4860:9::7")
	queue.Enqueue("shared", first)
	queue.Enqueue("shared", second)
	queue.Enqueue("dedicated", dedicated)
	queue.Enqueue("shared", third)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- queue.Run(ctx) }()
	waitForDrainAddresses(t, completer, 4)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() = %v", err)
	}
	want := []drainBatchCompletion{
		{pool: "shared", addresses: []netip.Addr{first, second, third}},
		{pool: "dedicated", addresses: []netip.Addr{dedicated}},
	}
	if got := completer.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("completions = %#v, want %#v", got, want)
	}
}

func TestDrainQueueDoesNotBlockProducerAndReportsFailures(t *testing.T) {
	completer := &recordingDrainCompleter{err: errors.New("disk failed")}
	reported := make(chan error, 1)
	queue, err := NewDrainQueue(completer, func(err error) { reported <- err })
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 10_000; index++ {
		queue.Enqueue("shared", netip.MustParseAddr("2001:4860:1::1"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- queue.Run(ctx) }()
	select {
	case err := <-reported:
		if err == nil || !errors.Is(err, completer.err) {
			t.Fatalf("reported error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("completion failure was not reported")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if _, err := NewDrainQueue(nil, nil); err == nil {
		t.Fatal("NewDrainQueue(nil) error = nil")
	}
}

func waitForDrainAddresses(t *testing.T, completer *recordingDrainCompleter, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if completer.completedAddresses() >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("completed addresses = %d, want at least %d", completer.completedAddresses(), count)
}
