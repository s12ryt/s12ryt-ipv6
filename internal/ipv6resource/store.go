package ipv6resource

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"sync"
)

type Ownership string

const (
	OwnershipAddress  Ownership = "address"
	OwnershipRoute    Ownership = "local-route"
	OwnershipExternal Ownership = "external"
)

type PoolKind string

const (
	PoolInbound           PoolKind = "inbound"
	PoolSharedOutbound    PoolKind = "shared-outbound"
	PoolDedicatedOutbound PoolKind = "dedicated-outbound"
)

type CanonicalAddress struct {
	Address    netip.Addr `yaml:"address"`
	Template   string     `yaml:"template"`
	Ownership  Ownership  `yaml:"ownership"`
	References int        `yaml:"references"`
}

type FixedAddress struct {
	Name      string     `yaml:"name"`
	Template  string     `yaml:"template"`
	Address   netip.Addr `yaml:"address"`
	Ownership Ownership  `yaml:"ownership"`
}

type DrainBatch struct {
	ID        string       `yaml:"id"`
	Addresses []netip.Addr `yaml:"addresses"`
}

type Pool struct {
	Name     string       `yaml:"name"`
	Kind     PoolKind     `yaml:"kind"`
	Template string       `yaml:"template"`
	Capacity int          `yaml:"capacity"`
	Pinned   []netip.Addr `yaml:"pinned"`
	Active   []netip.Addr `yaml:"active"`
	Draining []DrainBatch `yaml:"draining"`
}

type Store struct {
	mu        sync.RWMutex
	templates map[string]PrefixTemplate
	fixed     map[string]FixedAddress
	addresses map[netip.Addr]*CanonicalAddress
	pools     map[string]*Pool
	nextBatch uint64
}

func NewStore() *Store {
	return &Store{
		templates: make(map[string]PrefixTemplate),
		fixed:     make(map[string]FixedAddress),
		addresses: make(map[netip.Addr]*CanonicalAddress),
		pools:     make(map[string]*Pool),
	}
}

func (s *Store) AddTemplate(template PrefixTemplate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing := make([]PrefixTemplate, 0, len(s.templates))
	for _, current := range s.templates {
		existing = append(existing, current)
	}
	if err := ValidateTemplateSet(existing, template); err != nil {
		return err
	}
	s.templates[template.Name] = template
	return nil
}

func (s *Store) DeleteTemplate(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.templates[name]; !exists {
		return fmt.Errorf("template %q does not exist", name)
	}
	refs := s.templateReferences(name)
	if len(refs) > 0 {
		return fmt.Errorf("template %q is referenced by %v", name, refs)
	}
	delete(s.templates, name)
	return nil
}

func (s *Store) CreateFixedAddress(name, templateName string, address netip.Addr, ownership Ownership) (FixedAddress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "" {
		return FixedAddress{}, errors.New("fixed address name is required")
	}
	if _, exists := s.fixed[name]; exists {
		return FixedAddress{}, fmt.Errorf("fixed address name %q already exists", name)
	}
	template, exists := s.templates[templateName]
	if !exists {
		return FixedAddress{}, fmt.Errorf("template %q does not exist", templateName)
	}
	if !address.IsValid() || !address.Is6() || !template.Prefix.Contains(address) {
		return FixedAddress{}, fmt.Errorf("address %s is outside template %q", address, templateName)
	}
	if !validOwnership(ownership) {
		return FixedAddress{}, fmt.Errorf("unsupported ownership %q", ownership)
	}
	if expected := ownershipForMode(template.Mode); ownership != expected {
		return FixedAddress{}, fmt.Errorf("ownership %q does not match template mode %q", ownership, template.Mode)
	}
	if _, exists := s.addresses[address]; exists {
		return FixedAddress{}, fmt.Errorf("address %s already exists", address)
	}

	fixed := FixedAddress{Name: name, Template: templateName, Address: address, Ownership: ownership}
	s.fixed[name] = fixed
	s.addresses[address] = &CanonicalAddress{
		Address: address, Template: templateName, Ownership: ownership, References: 1,
	}
	return fixed, nil
}

func (s *Store) DeleteFixedAddress(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fixed, exists := s.fixed[name]
	if !exists {
		return fmt.Errorf("fixed address %q does not exist", name)
	}
	canonical := s.addresses[fixed.Address]
	if canonical.References > 1 {
		return fmt.Errorf("fixed address %q has %d references", name, canonical.References)
	}
	delete(s.addresses, fixed.Address)
	delete(s.fixed, name)
	return nil
}

