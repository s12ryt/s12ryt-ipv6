//go:build linux && integration

package network

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/vishvananda/netlink"
)

func TestLinuxKernelIntegrationAddressDADRouteFreebindAndShutdown(t *testing.T) {
	requireDisposableRootNetworkNamespace(t)
	name := fmt.Sprintf("s12r%x", time.Now().UnixNano()&0xffffff)
	attrs := netlink.NewLinkAttrs()
	attrs.Name = name
	link := &netlink.Dummy{LinkAttrs: attrs}
	if err := netlink.LinkAdd(link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = netlink.LinkDel(link) })
	if err := netlink.LinkSetUp(link); err != nil {
		t.Fatal(err)
	}

	kernel, err := NewLinuxKernel()
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewFileOwnershipStore(filepath.Join(t.TempDir(), "ownership.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewResourceManager(kernel, store, 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	addressTemplate, err := ipv6resource.NewPrefixTemplate("address", "2001:db8:1200::/120", name, ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	address := netip.MustParseAddr("2001:db8:1200::1")
	if err := manager.Apply(context.Background(), addressTemplate, []netip.Addr{address}); err != nil {
		t.Fatal(err)
	}

	routeTemplate, err := ipv6resource.NewPrefixTemplate("freebind", "2001:db8:1300::/120", name, ipv6resource.ModeLocalRouteFreebind)
	if err != nil {
		t.Fatal(err)
	}
	freebindAddress := netip.MustParseAddr("2001:db8:1300::1")
	if err := manager.Apply(context.Background(), routeTemplate, []netip.Addr{freebindAddress}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	owned, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(owned.Addresses) != 0 || len(owned.Routes) != 0 {
		t.Fatalf("ownership after shutdown = %#v", owned)
	}
}

func requireDisposableRootNetworkNamespace(t *testing.T) {
	t.Helper()
	if os.Getenv("S12RYT_INTEGRATION_NETNS") != "1" {
		t.Skip("set S12RYT_INTEGRATION_NETNS=1 inside a disposable Linux network namespace")
	}
	if os.Geteuid() != 0 {
		t.Skip("integration test requires root or equivalent capabilities")
	}
}
