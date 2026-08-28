package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/admin"
	"github.com/s12ryt/s12ryt-ipv6/internal/dns64"
	"github.com/s12ryt/s12ryt-ipv6/internal/firewall"
	"github.com/s12ryt/s12ryt-ipv6/internal/network"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type productionTestFileInfo struct {
	mode fs.FileMode
}

func (i productionTestFileInfo) Name() string       { return "control.sock" }
func (i productionTestFileInfo) Size() int64        { return 0 }
func (i productionTestFileInfo) Mode() fs.FileMode  { return i.mode }
func (i productionTestFileInfo) ModTime() time.Time { return time.Time{} }
func (i productionTestFileInfo) IsDir() bool        { return false }
func (i productionTestFileInfo) Sys() any           { return nil }

type productionTestKernel struct{}

func (productionTestKernel) AddressExists(context.Context, network.AddressRef) (bool, error) {
	return false, nil
}
func (productionTestKernel) AddAddress(context.Context, network.AddressRef) error       { return nil }
func (productionTestKernel) RemoveAddress(context.Context, network.AddressRef) error    { return nil }
func (productionTestKernel) WaitAddressReady(context.Context, network.AddressRef) error { return nil }
func (productionTestKernel) InterfaceAddresses(context.Context, string) ([]netip.Addr, error) {
	return nil, nil
}
func (productionTestKernel) WaitAddressesReady(context.Context, []network.AddressRef) error {
	return nil
}
func (productionTestKernel) LocalRouteExists(context.Context, network.RouteRef) (bool, error) {
	return false, nil
}
func (productionTestKernel) AddLocalRoute(context.Context, network.RouteRef) error    { return nil }
func (productionTestKernel) RemoveLocalRoute(context.Context, network.RouteRef) error { return nil }
func (productionTestKernel) ValidateBindable(context.Context, network.AddressRef, bool) error {
	return nil
}

type productionTestFirewallBackend struct{}

func (productionTestFirewallBackend) Apply(context.Context, firewall.Ruleset) error { return nil }
func (productionTestFirewallBackend) Delete(context.Context, string) error          { return nil }
func (productionTestFirewallBackend) Diagnose(context.Context) (firewall.Diagnosis, error) {
	return firewall.Diagnosis{}, nil
}

type productionTestBinder struct{}

func (productionTestBinder) Bind(context.Context, proxy.BindEndpoint) (io.Closer, error) {
	return nil, io.ErrClosedPipe
}

type productionTestQueryer struct{}

func (productionTestQueryer) Query(context.Context, dns64.Endpoint, string, dns64.RecordType) (dns64.QueryResult, error) {
	return dns64.QueryResult{}, dns64.ErrNAT64Unavailable
}

type productionTestDiscovery struct{}

func (productionTestDiscovery) Discover(context.Context) (network.NetworkDiscoverySnapshot, error) {
	return network.NetworkDiscoverySnapshot{}, nil
}

type productionTestControlListener struct {
	connections chan net.Conn
	closed      chan struct{}
	once        sync.Once
}

func newProductionTestControlListener() *productionTestControlListener {
	return &productionTestControlListener{connections: make(chan net.Conn), closed: make(chan struct{})}
}

