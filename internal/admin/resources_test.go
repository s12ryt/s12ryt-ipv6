package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
)

type fakeResourceService struct {
	snapshot      ResourceSnapshot
	template      ipv6resource.PrefixTemplate
	fixedName     string
	fixedTemplate string
	fixedAddress  *netip.Addr
	poolName      string
	poolKind      ipv6resource.PoolKind
	poolTemplate  string
	poolCapacity  int
	pinned        []string
	drainPool     string
	drainBatch    string
	operationErr  error
}

func (s *fakeResourceService) Snapshot() ResourceSnapshot { return s.snapshot }

func (s *fakeResourceService) CreateTemplate(_ context.Context, template ipv6resource.PrefixTemplate) error {
	s.template = template
	return s.operationErr
}

func (s *fakeResourceService) DeleteTemplate(_ context.Context, name string) error {
	s.template.Name = name
	return s.operationErr
}

func (s *fakeResourceService) CreateFixedAddress(_ context.Context, name, template string, address *netip.Addr) (ipv6resource.FixedAddress, error) {
	s.fixedName, s.fixedTemplate, s.fixedAddress = name, template, address
	if s.operationErr != nil {
		return ipv6resource.FixedAddress{}, s.operationErr
	}
	value := netip.MustParseAddr("2001:4860:1::20")
	if address != nil {
		value = *address
	}
	return ipv6resource.FixedAddress{Name: name, Template: template, Address: value, Ownership: ipv6resource.OwnershipAddress}, nil
}

func (s *fakeResourceService) DeleteFixedAddress(_ context.Context, name string) error {
	s.fixedName = name
	return s.operationErr
}

func (s *fakeResourceService) CreatePool(_ context.Context, name string, kind ipv6resource.PoolKind, template string, capacity int, pinned []string) (*ipv6resource.Pool, error) {
	s.poolName, s.poolKind, s.poolTemplate, s.poolCapacity, s.pinned = name, kind, template, capacity, append([]string(nil), pinned...)
	if s.operationErr != nil {
		return nil, s.operationErr
	}
	return &ipv6resource.Pool{Name: name, Kind: kind, Template: template, Capacity: capacity}, nil
}

func (s *fakeResourceService) DeletePool(_ context.Context, name string) error {
	s.poolName = name
	return s.operationErr
}

func (s *fakeResourceService) RefreshPool(_ context.Context, name string) (*ipv6resource.Pool, error) {
	s.poolName = name
	if s.operationErr != nil {
		return nil, s.operationErr
	}
	return &ipv6resource.Pool{Name: name, Kind: ipv6resource.PoolSharedOutbound, Template: "edge", Capacity: 2, Active: []netip.Addr{netip.MustParseAddr("2001:4860:1::30")}}, nil
}

func (s *fakeResourceService) ForceDrain(_ context.Context, pool, batch string) error {
	s.drainPool, s.drainBatch = pool, batch
	return s.operationErr
}

