package node

import (
	"context"
	"errors"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type recordingInboundResolver struct {
	resolved Config
	err      error
	calls    []Config
}

func (r *recordingInboundResolver) Resolve(config Config) (Config, error) {
	r.calls = append(r.calls, cloneConfig(config))
	if r.err != nil {
		return Config{}, r.err
	}
	return cloneConfig(r.resolved), nil
}

type recordingReplacementFactory struct {
	started  []Config
	replaced []Config
	runtime  Runtime
}

func (f *recordingReplacementFactory) Start(_ context.Context, config Config) (Runtime, error) {
	f.started = append(f.started, cloneConfig(config))
	return f.runtime, nil
}

func (f *recordingReplacementFactory) Replace(_ context.Context, _ Runtime, config Config) (Runtime, error) {
	f.replaced = append(f.replaced, cloneConfig(config))
	return f.runtime, nil
}

func TestResolvedRuntimeFactoryKeepsDeclarationOutsideRuntime(t *testing.T) {
	declaration := validConfig("node-a", "Node A")
	declaration.Inbound = nil
	declaration.InboundMode = InboundIPv6
	declaration.InboundResource = "pool-in"
	resolved := cloneConfig(declaration)
	resolved.Inbound = []proxy.BindSpec{{Protocol: proxy.BindTCP, Family: proxy.BindIPv6}}
	operations := []string{}
	baseRuntime := &fakeRuntime{name: "Node A", log: &operations}
	resolver := &recordingInboundResolver{resolved: resolved}
	base := &recordingReplacementFactory{runtime: baseRuntime}

	factory, err := NewResolvedRuntimeFactory(resolver, base)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := factory.Start(context.Background(), declaration)
	if err != nil {
		t.Fatal(err)
	}
	if runtime != baseRuntime || len(base.started) != 1 || len(base.started[0].Inbound) != 1 {
		t.Fatalf("runtime did not receive resolved config: %#v", base.started)
	}
	if len(declaration.Inbound) != 0 || declaration.InboundResource != "pool-in" {
		t.Fatalf("declaration was mutated: %#v", declaration)
	}

	resolved.Inbound = append(resolved.Inbound, proxy.BindSpec{Protocol: proxy.BindTCP, Family: proxy.BindIPv4})
	resolver.resolved = resolved
	replacement, err := factory.Replace(context.Background(), runtime, declaration)
	if err != nil {
		t.Fatal(err)
	}
	if replacement != baseRuntime || len(base.replaced) != 1 || len(base.replaced[0].Inbound) != 2 {
		t.Fatalf("replacement did not receive refreshed config: %#v", base.replaced)
	}
}

func TestResolvedRuntimeFactoryRejectsResolutionBeforeStartingRuntime(t *testing.T) {
	resolver := &recordingInboundResolver{err: errors.New("missing inbound resource")}
	operations := []string{}
	base := &recordingReplacementFactory{runtime: &fakeRuntime{name: "Node A", log: &operations}}
	factory, err := NewResolvedRuntimeFactory(resolver, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factory.Start(context.Background(), validConfig("node-a", "Node A")); err == nil {
		t.Fatal("expected resolution error")
	}
	if len(base.started) != 0 {
		t.Fatal("runtime started before inbound resolution succeeded")
	}
}

func TestResolvedRuntimeFactoryValidatesDependencies(t *testing.T) {
	operations := []string{}
	base := &recordingReplacementFactory{runtime: &fakeRuntime{name: "Node A", log: &operations}}
	resolver := &recordingInboundResolver{resolved: validConfig("node-a", "Node A")}
	if _, err := NewResolvedRuntimeFactory(nil, base); err == nil {
		t.Fatal("expected nil resolver error")
	}
	if _, err := NewResolvedRuntimeFactory(resolver, nil); err == nil {
		t.Fatal("expected nil runtime factory error")
	}
}
