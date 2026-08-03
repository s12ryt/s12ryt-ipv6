package ipv6resource

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

type State struct {
	Templates []PrefixTemplate   `yaml:"templates"`
	Fixed     []FixedAddress     `yaml:"fixed"`
	Addresses []CanonicalAddress `yaml:"addresses"`
	Pools     []Pool             `yaml:"pools"`
	NextBatch uint64             `yaml:"next_batch"`
}

func (s *Store) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stateLocked()
}

func NewStoreFromState(state State) (*Store, error) {
	store, err := buildStoreFromState(state)
	if err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) ReplaceState(state State) error {
	replacement, err := buildStoreFromState(state)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.templates = replacement.templates
	s.fixed = replacement.fixed
	s.addresses = replacement.addresses
	s.pools = replacement.pools
	s.nextBatch = replacement.nextBatch
	return nil
}

func (s *Store) Template(name string) (PrefixTemplate, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	template, exists := s.templates[name]
	return template, exists
}

func (s *Store) stateLocked() State {
	state := State{NextBatch: s.nextBatch}
	state.Templates = make([]PrefixTemplate, 0, len(s.templates))
	for _, template := range s.templates {
		state.Templates = append(state.Templates, template)
	}
	sort.Slice(state.Templates, func(i, j int) bool { return state.Templates[i].Name < state.Templates[j].Name })

	state.Fixed = make([]FixedAddress, 0, len(s.fixed))
	for _, fixed := range s.fixed {
		state.Fixed = append(state.Fixed, fixed)
	}
	sort.Slice(state.Fixed, func(i, j int) bool { return state.Fixed[i].Name < state.Fixed[j].Name })

	state.Addresses = make([]CanonicalAddress, 0, len(s.addresses))
	for _, address := range s.addresses {
		state.Addresses = append(state.Addresses, *address)
	}
	sort.Slice(state.Addresses, func(i, j int) bool {
		return state.Addresses[i].Address.Compare(state.Addresses[j].Address) < 0
	})

	state.Pools = make([]Pool, 0, len(s.pools))
	for _, pool := range s.pools {
		state.Pools = append(state.Pools, *clonePool(pool))
	}
	sort.Slice(state.Pools, func(i, j int) bool { return state.Pools[i].Name < state.Pools[j].Name })
	return state
}

