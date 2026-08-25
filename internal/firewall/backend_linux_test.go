//go:build linux

package firewall

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
)

type fakeNftConn struct {
	tables       []*nftables.Table
	deleted      []*nftables.Table
	added        []*nftables.Table
	chains       []*nftables.Chain
	rules        []*nftables.Rule
	listedChains []*nftables.Chain
	listedRules  map[string][]*nftables.Rule
	flushes      int
	err          error
}

func (f *fakeNftConn) ListTables() ([]*nftables.Table, error) { return f.tables, nil }
func (f *fakeNftConn) DelTable(table *nftables.Table)         { f.deleted = append(f.deleted, table) }
func (f *fakeNftConn) AddTable(table *nftables.Table) *nftables.Table {
	f.added = append(f.added, table)
	return table
}
func (f *fakeNftConn) AddChain(chain *nftables.Chain) *nftables.Chain {
	f.chains = append(f.chains, chain)
	return chain
}
func (f *fakeNftConn) AddRule(rule *nftables.Rule) *nftables.Rule {
	f.rules = append(f.rules, rule)
	return rule
}
func (f *fakeNftConn) Flush() error                           { f.flushes++; return f.err }
func (f *fakeNftConn) ListChains() ([]*nftables.Chain, error) { return f.listedChains, nil }
func (f *fakeNftConn) GetRules(_ *nftables.Table, chain *nftables.Chain) ([]*nftables.Rule, error) {
	return f.listedRules[chain.Name], nil
}

func TestNftBackendReplacesOnlyOwnedInetTableInOneFlush(t *testing.T) {
	owned := &nftables.Table{Family: nftables.TableFamilyINet, Name: OwnedTableName}
	foreign := &nftables.Table{Family: nftables.TableFamilyINet, Name: "foreign"}
	conn := &fakeNftConn{tables: []*nftables.Table{foreign, owned}}
	backend := newNftBackend(func() (nftConnection, error) { return conn, nil })
	rules := Ruleset{Family: TableFamilyINet, Table: OwnedTableName, Openings: []Opening{
		{Protocol: ProtocolTCP, Family: FamilyIPv4, Port: 34466},
		{Protocol: ProtocolUDP, Family: FamilyIPv6, Address: netip.MustParseAddr("2001:4860:1::1"), Port: 51000},
	}}
	if err := backend.Apply(context.Background(), rules); err != nil {
		t.Fatal(err)
	}
	if len(conn.deleted) != 1 || conn.deleted[0] != owned {
		t.Fatalf("deleted tables = %#v, want owned table only", conn.deleted)
	}
	if len(conn.added) != 1 || conn.added[0].Family != nftables.TableFamilyINet || conn.added[0].Name != OwnedTableName {
		t.Fatalf("added tables = %#v", conn.added)
	}
	if len(conn.chains) != 1 || conn.chains[0].Name != "input" || conn.chains[0].Hooknum == nil {
		t.Fatalf("chains = %#v", conn.chains)
	}
	if len(conn.rules) != 2 || conn.flushes != 1 {
		t.Fatalf("rules=%d flushes=%d", len(conn.rules), conn.flushes)
	}
	for _, rule := range conn.rules {
		verdict, ok := rule.Exprs[len(rule.Exprs)-1].(*expr.Verdict)
		if !ok || verdict.Kind != expr.VerdictAccept {
			t.Fatalf("rule does not end in accept verdict: %#v", rule.Exprs)
		}
	}
}

func TestNftBackendRejectsNonOwnedRulesetBeforeConnecting(t *testing.T) {
	called := false
	backend := newNftBackend(func() (nftConnection, error) {
		called = true
		return nil, errors.New("must not connect")
	})
	if err := backend.Apply(context.Background(), Ruleset{Family: TableFamilyINet, Table: "foreign"}); err == nil {
		t.Fatal("Apply() error = nil, want ownership boundary failure")
	}
	if called {
		t.Fatal("invalid ruleset reached nftables connection")
	}
}