func (l *productionTestControlListener) Accept() (net.Conn, error) {
	select {
	case connection := <-l.connections:
		return connection, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *productionTestControlListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *productionTestControlListener) Addr() net.Addr { return productionTestAddr("control") }

type productionTestAddr string

func (a productionTestAddr) Network() string { return "unix" }
func (a productionTestAddr) String() string  { return string(a) }

func productionTestPlatform(frontend fs.FS) productionPlatform {
	return productionPlatform{
		newKernel:          func() (network.Kernel, error) { return productionTestKernel{}, nil },
		newFirewallBackend: func() (firewall.Backend, error) { return productionTestFirewallBackend{}, nil },
		binder:             productionTestBinder{},
		managementListen: func(context.Context, string, string) (net.Listener, error) {
			return nil, io.ErrClosedPipe
		},
		controlListen: func(string) (net.Listener, error) { return nil, io.ErrClosedPipe },
		newQueryer:    func(time.Duration) (dns64.Queryer, error) { return productionTestQueryer{}, nil },
		discovery:     productionTestDiscovery{},
		frontend:      frontend,
		hostAddresses: func() ([]netip.Addr, error) { return []netip.Addr{netip.MustParseAddr("2001:db8::1")}, nil },
		connector:     func(string, bool) proxy.Connector { return proxy.NewSystemConnector("", false) },
	}
}

func TestBuildProductionCreatesCompleteServiceAndDurableBootstrapFiles(t *testing.T) {
	directory := t.TempDir()
	frontend := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<!doctype html><title>s12ryt</title>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('app')")},
	}
	platform := productionTestPlatform(frontend)

	service, err := buildProduction(ProductionOptions{DataDirectory: directory, Stdout: io.Discard}, platform)
	if err != nil {
		t.Fatalf("buildProduction() error = %v", err)
	}
	if service == nil {
		t.Fatal("buildProduction() service = nil")
	}
	built, ok := service.(*productionService)
	if !ok {
		t.Fatalf("buildProduction() type = %T, want *productionService", service)
	}
	t.Cleanup(func() { _ = built.close() })
	discoveryResponse := httptest.NewRecorder()
	built.handler.ServeHTTP(discoveryResponse, httptest.NewRequest(http.MethodGet, "http://manager.example:34466/api/discovery/network", nil))
	if discoveryResponse.Code != http.StatusUnauthorized {
		t.Fatalf("network discovery without session status = %d", discoveryResponse.Code)
	}

	paths, err := NewDataPaths(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paths.Configuration, paths.MasterKey, paths.EventLog} {
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() {
			t.Fatalf("bootstrap file %q stat = %v, info = %#v", path, statErr, info)
		}
	}
}