func buildStoreFromState(state State) (*Store, error) {
	store := NewStore()
	store.nextBatch = state.NextBatch
	for _, template := range state.Templates {
		validated, err := NewPrefixTemplate(template.Name, template.Prefix.String(), template.Interface, template.Mode)
		if err != nil || validated != template {
			if err == nil {
				err = errors.New("template is not canonical")
			}
			return nil, fmt.Errorf("validate template %q: %w", template.Name, err)
		}
		if err := store.AddTemplate(template); err != nil {
			return nil, err
		}
	}

	for _, address := range state.Addresses {
		address.Address = address.Address.Unmap()
		template, exists := store.templates[address.Template]
		if !exists || !address.Address.Is6() || address.Address.Is4In6() || !template.Prefix.Contains(address.Address) {
			return nil, fmt.Errorf("canonical address %s has an invalid template", address.Address)
		}
		if !validOwnership(address.Ownership) || address.Ownership != ownershipForMode(template.Mode) || address.References < 1 {
			return nil, fmt.Errorf("canonical address %s has invalid ownership or references", address.Address)
		}
		if _, exists := store.addresses[address.Address]; exists {
			return nil, fmt.Errorf("canonical address %s is duplicated", address.Address)
		}
		copy := address
		store.addresses[address.Address] = &copy
	}

	expectedReferences := make(map[netip.Addr]int, len(store.addresses))
	fixedAddresses := make(map[netip.Addr]struct{}, len(state.Fixed))
	for _, fixed := range state.Fixed {
		if strings.TrimSpace(fixed.Name) == "" {
			return nil, errors.New("fixed address name is required")
		}
		if _, exists := store.fixed[fixed.Name]; exists {
			return nil, fmt.Errorf("fixed address name %q is duplicated", fixed.Name)
		}
		canonical := store.addresses[fixed.Address.Unmap()]
		if canonical == nil || canonical.Template != fixed.Template || canonical.Ownership != fixed.Ownership {
			return nil, fmt.Errorf("fixed address %q has inconsistent canonical address", fixed.Name)
		}
		fixed.Address = fixed.Address.Unmap()
		if _, duplicate := fixedAddresses[fixed.Address]; duplicate {
			return nil, fmt.Errorf("canonical address %s is used by more than one fixed address", fixed.Address)
		}
		store.fixed[fixed.Name] = fixed
		fixedAddresses[fixed.Address] = struct{}{}
		expectedReferences[fixed.Address]++
	}

	batchIDs := make(map[string]struct{})
	var highestBatch uint64
	for _, value := range state.Pools {
		pool := clonePool(&value)
		if strings.TrimSpace(pool.Name) == "" || !validPoolKind(pool.Kind) {
			return nil, fmt.Errorf("pool %q has invalid name or kind", pool.Name)
		}
		if _, exists := store.pools[pool.Name]; exists {
			return nil, fmt.Errorf("pool name %q is duplicated", pool.Name)
		}
		if _, exists := store.templates[pool.Template]; !exists {
			return nil, fmt.Errorf("pool %q references missing template %q", pool.Name, pool.Template)
		}
		if pool.Capacity < 1 || pool.Capacity > MaxPoolSize || len(pool.Active) != pool.Capacity || len(pool.Pinned) > pool.Capacity {
			return nil, fmt.Errorf("pool %q has invalid capacity", pool.Name)
		}

		active := make(map[netip.Addr]struct{}, len(pool.Active))
		for i, address := range pool.Active {
			address = address.Unmap()
			pool.Active[i] = address
			if _, duplicate := active[address]; duplicate {
				return nil, fmt.Errorf("pool %q has duplicate active address %s", pool.Name, address)
			}
			if err := validatePoolAddress(store, pool, address); err != nil {
				return nil, err
			}
			active[address] = struct{}{}
		}
		pinned := make(map[netip.Addr]struct{}, len(pool.Pinned))
		for i, address := range pool.Pinned {
			address = address.Unmap()
			pool.Pinned[i] = address
			if _, duplicate := pinned[address]; duplicate {
				return nil, fmt.Errorf("pool %q has duplicate pinned address %s", pool.Name, address)
			}
			if _, exists := active[address]; !exists {
				return nil, fmt.Errorf("pool %q pinned address %s is not active", pool.Name, address)
			}
			if _, exists := fixedAddresses[address]; !exists {
				return nil, fmt.Errorf("pool %q pinned address %s is not fixed", pool.Name, address)
			}
			pinned[address] = struct{}{}
			expectedReferences[address]++
		}
		for address := range active {
			if _, isPinned := pinned[address]; !isPinned {
				expectedReferences[address]++
			}
		}

		draining := make(map[netip.Addr]struct{})
		for batchIndex := range pool.Draining {
			batch := &pool.Draining[batchIndex]
			sequence, err := parseDrainSequence(batch.ID)
			if err != nil {
				return nil, fmt.Errorf("pool %q: %w", pool.Name, err)
			}
			if _, duplicate := batchIDs[batch.ID]; duplicate {
				return nil, fmt.Errorf("drain batch ID %q is duplicated", batch.ID)
			}
			batchIDs[batch.ID] = struct{}{}
			if sequence > highestBatch {
				highestBatch = sequence
			}
			if len(batch.Addresses) == 0 {
				return nil, fmt.Errorf("drain batch %q is empty", batch.ID)
			}
			for i, address := range batch.Addresses {
				address = address.Unmap()
				batch.Addresses[i] = address
				if _, exists := active[address]; exists {
					return nil, fmt.Errorf("pool %q draining address %s is still active", pool.Name, address)
				}
				if _, duplicate := draining[address]; duplicate {
					return nil, fmt.Errorf("pool %q has duplicate draining address %s", pool.Name, address)
				}
				if err := validatePoolAddress(store, pool, address); err != nil {
					return nil, err
				}
				draining[address] = struct{}{}
				expectedReferences[address]++
			}
		}
		store.pools[pool.Name] = pool
	}
	if highestBatch > state.NextBatch {
		return nil, fmt.Errorf("next drain batch %d precedes persisted batch %d", state.NextBatch, highestBatch)
	}
	for address, canonical := range store.addresses {
		if expectedReferences[address] != canonical.References {
			return nil, fmt.Errorf("canonical address %s references = %d, want %d", address, canonical.References, expectedReferences[address])
		}
	}
	return store, nil
}

func validatePoolAddress(store *Store, pool *Pool, address netip.Addr) error {
	canonical := store.addresses[address]
	if canonical == nil || canonical.Template != pool.Template {
		return fmt.Errorf("pool %q address %s has inconsistent canonical address", pool.Name, address)
	}
	return nil
}

func parseDrainSequence(id string) (uint64, error) {
	const prefix = "drain-"
	if !strings.HasPrefix(id, prefix) {
		return 0, fmt.Errorf("invalid drain batch ID %q", id)
	}
	sequence, err := strconv.ParseUint(strings.TrimPrefix(id, prefix), 10, 64)
	if err != nil || sequence == 0 {
		return 0, fmt.Errorf("invalid drain batch ID %q", id)
	}
	return sequence, nil
}
