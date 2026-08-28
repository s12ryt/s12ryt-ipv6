package app

import (
	"context"
	"errors"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/node"
)

type secretTestNodeService struct {
	created       node.Node
	updated       node.Node
	existing      node.Node
	existingFound bool
	deleteErr     error
	deletedID     string
	// current tracks the node state after Create/Update so Get behaves like
	// the real service while tests drive a full lifecycle.
	current    node.Node
	hasCurrent bool
}

func (s *secretTestNodeService) Create(context.Context, node.Config, bool) (node.Node, error) {
	s.current, s.hasCurrent = s.created, true
	return s.created, nil
}
func (s *secretTestNodeService) CreateBatch(context.Context, []node.Config, bool) ([]node.Node, error) {
	s.current, s.hasCurrent = s.created, true
	return []node.Node{s.created}, nil
}
func (s *secretTestNodeService) Update(context.Context, string, node.Config, bool) (node.Node, error) {
	s.current, s.hasCurrent = s.updated, true
	return s.updated, nil
}
func (s *secretTestNodeService) Start(context.Context, string) (node.Node, error) {
	return node.Node{}, nil
}
func (s *secretTestNodeService) Stop(context.Context, string) (node.Node, error) {
	return node.Node{}, nil
}
func (s *secretTestNodeService) Delete(_ context.Context, id string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deletedID = id
	return nil
}
func (s *secretTestNodeService) MoveToFolder(context.Context, string, string) (node.Node, error) {
	return node.Node{}, nil
}
func (s *secretTestNodeService) RenameFolder(context.Context, string, string) ([]node.Node, error) {
	return nil, nil
}
func (s *secretTestNodeService) Get(string) (node.Node, bool) {
	if s.hasCurrent {
		return s.current, true
	}
	return s.existing, s.existingFound
}
func (s *secretTestNodeService) List() []node.Node { return nil }

type secretTestRegistrar struct {
	values       []string
	unregistered []string
}

func (r *secretTestRegistrar) RegisterSecret(value string) {
	r.values = append(r.values, value)
}

func (r *secretTestRegistrar) UnregisterSecret(value string) {
	r.unregistered = append(r.unregistered, value)
}

type registerOnlyTestRegistrar struct {
	values []string
}

func (r *registerOnlyTestRegistrar) RegisterSecret(value string) {
	r.values = append(r.values, value)
}

type statsTestRemover struct {
	removed []string
}

func (r *statsTestRemover) RemoveNode(nodeID string) {
	r.removed = append(r.removed, nodeID)
}