func TestHTTPServerResourceSnapshotAndMutations(t *testing.T) {
	template, err := ipv6resource.NewPrefixTemplate("edge", "2001:4860:1::/120", "eth0", ipv6resource.ModeAddress)
	if err != nil {
		t.Fatal(err)
	}
	service := &fakeResourceService{snapshot: ResourceSnapshot{
		Templates: []ipv6resource.PrefixTemplate{template},
		Fixed:     []ipv6resource.FixedAddress{{Name: "fixed", Template: "edge", Address: netip.MustParseAddr("2001:4860:1::10"), Ownership: ipv6resource.OwnershipAddress}},
		Addresses: []ipv6resource.CanonicalAddress{{Address: netip.MustParseAddr("2001:4860:1::10"), Template: "edge", Ownership: ipv6resource.OwnershipAddress, References: 1}},
		Pools:     []*ipv6resource.Pool{{Name: "shared", Kind: ipv6resource.PoolSharedOutbound, Template: "edge", Capacity: 1, Active: []netip.Addr{netip.MustParseAddr("2001:4860:1::10")}}},
	}}
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "correct-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetResourceService(service); err != nil {
		t.Fatalf("SetResourceService() error = %v", err)
	}
	cookie, csrf := loginForMutation(t, server)

	get := httptest.NewRequest(http.MethodGet, "http://manager.example:34466/api/resources", nil)
	get.AddCookie(cookie)
	getResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"prefix":"2001:4860:1::/120"`) || !strings.Contains(getResponse.Body.String(), `"address":"2001:4860:1::10"`) {
		t.Fatalf("resource snapshot = %d %s", getResponse.Code, getResponse.Body.String())
	}

	createTemplate := httptest.NewRecorder()
	server.Handler().ServeHTTP(createTemplate, mutationRequest(http.MethodPost, "/api/resources/templates", `{"name":"new","prefix":"2001:4860:2::/64","interface":"eth1","mode":"external"}`, cookie, csrf))
	if createTemplate.Code != http.StatusCreated || service.template.Name != "new" || service.template.Prefix.String() != "2001:4860:2::/64" {
		t.Fatalf("create template = %d, captured %#v", createTemplate.Code, service.template)
	}

	createFixed := httptest.NewRecorder()
	server.Handler().ServeHTTP(createFixed, mutationRequest(http.MethodPost, "/api/resources/fixed", `{"name":"auto","template":"edge"}`, cookie, csrf))
	if createFixed.Code != http.StatusCreated || service.fixedAddress != nil {
		t.Fatalf("create auto fixed = %d, address %#v", createFixed.Code, service.fixedAddress)
	}

	createPool := httptest.NewRecorder()
	server.Handler().ServeHTTP(createPool, mutationRequest(http.MethodPost, "/api/resources/pools", `{"name":"pool","kind":"shared-outbound","template":"edge","capacity":2,"pinned":["fixed"]}`, cookie, csrf))
	if createPool.Code != http.StatusCreated || service.poolCapacity != 2 || len(service.pinned) != 1 {
		t.Fatalf("create pool = %d, captured %#v", createPool.Code, service)
	}

	refresh := httptest.NewRecorder()
	server.Handler().ServeHTTP(refresh, mutationRequest(http.MethodPost, "/api/resources/pools/pool/refresh", `{}`, cookie, csrf))
	if refresh.Code != http.StatusOK || service.poolName != "pool" || !strings.Contains(refresh.Body.String(), "2001:4860:1::30") {
		t.Fatalf("refresh pool = %d %s", refresh.Code, refresh.Body.String())
	}

	force := httptest.NewRecorder()
	server.Handler().ServeHTTP(force, mutationRequest(http.MethodPost, "/api/resources/pools/pool/drains/drain-1/force", `{"confirm":true}`, cookie, csrf))
	if force.Code != http.StatusNoContent || service.drainPool != "pool" || service.drainBatch != "drain-1" {
		t.Fatalf("force drain = %d, captured %s/%s", force.Code, service.drainPool, service.drainBatch)
	}
}

func TestHTTPServerResourceAPIRequiresForceConfirmationAndSanitizesErrors(t *testing.T) {
	service := &fakeResourceService{}
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "correct-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetResourceService(service); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginForMutation(t, server)
	missingConfirm := httptest.NewRecorder()
	server.Handler().ServeHTTP(missingConfirm, mutationRequest(http.MethodPost, "/api/resources/pools/pool/drains/drain-1/force", `{"confirm":false}`, cookie, csrf))
	if missingConfirm.Code != http.StatusUnprocessableEntity || service.drainBatch != "" {
		t.Fatalf("missing confirmation = %d, drain called=%q", missingConfirm.Code, service.drainBatch)
	}

	service.operationErr = errors.New("sensitive kernel detail")
	failed := httptest.NewRecorder()
	server.Handler().ServeHTTP(failed, mutationRequest(http.MethodPost, "/api/resources/templates", `{"name":"new","prefix":"2001:4860:2::/64","interface":"eth1","mode":"external"}`, cookie, csrf))
	if failed.Code != http.StatusBadRequest || strings.Contains(failed.Body.String(), "sensitive kernel detail") {
		t.Fatalf("resource error response = %d %s", failed.Code, failed.Body.String())
	}
}
