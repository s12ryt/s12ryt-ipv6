package app

import (
	"errors"
	"fmt"
	"sync"

	"github.com/s12ryt/s12ryt-ipv6/internal/admin"
	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/node"
)

type protectedResourceStateStore struct {
	mu       sync.RWMutex
	delegate admin.ResourceStateStore
	loadErr  error
}

func newProtectedResourceStateStore(delegate admin.ResourceStateStore) (*protectedResourceStateStore, error) {
	if delegate == nil {
		return nil, errors.New("resource state store is required")
	}
	return &protectedResourceStateStore{delegate: delegate}, nil
}

func (s *protectedResourceStateStore) Load() (ipv6resource.State, bool, error) {
	state, exists, err := s.delegate.Load()
	if err == nil {
		return state, exists, nil
	}
	s.mu.Lock()
	s.loadErr = err
	s.mu.Unlock()
	return ipv6resource.State{}, false, nil
}

func (s *protectedResourceStateStore) Save(state ipv6resource.State) error {
	if err := s.CheckWritable(); err != nil {
		return err
	}
	return s.delegate.Save(state)
}

func (s *protectedResourceStateStore) CheckWritable() error {
	if err := s.StartupError(); err != nil {
		return fmt.Errorf("resource state is read-only after a load failure: %w", err)
	}
	return nil
}

func (s *protectedResourceStateStore) StartupError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadErr
}

type tolerantStartupNodeLoader struct {
	delegate startupNodeStateLoader
}

func (l tolerantStartupNodeLoader) Load() (node.State, bool, error) {
	state, exists, err := l.delegate.Load()
	if err != nil {
		return node.State{}, false, nil
	}
	return state, exists, nil
}
