package firewall

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

type fakeBackend struct {
	applied     []Ruleset
	applyErr    error
	deletes     int
	deleteErr   error
	diagnosis   Diagnosis
	diagnoseErr error
}

func (f *fakeBackend) Apply(_ context.Context, rules Ruleset) error {
	if f.applyErr != nil {
		return f.applyErr
	}
	f.applied = append(f.applied, cloneRuleset(rules))
	return nil
}

func (f *fakeBackend) Delete(context.Context, string) error {
	f.deletes++
	return f.deleteErr
}

func (f *fakeBackend) Diagnose(context.Context) (Diagnosis, error) {
	return f.diagnosis, f.diagnoseErr
}

func TestManagerAtomicallyReplacesCompleteOwnedRuleset(t *testing.T) {
	backend := &fakeBackend{}
	manager, err := NewManager(backend)
	if err != nil {
		t.Fatal(err)
	}
	first := []Opening{
		{Protocol: ProtocolTCP, Family: FamilyIPv4, Port: 34466, Purpose: "management"},
		{Protocol: ProtocolTCP, Family: FamilyIPv6, Port: 34466, Purpose: "management"},
	}
	if err := manager.Replace(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := append(first, Opening{
		Protocol: ProtocolUDP,
		Family:   FamilyIPv6,
		Address:  netip.MustParseAddr("2001:4860:1::10"),
		Port:     51000,
		Purpose:  "udp-relay",
	})
	if err := manager.Replace(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if len(backend.applied) != 2 {
		t.Fatalf("Apply calls = %d, want 2", len(backend.applied))
	}
	got := backend.applied[1]
	if got.Family != TableFamilyINet || got.Table != OwnedTableName || len(got.Openings) != 3 {
		t.Fatalf("second ruleset = %#v", got)
	}
	second[2].Port = 1
	if got.Openings[2].Port != 51000 {
		t.Fatal("backend ruleset aliases caller-owned slice")
	}
}

func TestManagerKeepsCommittedStateWhenBackendRejectsUpdate(t *testing.T) {
	backend := &fakeBackend{}
	manager, err := NewManager(backend)
	if err != nil {
		t.Fatal(err)
	}
	initial := []Opening{{Protocol: ProtocolTCP, Family: FamilyIPv6, Port: 34466, Purpose: "management"}}
	if err := manager.Replace(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	backend.applyErr = errors.New("nft transaction failed")
	if err := manager.Replace(context.Background(), []Opening{{Protocol: ProtocolUDP, Family: FamilyIPv6, Port: 51000, Purpose: "relay"}}); err == nil {
		t.Fatal("Replace() error = nil, want backend failure")
	}
	state := manager.State()
	if len(state.Openings) != 1 || state.Openings[0] != initial[0] {
		t.Fatalf("State() = %#v, want prior committed rules", state)
	}
}

func TestManagerValidatesAndDeduplicatesOpenings(t *testing.T) {
	backend := &fakeBackend{}
	manager, err := NewManager(backend)
	if err != nil {
		t.Fatal(err)
	}
	opening := Opening{Protocol: ProtocolTCP, Family: FamilyIPv6, Address: netip.MustParseAddr("2001:4860:1::1"), Port: 1080, Purpose: "node"}
	if err := manager.Replace(context.Background(), []Opening{opening, opening}); err != nil {
		t.Fatal(err)
	}
	if got := len(manager.State().Openings); got != 1 {
		t.Fatalf("deduplicated openings = %d, want 1", got)
	}
	bad := []Opening{
		{Protocol: "icmp", Family: FamilyIPv6, Port: 1},
		{Protocol: ProtocolTCP, Family: "inet", Port: 1},
		{Protocol: ProtocolTCP, Family: FamilyIPv4, Address: netip.MustParseAddr("2001:4860::1"), Port: 1},
		{Protocol: ProtocolTCP, Family: FamilyIPv6, Port: 0},
	}
	for _, candidate := range bad {
		if err := manager.Replace(context.Background(), []Opening{candidate}); err == nil {
			t.Fatalf("Replace(%#v) error = nil, want validation failure", candidate)
		}
	}
	if len(backend.applied) != 1 {
		t.Fatalf("invalid rules reached backend: calls=%d", len(backend.applied))
	}
}

func TestManagerShutdownDeletesOnlyOwnedTableAndClearsStateAfterSuccess(t *testing.T) {
	backend := &fakeBackend{}
	manager, err := NewManager(backend)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Replace(context.Background(), []Opening{{Protocol: ProtocolTCP, Family: FamilyIPv6, Port: 34466}}); err != nil {
		t.Fatal(err)
	}
	backend.deleteErr = errors.New("delete failed")
	if err := manager.Shutdown(context.Background()); err == nil {
		t.Fatal("Shutdown() error = nil, want delete failure")
	}
	if len(manager.State().Openings) != 1 {
		t.Fatal("failed shutdown cleared committed state")
	}
	backend.deleteErr = nil
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if backend.deletes != 2 || len(manager.State().Openings) != 0 {
		t.Fatalf("shutdown state: deletes=%d state=%#v", backend.deletes, manager.State())
	}
}
