package stats

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type NodeCounters struct {
	ActiveTCP        uint64 `json:"active_tcp"`
	ActiveUDP        uint64 `json:"active_udp"`
	TotalConnections uint64 `json:"total_connections"`
	BytesUp          uint64 `json:"bytes_up"`
	BytesDown        uint64 `json:"bytes_down"`
	Errors           uint64 `json:"errors"`
}

type Snapshot struct {
	Nodes map[string]NodeCounters `json:"nodes"`
}

type Registry struct {
	mu    sync.RWMutex
	nodes map[string]NodeCounters
}

func NewRegistry() *Registry {
	return &Registry{nodes: make(map[string]NodeCounters)}
}

func NewRegistryFromSnapshot(snapshot Snapshot) (*Registry, error) {
	nodes := make(map[string]NodeCounters, len(snapshot.Nodes))
	for node, counters := range snapshot.Nodes {
		if strings.TrimSpace(node) == "" {
			return nil, errors.New("statistics node ID is required")
		}
		counters.ActiveTCP = 0
		counters.ActiveUDP = 0
		nodes[node] = counters
	}
	return &Registry{nodes: nodes}, nil
}

func (r *Registry) TCPOpened(node string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	counters := r.nodes[node]
	counters.ActiveTCP++
	counters.TotalConnections++
	r.nodes[node] = counters
}

func (r *Registry) TCPClosed(node string, bytesUp, bytesDown uint64, failed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	counters := r.nodes[node]
	if counters.ActiveTCP > 0 {
		counters.ActiveTCP--
	}
	counters.BytesUp += bytesUp
	counters.BytesDown += bytesDown
	if failed {
		counters.Errors++
	}
	r.nodes[node] = counters
}

func (r *Registry) UDPAssociationOpened(node string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	counters := r.nodes[node]
	counters.ActiveUDP++
	counters.TotalConnections++
	r.nodes[node] = counters
}

func (r *Registry) UDPAssociationClosed(node string, bytesUp, bytesDown uint64, failed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	counters := r.nodes[node]
	if counters.ActiveUDP > 0 {
		counters.ActiveUDP--
	}
	counters.BytesUp += bytesUp
	counters.BytesDown += bytesDown
	if failed {
		counters.Errors++
	}
	r.nodes[node] = counters
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	nodes := make(map[string]NodeCounters, len(r.nodes))
	for node, counters := range r.nodes {
		nodes[node] = counters
	}
	return Snapshot{Nodes: nodes}
}

func (r *Registry) ResetNode(node string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	counters := r.nodes[node]
	r.nodes[node] = NodeCounters{ActiveTCP: counters.ActiveTCP, ActiveUDP: counters.ActiveUDP}
}

// RemoveNode drops a node's counters entirely. Callers use it when the node is
// permanently deleted so stale entries stop reappearing in every saved
// snapshot; unknown or empty IDs are ignored.
func (r *Registry) RemoveNode(node string) {
	if node == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.nodes, node)
}

func (r *Registry) ResetAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for node, counters := range r.nodes {
		r.nodes[node] = NodeCounters{ActiveTCP: counters.ActiveTCP, ActiveUDP: counters.ActiveUDP}
	}
}

func Save(path string, snapshot Snapshot) error {
	if path == "" {
		return errors.New("stats path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create stats directory: %w", err)
	}
	persisted := Snapshot{Nodes: make(map[string]NodeCounters, len(snapshot.Nodes))}
	for node, counters := range snapshot.Nodes {
		counters.ActiveTCP = 0
		counters.ActiveUDP = 0
		persisted.Nodes[node] = counters
	}
	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("encode stats: %w", err)
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".stats-*")
	if err != nil {
		return fmt.Errorf("create temporary stats file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("set stats permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write stats: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync stats: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close stats: %w", err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("replace stats: %w", err)
	}
	return nil
}

func Load(path string) (Snapshot, error) {
	file, err := os.Open(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open stats: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var snapshot Snapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decode stats: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Snapshot{}, errors.New("stats contain trailing JSON values")
		}
		return Snapshot{}, fmt.Errorf("decode trailing stats data: %w", err)
	}
	if snapshot.Nodes == nil {
		snapshot.Nodes = make(map[string]NodeCounters)
	}
	for node, counters := range snapshot.Nodes {
		counters.ActiveTCP = 0
		counters.ActiveUDP = 0
		snapshot.Nodes[node] = counters
	}
	return snapshot, nil
}
