package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/s12ryt/s12ryt-ipv6/internal/node"
)

type startupNodeStateLoader interface {
	Load() (node.State, bool, error)
}

type startupNodeBindingManager interface {
	List() []node.Node
	RefreshInboundBindings(context.Context, node.InboundConfigResolver, node.InboundDrainedObserver) error
}

type startupNodeRefresher struct {
	mu       sync.RWMutex
	manager  startupNodeBindingManager
	desired  []node.Node
	restored bool
}

func newStartupNodeRefresher(manager startupNodeBindingManager, store startupNodeStateLoader) (*startupNodeRefresher, error) {
	if manager == nil || store == nil {
		return nil, errors.New("startup node manager and state store are required")
	}
	state, exists, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load desired startup nodes: %w", err)
	}
	desired := []node.Node(nil)
	if exists {
		desired = append(desired, state.Nodes...)
	}
	return &startupNodeRefresher{manager: manager, desired: desired}, nil
}

func (r *startupNodeRefresher) List() []node.Node {
	r.mu.RLock()
	restored := r.restored
	desired := append([]node.Node(nil), r.desired...)
	r.mu.RUnlock()
	if restored {
		return r.manager.List()
	}
	if live := r.manager.List(); len(live) != 0 {
		return live
	}
	return desired
}

func (r *startupNodeRefresher) RefreshInboundBindings(ctx context.Context, resolver node.InboundConfigResolver, observer node.InboundDrainedObserver) error {
	return r.manager.RefreshInboundBindings(ctx, resolver, observer)
}

func (r *startupNodeRefresher) MarkRestored() {
	r.mu.Lock()
	r.restored = true
	r.desired = nil
	r.mu.Unlock()
}

var _ node.InboundBindingRefresher = (*startupNodeRefresher)(nil)