func TestSecretRegisteringNodeServiceRegistersCreatedAndUpdatedCredentials(t *testing.T) {
	delegate := &secretTestNodeService{
		created: node.Node{Config: node.Config{Username: "created-user", Password: "created-password"}},
		updated: node.Node{Config: node.Config{Username: "updated-user", Password: "updated-password"}},
	}
	registrar := &secretTestRegistrar{}
	service, err := newSecretRegisteringNodeService(delegate, registrar, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(context.Background(), node.Config{}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(context.Background(), "node", node.Config{}, false); err != nil {
		t.Fatal(err)
	}
	want := []string{"created-user", "created-password", "updated-user", "updated-password"}
	if len(registrar.values) != len(want) {
		t.Fatalf("registered values = %v", registrar.values)
	}
	for index := range want {
		if registrar.values[index] != want[index] {
			t.Fatalf("registered values = %v, want %v", registrar.values, want)
		}
	}
}

func TestSecretRegisteringNodeServiceValidatesDependencies(t *testing.T) {
	if _, err := newSecretRegisteringNodeService(nil, &secretTestRegistrar{}, nil); err == nil {
		t.Fatal("nil node service accepted")
	}
	if _, err := newSecretRegisteringNodeService(&secretTestNodeService{}, nil, nil); err == nil {
		t.Fatal("nil secret registrar accepted")
	}
}

func TestSecretRegisteringNodeServiceDeleteCleansUpSecretsAndStats(t *testing.T) {
	delegate := &secretTestNodeService{
		existing:      node.Node{Config: node.Config{ID: "node-1", Username: "user-a", Password: "pass-a"}},
		existingFound: true,
	}
	registrar := &secretTestRegistrar{}
	remover := &statsTestRemover{}
	service, err := newSecretRegisteringNodeService(delegate, registrar, remover)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Delete(context.Background(), "node-1"); err != nil {
		t.Fatal(err)
	}

	wantUnregistered := []string{"user-a", "pass-a"}
	if len(registrar.unregistered) != len(wantUnregistered) {
		t.Fatalf("unregistered values = %v", registrar.unregistered)
	}
	for index := range wantUnregistered {
		if registrar.unregistered[index] != wantUnregistered[index] {
			t.Fatalf("unregistered values = %v, want %v", registrar.unregistered, wantUnregistered)
		}
	}
	if len(remover.removed) != 1 || remover.removed[0] != "node-1" {
		t.Fatalf("removed stats nodes = %v", remover.removed)
	}
}

func TestSecretRegisteringNodeServiceDeleteFailureSkipsCleanup(t *testing.T) {
	delegate := &secretTestNodeService{
		existing:      node.Node{Config: node.Config{ID: "node-1", Username: "user-a", Password: "pass-a"}},
		existingFound: true,
		deleteErr:     errors.New("delegate failure"),
	}
	registrar := &secretTestRegistrar{}
	remover := &statsTestRemover{}
	service, err := newSecretRegisteringNodeService(delegate, registrar, remover)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Delete(context.Background(), "node-1"); err == nil {
		t.Fatal("Delete() error = nil, want delegate failure")
	}

	if len(registrar.unregistered) != 0 {
		t.Fatalf("secrets unregistered despite failed delete: %v", registrar.unregistered)
	}
	if len(remover.removed) != 0 {
		t.Fatalf("stats removed despite failed delete: %v", remover.removed)
	}
}

func TestSecretRegisteringNodeServiceDeleteToleratesRegisterOnlyRegistrar(t *testing.T) {
	delegate := &secretTestNodeService{
		existing:      node.Node{Config: node.Config{ID: "node-2", Username: "user-b", Password: "pass-b"}},
		existingFound: true,
	}
	registrar := &registerOnlyTestRegistrar{}
	remover := &statsTestRemover{}
	service, err := newSecretRegisteringNodeService(delegate, registrar, remover)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.Delete(context.Background(), "node-2"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if len(remover.removed) != 1 || remover.removed[0] != "node-2" {
		t.Fatalf("removed stats nodes = %v", remover.removed)
	}
}

// countingRegistrar tracks the per-secret reference count the way the eventlog
// registry does, so tests can assert a full node lifecycle leaves no residue.
type countingRegistrar struct {
	counts map[string]int
}

func (r *countingRegistrar) RegisterSecret(value string) {
	r.counts[value]++
}

func (r *countingRegistrar) UnregisterSecret(value string) {
	r.counts[value]--
}

func assertSecretCountsDrained(t *testing.T, registrar *countingRegistrar) {
	t.Helper()
	for value, count := range registrar.counts {
		if count != 0 {
			t.Fatalf("secret %q reference count = %d after full lifecycle, want 0 (all counts: %v)", value, count, registrar.counts)
		}
	}
}

func TestSecretRegisteringNodeServiceUpdateReleasesRotatedCredentials(t *testing.T) {
	delegate := &secretTestNodeService{
		created: node.Node{Config: node.Config{Username: "user-a", Password: "pass-a"}},
		updated: node.Node{Config: node.Config{Username: "user-b", Password: "pass-b"}},
	}
	registrar := &countingRegistrar{counts: map[string]int{}}
	service, err := newSecretRegisteringNodeService(delegate, registrar, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Create(context.Background(), node.Config{}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(context.Background(), "node-1", node.Config{}, false); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(context.Background(), "node-1"); err != nil {
		t.Fatal(err)
	}

	assertSecretCountsDrained(t, registrar)
}

func TestSecretRegisteringNodeServiceUnchangedUpdateKeepsReferenceCountBalanced(t *testing.T) {
	delegate := &secretTestNodeService{
		created: node.Node{Config: node.Config{Username: "user-a", Password: "pass-a"}},
		updated: node.Node{Config: node.Config{Username: "user-a", Password: "pass-a"}},
	}
	registrar := &countingRegistrar{counts: map[string]int{}}
	service, err := newSecretRegisteringNodeService(delegate, registrar, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Create(context.Background(), node.Config{}, false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := service.Update(context.Background(), "node-1", node.Config{}, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Delete(context.Background(), "node-1"); err != nil {
		t.Fatal(err)
	}

	assertSecretCountsDrained(t, registrar)
}