func TestNftBackendEmitsPortRangeAndSinglePortComparisons(t *testing.T) {
	conn := &fakeNftConn{}
	backend := newNftBackend(func() (nftConnection, error) { return conn, nil })
	rules := Ruleset{Family: TableFamilyINet, Table: OwnedTableName, Openings: []Opening{
		{Protocol: ProtocolUDP, Family: FamilyIPv6, Address: netip.MustParseAddr("2001:4860:1::1"), Port: 51000},
		{Protocol: ProtocolUDP, Family: FamilyIPv6, Address: netip.MustParseAddr("2001:4860:1::2"), Port: 49152, PortEnd: 65535},
	}}
	if err := backend.Apply(context.Background(), rules); err != nil {
		t.Fatal(err)
	}
	if len(conn.rules) != 2 {
		t.Fatalf("rules = %d, want 2", len(conn.rules))
	}
	single := transportComparisons(conn.rules[0])
	if len(single) != 1 ||
		single[0].Op != expr.CmpOpEq ||
		string(single[0].Data) != string([]byte{0xC7, 0x38}) {
		t.Fatalf("single-port comparison = %#v, want one Eq on 51000", single)
	}
	ranged := transportComparisons(conn.rules[1])
	if len(ranged) != 2 ||
		ranged[0].Op != expr.CmpOpGte ||
		ranged[1].Op != expr.CmpOpLte {
		t.Fatalf("range comparisons = %#v, want Gte then Lte", ranged)
	}
	if string(ranged[0].Data) != string([]byte{0xC0, 0x00}) ||
		string(ranged[1].Data) != string([]byte{0xFF, 0xFF}) {
		t.Fatalf("range comparison data = %x / %x, want c000..ffff", ranged[0].Data, ranged[1].Data)
	}
}

func transportComparisons(rule *nftables.Rule) []*expr.Cmp {
	var result []*expr.Cmp
	seenTransportHeader := false
	for _, expression := range rule.Exprs {
		if payload, ok := expression.(*expr.Payload); ok && payload.Base == expr.PayloadBaseTransportHeader {
			seenTransportHeader = true
			continue
		}
		if !seenTransportHeader {
			continue
		}
		if cmp, ok := expression.(*expr.Cmp); ok {
			result = append(result, cmp)
		}
	}
	return result
}

func TestNftBackendDeleteIgnoresForeignTables(t *testing.T) {
	owned := &nftables.Table{Family: nftables.TableFamilyINet, Name: OwnedTableName}
	foreign := &nftables.Table{Family: nftables.TableFamilyINet, Name: "foreign"}
	conn := &fakeNftConn{tables: []*nftables.Table{foreign, owned}}
	backend := newNftBackend(func() (nftConnection, error) { return conn, nil })
	if err := backend.Delete(context.Background(), OwnedTableName); err != nil {
		t.Fatal(err)
	}
	if len(conn.deleted) != 1 || conn.deleted[0] != owned || conn.flushes != 1 {
		t.Fatalf("delete result: deleted=%#v flushes=%d", conn.deleted, conn.flushes)
	}
}

func TestNftBackendDiagnosesForeignInputDropWithoutMutation(t *testing.T) {
	drop := nftables.ChainPolicyDrop
	accept := nftables.ChainPolicyAccept
	input := nftables.ChainHookInput
	output := nftables.ChainHookOutput
	foreign := &nftables.Table{Family: nftables.TableFamilyINet, Name: "host_firewall"}
	owned := &nftables.Table{Family: nftables.TableFamilyINet, Name: OwnedTableName}
	foreignPolicyDrop := &nftables.Chain{Name: "input", Table: foreign, Hooknum: input, Policy: &drop}
	foreignRuleDrop := &nftables.Chain{Name: "guard", Table: foreign, Hooknum: input, Policy: &accept}
	ignoredOutput := &nftables.Chain{Name: "output", Table: foreign, Hooknum: output, Policy: &drop}
	ignoredOwned := &nftables.Chain{Name: "input", Table: owned, Hooknum: input, Policy: &drop}
	conn := &fakeNftConn{
		listedChains: []*nftables.Chain{foreignPolicyDrop, foreignRuleDrop, ignoredOutput, ignoredOwned},
		listedRules: map[string][]*nftables.Rule{
			"guard": {{Exprs: []expr.Any{&expr.Verdict{Kind: expr.VerdictDrop}}}},
		},
	}
	backend := newNftBackend(func() (nftConnection, error) { return conn, nil })
	diagnosis, err := backend.Diagnose(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !diagnosis.Degraded || len(diagnosis.Blockers) != 2 {
		t.Fatalf("Diagnose() = %#v, want two foreign blockers", diagnosis)
	}
	if len(conn.deleted)+len(conn.added)+len(conn.chains)+len(conn.rules)+conn.flushes != 0 {
		t.Fatalf("diagnosis mutated nftables connection: %#v", conn)
	}
}
