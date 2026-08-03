package app

import (
	"context"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/node"
)

type startupNodeStore struct {
	state  node.State
	exists bool
	err    error
}

func (s startupNodeStore) Load() (node.State, bool, error) { return s.state, s.exists, s.err }

type startupNodeManager struct {
	nodes []node.Node
}

func (m *startupNodeManager) List() []node.Node { return append([]node.Node(nil), m.nodes...) }
func (*startupNodeManager) RefreshInboundBindings(context.Context, node.InboundConfigResolver, node.InboundDrainedObserver) error {
	return nil
}

func TestStartupNodeRefresherUsesDesiredStateUntilLiveNodesExist(t *testing.T) {
	desired := node.Node{Config: node.Config{ID: "desired"}, Status: node.StatusRunning}
	live := node.Node{Config: node.Config{ID: "live"}, Status: node.StatusRunning}
	manager := &startupNodeManager{}
	refresher, err := newStartupNodeRefresher(manager, startupNodeStore{state: node.State{Nodes: []node.Node{desired}}, exists: true})
	if err != nil {
		t.Fatalf("newStartupNodeRefresher() error = %v", err)
	}

	if got := refresher.List(); len(got) != 1 || got[0].Config.ID != "desired" {
		t.Fatalf("List() before restore = %#v", got)
	}
	manager.nodes = []node.Node{live}
	if got := refresher.List(); len(got) != 1 || got[0].Config.ID != "live" {
		t.Fatalf("List() after restore = %#v", got)
	}
	manager.nodes = nil
	refresher.MarkRestored()
	if got := refresher.List(); len(got) != 0 {
		t.Fatalf("List() after empty restored state = %#v", got)
	}
}

func TestStartupNodeRefresherRejectsUnreadableState(t *testing.T) {
	_, err := newStartupNodeRefresher(&startupNodeManager{}, startupNodeStore{err: context.Canceled})
	if err == nil {
		t.Fatal("newStartupNodeRefresher() error = nil")
	}
}
