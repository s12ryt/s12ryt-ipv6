package proxy

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"slices"
	"sync"
)

var (
	ErrSourceDrainForced = errors.New("source address drain was forced")
	ErrSourceNotDraining = errors.New("source address is not draining")
)

type SourcePool struct {
	mu        sync.Mutex
	current   []netip.Addr
	next      uint64
	active    map[netip.Addr]map[*SourceLease]struct{}
	draining  map[netip.Addr]struct{}
	onDrained func(netip.Addr)
}

type SourceLease struct {
	pool     *SourcePool
	address  netip.Addr
	mu       sync.Mutex
	closer   io.Closer
	forced   bool
	released bool
	once     sync.Once
}

func NewSourcePool(addresses []netip.Addr, onDrained func(netip.Addr)) (*SourcePool, error) {
	normalized, err := normalizeSourceAddresses(addresses)
	if err != nil {
		return nil, err
	}
	return &SourcePool{
		current:   normalized,
		active:    make(map[netip.Addr]map[*SourceLease]struct{}),
		draining:  make(map[netip.Addr]struct{}),
		onDrained: onDrained,
	}, nil
}

func (p *SourcePool) Acquire() (*SourceLease, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.current) == 0 {
		return nil, errors.New("source address pool is empty")
	}
	address := p.current[p.next%uint64(len(p.current))]
	p.next++
	lease := &SourceLease{pool: p, address: address}
	if p.active[address] == nil {
		p.active[address] = make(map[*SourceLease]struct{})
	}
	p.active[address][lease] = struct{}{}
	return lease, nil
}

func (p *SourcePool) Replace(addresses []netip.Addr) error {
	normalized, err := normalizeSourceAddresses(addresses)
	if err != nil {
		return err
	}

	p.mu.Lock()
	if slices.Equal(p.current, normalized) {
		// The reported active set is unchanged. Runtime synchronisation runs
		// after every resource transaction and drain completion, so treating
		// that as a rotation would restart round-robin selection from the top
		// and repeatedly reuse only the first addresses. Keep the cursor.
		p.mu.Unlock()
		return nil
	}
	newSet := make(map[netip.Addr]struct{}, len(normalized))
	for _, address := range normalized {
		newSet[address] = struct{}{}
	}

	ready := make([]netip.Addr, 0)
	for _, address := range p.current {
		if _, keep := newSet[address]; keep {
			continue
		}
		if len(p.active[address]) > 0 {
			p.draining[address] = struct{}{}
		} else {
			ready = append(ready, address)
		}
	}
	for _, address := range normalized {
		delete(p.draining, address)
	}
	p.current = normalized
	p.next = 0
	callback := p.onDrained
	p.mu.Unlock()

	if callback != nil {
		for _, address := range ready {
			callback(address)
		}
	}
	return nil
}

func (p *SourcePool) Draining() map[netip.Addr]uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make(map[netip.Addr]uint64, len(p.draining))
	for address := range p.draining {
		result[address] = uint64(len(p.active[address]))
	}
	return result
}

func (p *SourcePool) ForceDrain(address netip.Addr) error {
	address = address.Unmap()
	p.mu.Lock()
	if _, draining := p.draining[address]; !draining {
		p.mu.Unlock()
		return ErrSourceNotDraining
	}
	leases := make([]*SourceLease, 0, len(p.active[address]))
	for lease := range p.active[address] {
		leases = append(leases, lease)
	}
	p.mu.Unlock()

	var closeErrors []error
	for _, lease := range leases {
		if err := lease.force(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	return errors.Join(closeErrors...)
}

func (l *SourceLease) Address() netip.Addr {
	if l == nil {
		return netip.Addr{}
	}
	return l.address
}

func (l *SourceLease) Attach(closer io.Closer) error {
	if closer == nil {
		return errors.New("source lease closer is required")
	}
	if l == nil || l.pool == nil {
		closeErr := closer.Close()
		return errors.Join(errors.New("source lease is unavailable"), closeErr)
	}
	l.mu.Lock()
	if l.closer != nil {
		l.mu.Unlock()
		closeErr := closer.Close()
		return errors.Join(errors.New("source lease already has a connection"), closeErr)
	}
	if l.forced {
		l.mu.Unlock()
		closeErr := closer.Close()
		return errors.Join(ErrSourceDrainForced, closeErr)
	}
	if l.released {
		l.mu.Unlock()
		closeErr := closer.Close()
		return errors.Join(errors.New("source lease was released"), closeErr)
	}
	l.closer = closer
	l.mu.Unlock()
	return nil
}

func (l *SourceLease) Release() {
	if l == nil || l.pool == nil {
		return
	}
	l.once.Do(func() {
		l.mu.Lock()
		l.released = true
		l.mu.Unlock()
		l.pool.release(l)
	})
}

func (l *SourceLease) force() error {
	if l == nil || l.pool == nil {
		return nil
	}
	l.mu.Lock()
	if l.forced {
		l.mu.Unlock()
		return nil
	}
	l.forced = true
	closer := l.closer
	l.mu.Unlock()

	var closeErr error
	if closer != nil {
		closeErr = closer.Close()
	}
	l.Release()
	if closeErr != nil {
		return fmt.Errorf("close forced source lease: %w", closeErr)
	}
	return nil
}

func (p *SourcePool) release(lease *SourceLease) {
	p.mu.Lock()
	address := lease.address
	leases := p.active[address]
	if _, exists := leases[lease]; !exists {
		p.mu.Unlock()
		return
	}
	delete(leases, lease)
	if len(leases) > 0 {
		p.mu.Unlock()
		return
	}
	delete(p.active, address)
	_, wasDraining := p.draining[address]
	if wasDraining {
		delete(p.draining, address)
	}
	callback := p.onDrained
	p.mu.Unlock()

	if wasDraining && callback != nil {
		callback(address)
	}
}

func normalizeSourceAddresses(addresses []netip.Addr) ([]netip.Addr, error) {
	if len(addresses) == 0 {
		return nil, errors.New("at least one source IPv6 address is required")
	}
	normalized := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !address.IsValid() || !address.Is6() || address.Is4In6() {
			return nil, errors.New("source address must be native IPv6")
		}
		if _, exists := seen[address]; exists {
			return nil, errors.New("source addresses must be unique")
		}
		seen[address] = struct{}{}
		normalized = append(normalized, address)
	}
	return slices.Clone(normalized), nil
}