func (s *Store) CreatePool(name string, kind PoolKind, templateName string, capacity int, pinnedNames []string) (*Pool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "" {
		return nil, errors.New("pool name is required")
	}
	if _, exists := s.pools[name]; exists {
		return nil, fmt.Errorf("pool %q already exists", name)
	}
	if !validPoolKind(kind) {
		return nil, fmt.Errorf("unsupported pool kind %q", kind)
	}
	template, exists := s.templates[templateName]
	if !exists {
		return nil, fmt.Errorf("template %q does not exist", templateName)
	}
	if capacity < 1 || capacity > MaxPoolSize {
		return nil, fmt.Errorf("pool capacity must be between 1 and %d", MaxPoolSize)
	}

	pinned, err := s.resolvePinned(templateName, pinnedNames)
	if err != nil {
		return nil, err
	}
	if len(pinned) > capacity {
		return nil, errors.New("pinned addresses exceed pool capacity")
	}
	automatic, err := GenerateAddresses(template.Prefix, capacity-len(pinned), s.occupied())
	if capacity == len(pinned) {
		automatic, err = nil, nil
	}
	if err != nil {
		return nil, err
	}

	active := append(append([]netip.Addr(nil), pinned...), automatic...)
	pool := &Pool{Name: name, Kind: kind, Template: templateName, Capacity: capacity, Pinned: pinned, Active: active}
	for _, address := range pinned {
		s.addresses[address].References++
	}
	for _, address := range automatic {
		s.addresses[address] = &CanonicalAddress{
			Address: address, Template: templateName, Ownership: ownershipForMode(template.Mode), References: 1,
		}
	}
	s.pools[name] = pool
	return clonePool(pool), nil
}

func (s *Store) RefreshPool(name string) (*Pool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pool, exists := s.pools[name]
	if !exists {
		return nil, fmt.Errorf("pool %q does not exist", name)
	}
	template := s.templates[pool.Template]
	automaticCount := pool.Capacity - len(pool.Pinned)
	newAutomatic, err := GenerateAddresses(template.Prefix, automaticCount, s.occupied())
	if automaticCount == 0 {
		newAutomatic, err = nil, nil
	}
	if err != nil {
		return nil, err
	}

	oldAutomatic := difference(pool.Active, pool.Pinned)
	for _, address := range newAutomatic {
		s.addresses[address] = &CanonicalAddress{
			Address: address, Template: pool.Template, Ownership: ownershipForMode(template.Mode), References: 1,
		}
	}
	pool.Active = append(append([]netip.Addr(nil), pool.Pinned...), newAutomatic...)
	if len(oldAutomatic) > 0 {
		s.nextBatch++
		pool.Draining = append(pool.Draining, DrainBatch{
			ID: fmt.Sprintf("drain-%d", s.nextBatch), Addresses: oldAutomatic,
		})
	}
	return clonePool(pool), nil
}

func (s *Store) DeletePool(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pool, exists := s.pools[name]
	if !exists {
		return fmt.Errorf("pool %q does not exist", name)
	}
	releases := make(map[netip.Addr]int)
	for _, address := range pool.Pinned {
		releases[address]++
	}
	for _, address := range difference(pool.Active, pool.Pinned) {
		releases[address]++
	}
	for _, batch := range pool.Draining {
		for _, address := range batch.Addresses {
			releases[address]++
		}
	}
	for address, count := range releases {
		canonical := s.addresses[address]
		if canonical == nil || canonical.References < count {
			return fmt.Errorf("pool %q has inconsistent address ownership for %s", name, address)
		}
	}
	for address, count := range releases {
		canonical := s.addresses[address]
		canonical.References -= count
		if canonical.References == 0 {
			delete(s.addresses, address)
		}
	}
	delete(s.pools, name)
	return nil
}

func (s *Store) CompleteDrain(poolName, batchID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pool, exists := s.pools[poolName]
	if !exists {
		return fmt.Errorf("pool %q does not exist", poolName)
	}
	for i, batch := range pool.Draining {
		if batch.ID != batchID {
			continue
		}
		for _, address := range batch.Addresses {
			canonical := s.addresses[address]
			if canonical == nil {
				continue
			}
			canonical.References--
			if canonical.References == 0 {
				delete(s.addresses, address)
			}
		}
		pool.Draining = append(pool.Draining[:i], pool.Draining[i+1:]...)
		return nil
	}
	return fmt.Errorf("draining batch %q does not exist", batchID)
}

