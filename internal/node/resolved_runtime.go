package node

import (
	"context"
	"errors"
	"fmt"
)

type InboundConfigResolver interface {
	Resolve(Config) (Config, error)
}

type ResolvedRuntimeFactory struct {
	resolver InboundConfigResolver
	factory  RuntimeFactory
}

func NewResolvedRuntimeFactory(resolver InboundConfigResolver, factory RuntimeFactory) (*ResolvedRuntimeFactory, error) {
	if resolver == nil {
		return nil, errors.New("inbound config resolver is required")
	}
	if factory == nil {
		return nil, errors.New("node runtime factory is required")
	}
	return &ResolvedRuntimeFactory{resolver: resolver, factory: factory}, nil
}

func (f *ResolvedRuntimeFactory) Start(ctx context.Context, config Config) (Runtime, error) {
	resolved, err := f.resolve(config)
	if err != nil {
		return nil, err
	}
	return f.factory.Start(ctx, resolved)
}

func (f *ResolvedRuntimeFactory) Replace(ctx context.Context, current Runtime, config Config) (Runtime, error) {
	resolved, err := f.resolve(config)
	if err != nil {
		return nil, err
	}
	if replacer, ok := f.factory.(RuntimeReplacementFactory); ok {
		return replacer.Replace(ctx, current, resolved)
	}
	return f.factory.Start(ctx, resolved)
}

func (f *ResolvedRuntimeFactory) resolve(config Config) (Config, error) {
	resolved, err := f.resolver.Resolve(config)
	if err != nil {
		return Config{}, fmt.Errorf("resolve node inbound: %w", err)
	}
	if err := resolved.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate resolved node config: %w", err)
	}
	return resolved, nil
}

var (
	_ RuntimeFactory            = (*ResolvedRuntimeFactory)(nil)
	_ RuntimeReplacementFactory = (*ResolvedRuntimeFactory)(nil)
)
