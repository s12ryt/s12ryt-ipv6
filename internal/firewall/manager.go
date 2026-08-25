package firewall

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"sync"
)

const OwnedTableName = "s12ryt_ipv6"

type Protocol string

const (
	ProtocolTCP Protocol = "tcp"
	ProtocolUDP Protocol = "udp"
)

type AddressFamily string

const (
	FamilyIPv4 AddressFamily = "ipv4"
	FamilyIPv6 AddressFamily = "ipv6"
)

type TableFamily string

const TableFamilyINet TableFamily = "inet"

type Opening struct {
	Protocol Protocol
	Family   AddressFamily
	Address  netip.Addr
	Port     uint16
	// PortEnd > 0 opens the inclusive port range [Port, PortEnd] instead of a
	// single port. UDP relay scopes use this to cover the allocator range with
	// one rule instead of one rule per association.
	PortEnd uint16
	Purpose string
}

type Ruleset struct {
	Family   TableFamily
	Table    string
	Openings []Opening
}

type Diagnosis struct {
	Degraded bool
	Blockers []string
}

type Backend interface {
	Apply(context.Context, Ruleset) error
	Delete(context.Context, string) error
	Diagnose(context.Context) (Diagnosis, error)
}

type Manager struct {
	backend Backend
	mu      sync.RWMutex
	state   Ruleset
}

func NewManager(backend Backend) (*Manager, error) {
	if backend == nil {
		return nil, errors.New("firewall backend is required")
	}
	return &Manager{
		backend: backend,
		state:   Ruleset{Family: TableFamilyINet, Table: OwnedTableName},
	}, nil
}

func (m *Manager) Replace(ctx context.Context, openings []Opening) error {
	normalized, err := normalizeOpenings(openings)
	if err != nil {
		return err
	}
	next := Ruleset{Family: TableFamilyINet, Table: OwnedTableName, Openings: normalized}

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.backend.Apply(ctx, cloneRuleset(next)); err != nil {
		return fmt.Errorf("apply owned firewall table: %w", err)
	}
	m.state = next
	return nil
}

func (m *Manager) State() Ruleset {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneRuleset(m.state)
}

func (m *Manager) Diagnose(ctx context.Context) (Diagnosis, error) {
	diagnosis, err := m.backend.Diagnose(ctx)
	if err != nil {
		return Diagnosis{}, fmt.Errorf("diagnose host firewall: %w", err)
	}
	diagnosis.Blockers = slices.Clone(diagnosis.Blockers)
	return diagnosis, nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.backend.Delete(ctx, OwnedTableName); err != nil {
		return fmt.Errorf("delete owned firewall table: %w", err)
	}
	m.state = Ruleset{Family: TableFamilyINet, Table: OwnedTableName}
	return nil
}

func normalizeOpenings(openings []Opening) ([]Opening, error) {
	type endpoint struct {
		protocol Protocol
		family   AddressFamily
		address  netip.Addr
		port     uint16
		portEnd  uint16
	}
	seen := make(map[endpoint]struct{}, len(openings))
	normalized := make([]Opening, 0, len(openings))
	for _, opening := range openings {
		if opening.Protocol != ProtocolTCP && opening.Protocol != ProtocolUDP {
			return nil, fmt.Errorf("unsupported firewall protocol %q", opening.Protocol)
		}
		if opening.Family != FamilyIPv4 && opening.Family != FamilyIPv6 {
			return nil, fmt.Errorf("unsupported firewall address family %q", opening.Family)
		}
		if opening.Port == 0 {
			return nil, errors.New("firewall port must be non-zero")
		}
		if opening.PortEnd != 0 && opening.PortEnd < opening.Port {
			return nil, fmt.Errorf("firewall port range end %d is below start %d", opening.PortEnd, opening.Port)
		}
		if opening.Address.IsValid() {
			opening.Address = opening.Address.Unmap()
			if opening.Family == FamilyIPv4 && !opening.Address.Is4() {
				return nil, fmt.Errorf("address %s does not match IPv4 opening", opening.Address)
			}
			if opening.Family == FamilyIPv6 && !opening.Address.Is6() {
				return nil, fmt.Errorf("address %s does not match IPv6 opening", opening.Address)
			}
		}
		key := endpoint{opening.Protocol, opening.Family, opening.Address, opening.Port, opening.PortEnd}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, opening)
	}
	sort.Slice(normalized, func(i, j int) bool {
		left, right := normalized[i], normalized[j]
		if left.Family != right.Family {
			return left.Family < right.Family
		}
		if left.Protocol != right.Protocol {
			return left.Protocol < right.Protocol
		}
		if left.Address != right.Address {
			return left.Address.Compare(right.Address) < 0
		}
		if left.Port != right.Port {
			return left.Port < right.Port
		}
		return left.PortEnd < right.PortEnd
	})
	return normalized, nil
}

func cloneRuleset(rules Ruleset) Ruleset {
	rules.Openings = slices.Clone(rules.Openings)
	return rules
}
