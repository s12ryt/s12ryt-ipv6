package node

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
)

type NodeStateStore interface {
	Load() (State, bool, error)
	Save(State) error
}

type PersistentManager struct {
	mu       sync.Mutex
	manager  *Manager
	store    NodeStateStore
	restored bool
}

func NewPersistentManager(manager *Manager, store NodeStateStore) (*PersistentManager, error) {
	if manager == nil {
		return nil, errors.New("node manager is required")
	}
	if store == nil {
		return nil, errors.New("node state store is required")
	}
	return &PersistentManager{manager: manager, store: store}, nil
}

func (m *PersistentManager) Restore(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.restored {
		return errors.New("node state has already been restored")
	}
	state, exists, err := m.store.Load()
	if err != nil {
		return fmt.Errorf("load node state: %w", err)
	}
	if !exists {
		state = State{}
	}
	err = m.manager.Restore(ctx, state)
	if err == nil || len(m.manager.List()) != 0 || len(state.Nodes) == 0 {
		m.restored = true
	}
	return err
}

func (m *PersistentManager) Create(ctx context.Context, config Config, confirmUnauthenticated bool) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	created, err := m.manager.Create(ctx, config, confirmUnauthenticated)
	if err != nil {
		return Node{}, err
	}
	if err := m.store.Save(m.manager.State()); err != nil {
		rollbackErr := m.manager.Delete(context.WithoutCancel(ctx), created.Config.ID)
		return Node{}, errors.Join(fmt.Errorf("save node state: %w", err), rollbackErr)
	}
	return created, nil
}

func (m *PersistentManager) CreateBatch(ctx context.Context, configs []Config, confirmUnauthenticated bool) ([]Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	created, err := m.manager.CreateBatch(ctx, configs, confirmUnauthenticated)
	if err != nil {
		return nil, err
	}
	if err := m.store.Save(m.manager.State()); err != nil {
		ids := make([]string, len(created))
		for index, current := range created {
			ids[index] = current.Config.ID
		}
		rollbackErr := m.manager.rollbackBatch(context.WithoutCancel(ctx), ids)
		return nil, errors.Join(fmt.Errorf("save node state: %w", err), rollbackErr)
	}
	return created, nil
}

func (m *PersistentManager) Update(ctx context.Context, id string, config Config, confirmUnauthenticated bool) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	before, found := m.manager.Get(id)
	if !found {
		return Node{}, ErrNodeNotFound
	}
	updated, err := m.manager.Update(ctx, id, config, confirmUnauthenticated)
	if err != nil && !errors.Is(err, ErrPreviousRuntimeCleanup) {
		return Node{}, err
	}
	if saveErr := m.store.Save(m.manager.State()); saveErr != nil {
		_, rollbackErr := m.manager.Update(
			context.WithoutCancel(ctx), id, before.Config, before.Config.Username == "",
		)
		return Node{}, errors.Join(err, fmt.Errorf("save node state: %w", saveErr), rollbackErr)
	}
	return updated, err
}

func (m *PersistentManager) Start(ctx context.Context, id string) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	started, err := m.manager.Start(ctx, id)
	if err != nil {
		return Node{}, err
	}
	if err := m.store.Save(m.manager.State()); err != nil {
		_, rollbackErr := m.manager.Stop(context.WithoutCancel(ctx), id)
		return Node{}, errors.Join(fmt.Errorf("save node state: %w", err), rollbackErr)
	}
	return started, nil
}

func (m *PersistentManager) Stop(ctx context.Context, id string) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	before, found := m.manager.Get(id)
	if !found {
		return Node{}, ErrNodeNotFound
	}
	stopped, err := m.manager.Stop(ctx, id)
	if err != nil {
		return Node{}, err
	}
	if err := m.store.Save(m.manager.State()); err != nil {
		var rollbackErr error
		if before.Status == StatusRunning {
			_, rollbackErr = m.manager.Start(context.WithoutCancel(ctx), id)
		}
		return Node{}, errors.Join(fmt.Errorf("save node state: %w", err), rollbackErr)
	}
	return stopped, nil
}

func (m *PersistentManager) MoveToFolder(ctx context.Context, id, folder string) (Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	before, found := m.manager.Get(id)
	if !found {
		return Node{}, ErrNodeNotFound
	}
	moved, err := m.manager.MoveToFolder(ctx, id, folder)
	if err != nil {
		return Node{}, err
	}
	if err := m.store.Save(m.manager.State()); err != nil {
		_, rollbackErr := m.manager.MoveToFolder(context.WithoutCancel(ctx), id, before.Config.Folder)
		return Node{}, errors.Join(fmt.Errorf("save node state: %w", err), rollbackErr)
	}
	return moved, nil
}

func (m *PersistentManager) RenameFolder(ctx context.Context, source, target string) ([]Node, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	renamed, err := m.manager.RenameFolder(ctx, source, target)
	if err != nil {
		return nil, err
	}
	if err := m.store.Save(m.manager.State()); err != nil {
		_, rollbackErr := m.manager.RenameFolder(context.WithoutCancel(ctx), target, source)
		return nil, errors.Join(fmt.Errorf("save node state: %w", err), rollbackErr)
	}
	return renamed, nil
}

func (m *PersistentManager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	before, found := m.manager.Get(id)
	if !found {
		return ErrNodeNotFound
	}
	if err := m.manager.detach(ctx, id); err != nil {
		return err
	}
	if err := m.store.Save(m.manager.State()); err != nil {
		rollbackErr := m.manager.restoreNode(context.WithoutCancel(ctx), before)
		return errors.Join(fmt.Errorf("save node state: %w", err), rollbackErr)
	}
	if err := m.manager.cleanupDedicatedPool(ctx, before.Config); err != nil {
		stopped := before
		stopped.Status = StatusStopped
		restoreErr := m.manager.restoreNode(context.WithoutCancel(ctx), stopped)
		var saveErr error
		if restoreErr == nil {
			saveErr = m.store.Save(m.manager.State())
		}
		return errors.Join(err, restoreErr, saveErr)
	}
	return nil
}

func (m *PersistentManager) Get(id string) (Node, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.manager.Get(id)
}

func (m *PersistentManager) List() []Node {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.manager.List()
}

func (m *PersistentManager) RefreshInboundBindings(ctx context.Context, resolver InboundConfigResolver, onDrained InboundDrainedObserver) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.manager.RefreshInboundBindings(ctx, resolver, onDrained)
}

func (m *PersistentManager) ForceDrainInbound(ctx context.Context, resource string, addresses []netip.Addr) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.manager.ForceDrainInbound(ctx, resource, addresses)
}

func (m *PersistentManager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	desired := m.manager.State()
	shutdownErr := m.manager.Shutdown(ctx)
	saveErr := m.store.Save(desired)
	if saveErr != nil {
		saveErr = fmt.Errorf("save node state: %w", saveErr)
	}
	return errors.Join(shutdownErr, saveErr)
}
