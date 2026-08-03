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

type drainCompletion struct {
	pool    string
	address netip.Addr
}

type recordingDrainCompleter struct {
	mu          sync.Mutex
	completions []drainCompletion
	err         error
}

func (c *recordingDrainCompleter) CompleteDrainedAddress(_ context.Context, pool string, address netip.Addr) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.completions = append(c.completions, drainCompletion{pool: pool, address: address})
	return c.err
}

func (c *recordingDrainCompleter) snapshot() []drainCompletion {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]drainCompletion(nil), c.completions...)
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
	waitForDrainCompletions(t, completer, 2)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() = %v", err)
	}
	want := []drainCompletion{{pool: "shared", address: first}, {pool: "shared", address: second}}
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

func waitForDrainCompletions(t *testing.T, completer *recordingDrainCompleter, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(completer.snapshot()) >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("completion count = %d, want at least %d", len(completer.snapshot()), count)
}
