package app

import (
	"context"
	"errors"

	"github.com/s12ryt/s12ryt-ipv6/internal/admin"
	"github.com/s12ryt/s12ryt-ipv6/internal/node"
)

type secretRegistrar interface {
	RegisterSecret(string)
}

type secretRegisteringNodeService struct {
	delegate  admin.NodeService
	registrar secretRegistrar
}

func newSecretRegisteringNodeService(delegate admin.NodeService, registrar secretRegistrar) (*secretRegisteringNodeService, error) {
	if delegate == nil || registrar == nil {
		return nil, errors.New("node service and secret registrar are required")
	}
	return &secretRegisteringNodeService{delegate: delegate, registrar: registrar}, nil
}

func (s *secretRegisteringNodeService) Create(ctx context.Context, config node.Config, confirm bool) (node.Node, error) {
	created, err := s.delegate.Create(ctx, config, confirm)
	if err == nil {
		s.register(created)
	}
	return created, err
}

func (s *secretRegisteringNodeService) CreateBatch(ctx context.Context, configs []node.Config, confirm bool) ([]node.Node, error) {
	created, err := s.delegate.CreateBatch(ctx, configs, confirm)
	if err == nil {
		for _, current := range created {
			s.register(current)
		}
	}
	return created, err
}

func (s *secretRegisteringNodeService) Update(ctx context.Context, id string, config node.Config, confirm bool) (node.Node, error) {
	updated, err := s.delegate.Update(ctx, id, config, confirm)
	if err == nil || errors.Is(err, node.ErrPreviousRuntimeCleanup) {
		s.register(updated)
	}
	return updated, err
}

func (s *secretRegisteringNodeService) Start(ctx context.Context, id string) (node.Node, error) {
	return s.delegate.Start(ctx, id)
}

func (s *secretRegisteringNodeService) Stop(ctx context.Context, id string) (node.Node, error) {
	return s.delegate.Stop(ctx, id)
}

func (s *secretRegisteringNodeService) Delete(ctx context.Context, id string) error {
	return s.delegate.Delete(ctx, id)
}

func (s *secretRegisteringNodeService) MoveToFolder(ctx context.Context, id, folder string) (node.Node, error) {
	return s.delegate.MoveToFolder(ctx, id, folder)
}

func (s *secretRegisteringNodeService) RenameFolder(ctx context.Context, source, target string) ([]node.Node, error) {
	return s.delegate.RenameFolder(ctx, source, target)
}

func (s *secretRegisteringNodeService) Get(id string) (node.Node, bool) {
	return s.delegate.Get(id)
}

func (s *secretRegisteringNodeService) List() []node.Node {
	return s.delegate.List()
}

func (s *secretRegisteringNodeService) register(current node.Node) {
	if current.Config.Username != "" {
		s.registrar.RegisterSecret(current.Config.Username)
	}
	if current.Config.Password != "" {
		s.registrar.RegisterSecret(current.Config.Password)
	}
}

var _ admin.NodeService = (*secretRegisteringNodeService)(nil)
