//go:build linux

package firewall

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

type nftConnection interface {
	ListTables() ([]*nftables.Table, error)
	ListChains() ([]*nftables.Chain, error)
	GetRules(*nftables.Table, *nftables.Chain) ([]*nftables.Rule, error)
	DelTable(*nftables.Table)
	AddTable(*nftables.Table) *nftables.Table
	AddChain(*nftables.Chain) *nftables.Chain
	AddRule(*nftables.Rule) *nftables.Rule
	Flush() error
}

type nftConnectionFactory func() (nftConnection, error)

type nftBackend struct {
	connect nftConnectionFactory
}

func NewNftBackend() (Backend, error) {
	return newNftBackend(func() (nftConnection, error) { return nftables.New() }), nil
}

func newNftBackend(connect nftConnectionFactory) *nftBackend {
	return &nftBackend{connect: connect}
}

func (b *nftBackend) Apply(ctx context.Context, rules Ruleset) error {
	if rules.Family != TableFamilyINet || rules.Table != OwnedTableName {
		return errors.New("nftables backend may only manage inet s12ryt_ipv6")
	}
	openings, err := normalizeOpenings(rules.Openings)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	conn, err := b.connect()
	if err != nil {
		return fmt.Errorf("connect to nftables: %w", err)
	}
	tables, err := conn.ListTables()
	if err != nil {
		return fmt.Errorf("list nftables tables: %w", err)
	}
	for _, table := range tables {
		if table.Family == nftables.TableFamilyINet && table.Name == OwnedTableName {
			conn.DelTable(table)
		}
	}

	table := conn.AddTable(&nftables.Table{Family: nftables.TableFamilyINet, Name: OwnedTableName})
	policy := nftables.ChainPolicyAccept
	chain := conn.AddChain(&nftables.Chain{
		Name:     "input",
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookInput,
		Priority: nftables.ChainPriorityFilter,
		Policy:   &policy,
	})
	for _, opening := range openings {
		conn.AddRule(&nftables.Rule{Table: table, Chain: chain, Exprs: openingExpressions(opening)})
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := conn.Flush(); err != nil {
		return fmt.Errorf("commit nftables table: %w", err)
	}
	return nil
}

func (b *nftBackend) Delete(ctx context.Context, tableName string) error {
	if tableName != OwnedTableName {
		return errors.New("nftables backend may only delete inet s12ryt_ipv6")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	conn, err := b.connect()
	if err != nil {
		return fmt.Errorf("connect to nftables: %w", err)
	}
	tables, err := conn.ListTables()
	if err != nil {
		return fmt.Errorf("list nftables tables: %w", err)
	}
	deleted := false
	for _, table := range tables {
		if table.Family == nftables.TableFamilyINet && table.Name == OwnedTableName {
			conn.DelTable(table)
			deleted = true
		}
	}
	if !deleted {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := conn.Flush(); err != nil {
		return fmt.Errorf("delete nftables table: %w", err)
	}
	return nil
}

func (b *nftBackend) Diagnose(ctx context.Context) (Diagnosis, error) {
	if err := ctx.Err(); err != nil {
		return Diagnosis{}, err
	}
	conn, err := b.connect()
	if err != nil {
		return Diagnosis{}, fmt.Errorf("connect to nftables: %w", err)
	}
	chains, err := conn.ListChains()
	if err != nil {
		return Diagnosis{}, fmt.Errorf("list nftables chains: %w", err)
	}
	blockers := make(map[string]struct{})
	for _, chain := range chains {
		if !isForeignInputBaseChain(chain) {
			continue
		}
		name := fmt.Sprintf("%s/%s/%s", tableFamilyName(chain.Table.Family), chain.Table.Name, chain.Name)
		if chain.Policy != nil && *chain.Policy == nftables.ChainPolicyDrop {
			blockers[name+":policy-drop"] = struct{}{}
		}
		rules, err := conn.GetRules(chain.Table, chain)
		if err != nil {
			return Diagnosis{}, fmt.Errorf("list rules for %s: %w", name, err)
		}
		if containsDropVerdict(rules) {
			blockers[name+":drop-rule"] = struct{}{}
		}
	}
	result := Diagnosis{Degraded: len(blockers) > 0, Blockers: make([]string, 0, len(blockers))}
	for blocker := range blockers {
		result.Blockers = append(result.Blockers, blocker)
	}
	sort.Strings(result.Blockers)
	return result, nil
}

func isForeignInputBaseChain(chain *nftables.Chain) bool {
	if chain == nil || chain.Table == nil || chain.Hooknum == nil || *chain.Hooknum != *nftables.ChainHookInput {
		return false
	}
	if chain.Table.Family != nftables.TableFamilyINet && chain.Table.Family != nftables.TableFamilyIPv4 && chain.Table.Family != nftables.TableFamilyIPv6 {
		return false
	}
	return chain.Table.Family != nftables.TableFamilyINet || chain.Table.Name != OwnedTableName
}

func containsDropVerdict(rules []*nftables.Rule) bool {
	for _, rule := range rules {
		for _, expression := range rule.Exprs {
			if verdict, ok := expression.(*expr.Verdict); ok && verdict.Kind == expr.VerdictDrop {
				return true
			}
		}
	}
	return false
}

func tableFamilyName(family nftables.TableFamily) string {
	switch family {
	case nftables.TableFamilyIPv4:
		return "ip"
	case nftables.TableFamilyIPv6:
		return "ip6"
	case nftables.TableFamilyINet:
		return "inet"
	default:
		return fmt.Sprintf("family-%d", family)
	}
}

func openingExpressions(opening Opening) []expr.Any {
	networkProtocol := byte(unix.NFPROTO_IPV6)
	destinationOffset := uint32(24)
	destinationLength := uint32(16)
	if opening.Family == FamilyIPv4 {
		networkProtocol = byte(unix.NFPROTO_IPV4)
		destinationOffset = 16
		destinationLength = 4
	}
	transportProtocol := byte(unix.IPPROTO_TCP)
	if opening.Protocol == ProtocolUDP {
		transportProtocol = byte(unix.IPPROTO_UDP)
	}
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, opening.Port)

	expressions := []expr.Any{
		&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{networkProtocol}},
		&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{transportProtocol}},
	}
	if opening.Address.IsValid() {
		expressions = append(expressions,
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: destinationOffset, Len: destinationLength},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: opening.Address.AsSlice()},
		)
	}
	expressions = append(expressions,
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: port},
		&expr.Verdict{Kind: expr.VerdictAccept},
	)
	return expressions
}
