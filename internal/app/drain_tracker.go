package app

import (
	"errors"
	"fmt"
	"net/netip"
	"sync"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/node"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type drainKey struct {
	pool    string
	address netip.Addr
}

type DrainTracker struct {
	mu      sync.Mutex
	done    func(string, netip.Addr)
	pending map[drainKey]map[string]struct{}
}

func NewDrainTracker(done func(string, netip.Addr)) (*DrainTracker, error) {
	if done == nil {
		return nil, errors.New("drain completion callback is required")
	}
	return &DrainTracker{done: done, pending: make(map[drainKey]map[string]struct{})}, nil
}

func (t *DrainTracker) Prepare(state ipv6resource.State, nodes []node.Node) error {
	validated, err := ipv6resource.NewStoreFromState(state)
	if err != nil {
		return fmt.Errorf("validate drain state: %w", err)
	}
	runningInbound := make(map[string][]string)
	for _, current := range nodes {
		if current.Status != node.StatusRunning || current.Config.InboundMode == node.InboundIPv4 || current.Config.InboundMode == "" {
			continue
		}
		resource := current.Config.InboundResource
		runningInbound[resource] = append(runningInbound[resource], "inbound:"+current.Config.ID)
	}

	next := make(map[drainKey]map[string]struct{})
	var immediatelyReady []drainKey
	for _, pool := range validated.Pools() {
		var consumers []string
		if pool.Kind == ipv6resource.PoolInbound {
			consumers = runningInbound[pool.Name]
		} else {
			consumers = []string{"outbound"}
		}
		for _, batch := range pool.Draining {
			for _, address := range batch.Addresses {
				key := drainKey{pool: pool.Name, address: address.Unmap()}
				if len(consumers) == 0 {
					immediatelyReady = append(immediatelyReady, key)
					continue
				}
				expected := make(map[string]struct{}, len(consumers))
				for _, consumer := range consumers {
					expected[consumer] = struct{}{}
				}
				next[key] = expected
			}
		}
	}
	t.mu.Lock()
	t.pending = next
	done := t.done
	t.mu.Unlock()
	for _, key := range immediatelyReady {
		done(key.pool, key.address)
	}
	return nil
}

func (t *DrainTracker) InboundDrained(nodeID, resource string, endpoint proxy.BindEndpoint) {
	if !endpoint.Address.IsValid() {
		return
	}
	t.mark(drainKey{pool: resource, address: endpoint.Address.Unmap()}, "inbound:"+nodeID)
}

func (t *DrainTracker) OutboundDrained(pool string, address netip.Addr) {
	if !address.IsValid() {
		return
	}
	t.mark(drainKey{pool: pool, address: address.Unmap()}, "outbound")
}

func (t *DrainTracker) mark(key drainKey, consumer string) {
	t.mu.Lock()
	expected, exists := t.pending[key]
	if !exists {
		t.mu.Unlock()
		return
	}
	if _, expectedConsumer := expected[consumer]; !expectedConsumer {
		t.mu.Unlock()
		return
	}
	delete(expected, consumer)
	if len(expected) != 0 {
		t.mu.Unlock()
		return
	}
	delete(t.pending, key)
	done := t.done
	t.mu.Unlock()
	done(key.pool, key.address)
}
