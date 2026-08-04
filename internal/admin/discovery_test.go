package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/network"
)

type fakeNetworkDiscovery struct {
	snapshot network.NetworkDiscoverySnapshot
	err      error
}

func (f *fakeNetworkDiscovery) Discover(context.Context) (network.NetworkDiscoverySnapshot, error) {
	return f.snapshot, f.err
}

func TestNetworkCandidateServiceMarksExactAndOverlappingTemplates(t *testing.T) {
	exact, err := ipv6resource.NewPrefixTemplate("existing-exact", "2001:4860:10::/64", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	nested, err := ipv6resource.NewPrefixTemplate("existing-nested", "2001:4860:20::/64", "eth1", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	discovery := &fakeNetworkDiscovery{snapshot: network.NetworkDiscoverySnapshot{
		Interfaces: []network.DiscoveredInterface{{Name: "eth0", Index: 7}, {Name: "eth1", Index: 8}},
		Prefixes: []network.DiscoveredPrefix{
			{Interface: "eth0", Prefix: netip.MustParsePrefix("2001:4860:10::/64"), Sources: []network.PrefixSource{network.PrefixSourceAddress}},
			{Interface: "eth1", Prefix: netip.MustParsePrefix("2001:4860:20::/56"), Sources: []network.PrefixSource{network.PrefixSourceRoute}},
			{Interface: "eth1", Prefix: netip.MustParsePrefix("2001:4860:30::/64"), Sources: []network.PrefixSource{network.PrefixSourceRoute}},
		},
	}}
	service, err := NewNetworkCandidateService(discovery, func() ResourceSnapshot {
		return ResourceSnapshot{Templates: []ipv6resource.PrefixTemplate{nested, exact}}
	})
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Prefixes) != 3 {
		t.Fatalf("Prefixes = %#v", snapshot.Prefixes)
	}
	if snapshot.Prefixes[0].Available || !reflect.DeepEqual(snapshot.Prefixes[0].Conflicts, []PrefixConflict{{Template: "existing-exact", Reason: PrefixConflictExact}}) {
		t.Fatalf("exact candidate = %#v", snapshot.Prefixes[0])
	}
	if snapshot.Prefixes[1].Available || !reflect.DeepEqual(snapshot.Prefixes[1].Conflicts, []PrefixConflict{{Template: "existing-nested", Reason: PrefixConflictOverlap}}) {
		t.Fatalf("overlapping candidate = %#v", snapshot.Prefixes[1])
	}
	if !snapshot.Prefixes[2].Available || len(snapshot.Prefixes[2].Conflicts) != 0 {
		t.Fatalf("available candidate = %#v", snapshot.Prefixes[2])
	}

	snapshot.Interfaces[0].Name = "changed"
	snapshot.Prefixes[0].Sources[0] = "changed"
	again, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again.Interfaces[0].Name != "eth0" || again.Prefixes[0].Sources[0] != network.PrefixSourceAddress {
		t.Fatalf("Snapshot() returned aliased data: %#v", again)
	}
}

func TestNetworkCandidateServiceValidatesDependencies(t *testing.T) {
	if _, err := NewNetworkCandidateService(nil, func() ResourceSnapshot { return ResourceSnapshot{} }); err == nil {
		t.Fatal("NewNetworkCandidateService(nil, resources) error = nil")
	}
	if _, err := NewNetworkCandidateService(&fakeNetworkDiscovery{}, nil); err == nil {
		t.Fatal("NewNetworkCandidateService(discovery, nil) error = nil")
	}
}

func TestHTTPServerNetworkDiscoveryRequiresSessionAndSanitizesFailures(t *testing.T) {
	discovery := &fakeNetworkDiscovery{snapshot: network.NetworkDiscoverySnapshot{
		Interfaces: []network.DiscoveredInterface{{Name: "eth0", Index: 7}},
		Prefixes: []network.DiscoveredPrefix{{
			Interface: "eth0", Prefix: netip.MustParsePrefix("2001:4860:10::/64"),
			Sources: []network.PrefixSource{network.PrefixSourceAddress, network.PrefixSourceRoute},
		}},
	}}
	service, err := NewNetworkCandidateService(discovery, func() ResourceSnapshot { return ResourceSnapshot{} })
	if err != nil {
		t.Fatal(err)
	}
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "correct-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetNetworkCandidateService(service); err != nil {
		t.Fatal(err)
	}
	if err := server.SetNetworkCandidateService(service); err == nil {
		t.Fatal("second SetNetworkCandidateService() error = nil")
	}

	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "http://manager.example:34466/api/discovery/network", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	cookie, _ := loginForMutation(t, server)
	request := httptest.NewRequest(http.MethodGet, "http://manager.example:34466/api/discovery/network", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"eth0"`) || !strings.Contains(response.Body.String(), `"prefix":"2001:4860:10::/64"`) || !strings.Contains(response.Body.String(), `"sources":["address","route"]`) {
		t.Fatalf("discovery response = %d %s", response.Code, response.Body.String())
	}

	discovery.err = errors.New("secret netlink detail")
	failed := httptest.NewRecorder()
	server.Handler().ServeHTTP(failed, request.Clone(context.Background()))
	if failed.Code != http.StatusServiceUnavailable || strings.Contains(failed.Body.String(), "secret netlink detail") {
		t.Fatalf("failed discovery response = %d %s", failed.Code, failed.Body.String())
	}
}

func TestHTTPServerNetworkDiscoveryRejectsNilService(t *testing.T) {
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetNetworkCandidateService(nil); err == nil {
		t.Fatal("SetNetworkCandidateService(nil) error = nil")
	}
}