func (s *Store) CompleteDrainedAddress(poolName string, address netip.Addr) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	address = address.Unmap()
	pool, exists := s.pools[poolName]
	if !exists {
		return false, fmt.Errorf("pool %q does not exist", poolName)
	}
	for batchIndex := range pool.Draining {
		batch := &pool.Draining[batchIndex]
		for addressIndex, draining := range batch.Addresses {
			if draining != address {
				continue
			}
			canonical := s.addresses[address]
			if canonical == nil || canonical.References < 1 {
				return false, fmt.Errorf("pool %q has inconsistent draining address %s", poolName, address)
			}
			canonical.References--
			if canonical.References == 0 {
				delete(s.addresses, address)
			}
			batch.Addresses = append(batch.Addresses[:addressIndex], batch.Addresses[addressIndex+1:]...)
			if len(batch.Addresses) == 0 {
				pool.Draining = append(pool.Draining[:batchIndex], pool.Draining[batchIndex+1:]...)
				return true, nil
			}
			return false, nil
		}
	}
	return false, fmt.Errorf("draining address %s does not exist in pool %q", address, poolName)
}

func (s *Store) Address(address netip.Addr) *CanonicalAddress {
	s.mu.RLock()
	defer s.mu.RUnlock()
	canonical := s.addresses[address]
	if canonical == nil {
		return nil
	}
	copy := *canonical
	return &copy
}

func (s *Store) Pool(name string) *Pool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clonePool(s.pools[name])
}

func (s *Store) Templates() []PrefixTemplate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]PrefixTemplate, 0, len(s.templates))
	for _, template := range s.templates {
		result = append(result, template)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (s *Store) FixedAddresses() []FixedAddress {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]FixedAddress, 0, len(s.fixed))
	for _, fixed := range s.fixed {
		result = append(result, fixed)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (s *Store) Pools() []*Pool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Pool, 0, len(s.pools))
	for _, pool := range s.pools {
		result = append(result, clonePool(pool))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (s *Store) Addresses() []CanonicalAddress {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]CanonicalAddress, 0, len(s.addresses))
	for _, address := range s.addresses {
		result = append(result, *address)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Address.Compare(result[j].Address) < 0 })
	return result
}

func (s *Store) resolvePinned(templateName string, names []string) ([]netip.Addr, error) {
	addresses := make([]netip.Addr, 0, len(names))
	seen := make(map[netip.Addr]struct{}, len(names))
	for _, name := range names {
		fixed, exists := s.fixed[name]
		if !exists {
			return nil, fmt.Errorf("fixed address %q does not exist", name)
		}
		if fixed.Template != templateName {
			return nil, fmt.Errorf("fixed address %q belongs to template %q", name, fixed.Template)
		}
		if _, duplicate := seen[fixed.Address]; duplicate {
			return nil, fmt.Errorf("fixed address %q is pinned more than once", name)
		}
		seen[fixed.Address] = struct{}{}
		addresses = append(addresses, fixed.Address)
	}
	return addresses, nil
}

func (s *Store) occupied() map[netip.Addr]struct{} {
	occupied := make(map[netip.Addr]struct{}, len(s.addresses))
	for address := range s.addresses {
		occupied[address] = struct{}{}
	}
	return occupied
}

func (s *Store) templateReferences(name string) []string {
	var references []string
	for fixedName, fixed := range s.fixed {
		if fixed.Template == name {
			references = append(references, "fixed:"+fixedName)
		}
	}
	for poolName, pool := range s.pools {
		if pool.Template == name {
			references = append(references, "pool:"+poolName)
		}
	}
	sort.Strings(references)
	return references
}

func ownershipForMode(mode ConfigMode) Ownership {
	switch mode {
	case ModeExternal:
		return OwnershipExternal
	case ModeLocalRouteFreebind:
		return OwnershipRoute
	default:
		return OwnershipAddress
	}
}

func validOwnership(ownership Ownership) bool {
	switch ownership {
	case OwnershipAddress, OwnershipRoute, OwnershipExternal:
		return true
	default:
		return false
	}
}

func validPoolKind(kind PoolKind) bool {
	switch kind {
	case PoolInbound, PoolSharedOutbound, PoolDedicatedOutbound:
		return true
	default:
		return false
	}
}

func difference(addresses, excluded []netip.Addr) []netip.Addr {
	set := make(map[netip.Addr]struct{}, len(excluded))
	for _, address := range excluded {
		set[address] = struct{}{}
	}
	result := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		if _, found := set[address]; !found {
			result = append(result, address)
		}
	}
	return result
}

func clonePool(pool *Pool) *Pool {
	if pool == nil {
		return nil
	}
	copy := *pool
	copy.Pinned = append([]netip.Addr(nil), pool.Pinned...)
	copy.Active = append([]netip.Addr(nil), pool.Active...)
	copy.Draining = make([]DrainBatch, len(pool.Draining))
	for i, batch := range pool.Draining {
		copy.Draining[i] = DrainBatch{ID: batch.ID, Addresses: append([]netip.Addr(nil), batch.Addresses...)}
	}
	return &copy
}
