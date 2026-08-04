package app

import (
	"context"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/node"
)

type secretTestNodeService struct {
	created node.Node
	updated node.Node
}

func (s *secretTestNodeService) Create(context.Context, node.Config, bool) (node.Node, error) {
	return s.created, nil
}
func (s *secretTestNodeService) CreateBatch(context.Context, []node.Config, bool) ([]node.Node, error) {
	return []node.Node{s.created}, nil
}
func (s *secretTestNodeService) Update(context.Context, string, node.Config, bool) (node.Node, error) {
	return s.updated, nil
}
func (s *secretTestNodeService) Start(context.Context, string) (node.Node, error) {
	return node.Node{}, nil
}
func (s *secretTestNodeService) Stop(context.Context, string) (node.Node, error) {
	return node.Node{}, nil
}
func (s *secretTestNodeService) Delete(context.Context, string) error { return nil }
func (s *secretTestNodeService) MoveToFolder(context.Context, string, string) (node.Node, error) {
	return node.Node{}, nil
}
func (s *secretTestNodeService) RenameFolder(context.Context, string, string) ([]node.Node, error) {
	return nil, nil
}
func (s *secretTestNodeService) Get(string) (node.Node, bool) { return node.Node{}, false }
func (s *secretTestNodeService) List() []node.Node            { return nil }

type secretTestRegistrar struct {
	values []string
}

func (r *secretTestRegistrar) RegisterSecret(value string) {
	r.values = append(r.values, value)
}

func TestSecretRegisteringNodeServiceRegistersCreatedAndUpdatedCredentials(t *testing.T) {
	delegate := &secretTestNodeService{
		created: node.Node{Config: node.Config{Username: "created-user", Password: "created-password"}},
		updated: node.Node{Config: node.Config{Username: "updated-user", Password: "updated-password"}},
	}
	registrar := &secretTestRegistrar{}
	service, err := newSecretRegisteringNodeService(delegate, registrar)
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
	if _, err := newSecretRegisteringNodeService(nil, &secretTestRegistrar{}); err == nil {
		t.Fatal("nil node service accepted")
	}
	if _, err := newSecretRegisteringNodeService(&secretTestNodeService{}, nil); err == nil {
		t.Fatal("nil secret registrar accepted")
	}
}