func TestBuildProductionConnectsAgentServiceToControlSocket(t *testing.T) {
	directory := t.TempDir()
	listener := newProductionTestControlListener()
	frontend := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>s12ryt</title>")}}
	platform := productionTestPlatform(frontend)
	platform.controlListen = func(string) (net.Listener, error) { return listener, nil }

	service, err := buildProduction(ProductionOptions{DataDirectory: directory, Stdout: io.Discard}, platform)
	if err != nil {
		t.Fatalf("buildProduction() error = %v", err)
	}
	built := service.(*productionService)
	t.Cleanup(func() { _ = built.close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serveDone := make(chan error, 1)
	go func() { serveDone <- built.service.options.ServeControl(ctx) }()

	client, err := admin.NewControlClient(admin.ControlClientOptions{
		Timeout: time.Second,
		Dial: func(context.Context, string) (net.Conn, error) {
			server, client := net.Pipe()
			select {
			case listener.connections <- server:
				return client, nil
			case <-ctx.Done():
				_ = server.Close()
				_ = client.Close()
				return nil, ctx.Err()
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.CallAgent(ctx, "control.sock", json.RawMessage(`{"command":"status"}`))
	if err != nil {
		t.Fatalf("CallAgent(status) error = %v", err)
	}
	var response struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(result, &response); err != nil || !response.OK {
		t.Fatalf("status response = %s, error = %v", result, err)
	}
	cancel()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("ServeControl() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ServeControl() did not stop after cancellation")
	}
}

func TestBuildProductionKeepsManagementAvailableWithCorruptResourceAndNodeState(t *testing.T) {
	directory := t.TempDir()
	paths, err := NewDataPaths(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Resources, []byte("schema_version: 1\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Nodes, []byte("schema_version: 1\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	frontend := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>s12ryt</title>")},
	}

	service, err := buildProduction(
		ProductionOptions{DataDirectory: directory, Stdout: io.Discard},
		productionTestPlatform(frontend),
	)
	if err != nil {
		t.Fatalf("buildProduction() error = %v", err)
	}
	built := service.(*productionService)
	t.Cleanup(func() { _ = built.close() })

	resourceContent, err := os.ReadFile(paths.Resources)
	if err != nil {
		t.Fatal(err)
	}
	if string(resourceContent) != "schema_version: 1\nunknown: true\n" {
		t.Fatalf("corrupt resource state was overwritten: %q", resourceContent)
	}
	nodeContent, err := os.ReadFile(paths.Nodes)
	if err != nil {
		t.Fatal(err)
	}
	if string(nodeContent) != "schema_version: 1\nunknown: true\n" {
		t.Fatalf("corrupt node state was overwritten: %q", nodeContent)
	}
}

func TestPrepareControlSocketOnlyRemovesStaleSocket(t *testing.T) {
	t.Run("missing path needs no removal", func(t *testing.T) {
		removed := false
		err := prepareControlSocketWith("control.sock", func(string) (os.FileInfo, error) {
			return nil, fs.ErrNotExist
		}, func(string) error {
			removed = true
			return nil
		})
		if err != nil || removed {
			t.Fatalf("prepareControlSocketWith() = %v, removed = %v", err, removed)
		}
	})

	t.Run("stale socket is removed", func(t *testing.T) {
		removedPath := ""
		err := prepareControlSocketWith("control.sock", func(string) (os.FileInfo, error) {
			return productionTestFileInfo{mode: os.ModeSocket | 0o600}, nil
		}, func(path string) error {
			removedPath = path
			return nil
		})
		if err != nil || removedPath != "control.sock" {
			t.Fatalf("prepareControlSocketWith() = %v, removed = %q", err, removedPath)
		}
	})

	t.Run("regular file is never removed", func(t *testing.T) {
		removed := false
		err := prepareControlSocketWith("control.sock", func(string) (os.FileInfo, error) {
			return productionTestFileInfo{mode: 0o600}, nil
		}, func(string) error {
			removed = true
			return nil
		})
		if err == nil || removed {
			t.Fatalf("prepareControlSocketWith() = %v, removed = %v", err, removed)
		}
	})

	t.Run("inspection and removal errors are preserved", func(t *testing.T) {
		inspectErr := errors.New("inspect failed")
		if err := prepareControlSocketWith("control.sock", func(string) (os.FileInfo, error) {
			return nil, inspectErr
		}, os.Remove); !errors.Is(err, inspectErr) {
			t.Fatalf("inspection error = %v", err)
		}
		removeErr := errors.New("remove failed")
		if err := prepareControlSocketWith("control.sock", func(string) (os.FileInfo, error) {
			return productionTestFileInfo{mode: os.ModeSocket}, nil
		}, func(string) error { return removeErr }); !errors.Is(err, removeErr) {
			t.Fatalf("removal error = %v", err)
		}
	})
}

func TestPreparedControlListenerPreparesBeforeListening(t *testing.T) {
	order := make([]string, 0, 2)
	_, err := listenPreparedControlSocket("control.sock", func(path string) error {
		order = append(order, "prepare:"+path)
		return nil
	}, func(path string) (net.Listener, error) {
		order = append(order, "listen:"+path)
		return nil, io.ErrClosedPipe
	})
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("listenPreparedControlSocket() error = %v", err)
	}
	want := []string{"prepare:control.sock", "listen:control.sock"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("order = %v, want %v", order, want)
	}

	prepareErr := errors.New("unsafe path")
	called := false
	_, err = listenPreparedControlSocket("control.sock", func(string) error { return prepareErr }, func(string) (net.Listener, error) {
		called = true
		return nil, nil
	})
	if !errors.Is(err, prepareErr) || called {
		t.Fatalf("prepare failure = %v, listen called = %v", err, called)
	}
}
