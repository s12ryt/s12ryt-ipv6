package app

import (
	"context"
	"errors"
	"sync"

	"github.com/s12ryt/s12ryt-ipv6/internal/node"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type RuntimeFirewall interface {
	node.NodeFirewall
	proxy.UDPRelayFirewall
}

type DeferredRuntimeFirewall struct {
	mu     sync.RWMutex
	target RuntimeFirewall
}

func NewDeferredRuntimeFirewall() *DeferredRuntimeFirewall {
	return &DeferredRuntimeFirewall{}
}

func (f *DeferredRuntimeFirewall) Set(target RuntimeFirewall) error {
	if target == nil {
		return errors.New("runtime firewall is required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.target != nil {
		return errors.New("runtime firewall is already configured")
	}
	f.target = target
	return nil
}

func (f *DeferredRuntimeFirewall) OpenNode(ctx context.Context, id string, endpoints []proxy.BindEndpoint) error {
	target, err := f.configured()
	if err != nil {
		return err
	}
	return target.OpenNode(ctx, id, endpoints)
}

func (f *DeferredRuntimeFirewall) CloseNode(ctx context.Context, id string, endpoints []proxy.BindEndpoint) error {
	target, err := f.configured()
	if err != nil {
		return err
	}
	return target.CloseNode(ctx, id, endpoints)
}

func (f *DeferredRuntimeFirewall) Open(ctx context.Context, endpoint proxy.BindEndpoint) error {
	target, err := f.configured()
	if err != nil {
		return err
	}
	return target.Open(ctx, endpoint)
}

func (f *DeferredRuntimeFirewall) Close(ctx context.Context, endpoint proxy.BindEndpoint) error {
	target, err := f.configured()
	if err != nil {
		return err
	}
	return target.Close(ctx, endpoint)
}

func (f *DeferredRuntimeFirewall) configured() (RuntimeFirewall, error) {
	f.mu.RLock()
	target := f.target
	f.mu.RUnlock()
	if target == nil {
		return nil, errors.New("runtime firewall is not configured")
	}
	return target, nil
}

var _ node.NodeFirewall = (*DeferredRuntimeFirewall)(nil)
var _ proxy.UDPRelayFirewall = (*DeferredRuntimeFirewall)(nil)
