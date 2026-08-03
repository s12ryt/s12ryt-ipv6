package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/config"
	"github.com/s12ryt/s12ryt-ipv6/internal/dns64"
	"github.com/s12ryt/s12ryt-ipv6/internal/eventlog"
	"github.com/s12ryt/s12ryt-ipv6/internal/firewall"
	"github.com/s12ryt/s12ryt-ipv6/internal/stats"
)

type fakeOperationsService struct {
	overview          OperationsSnapshot
	statistics        stats.Snapshot
	logs              []eventlog.Event
	logFilter         eventlog.Filter
	logLimit          int
	clearedBy         string
	resetNode         string
	manualPrefix      netip.Prefix
	resolvers         []config.Resolver
	connectivity      []ConnectivityCheck
	operationErr      error
	connectivityCalls int
}

func (s *fakeOperationsService) Overview() OperationsSnapshot { return s.overview }
func (s *fakeOperationsService) Statistics() stats.Snapshot   { return s.statistics }
func (s *fakeOperationsService) TailLogs(filter eventlog.Filter, limit int) ([]eventlog.Event, error) {
	s.logFilter, s.logLimit = filter, limit
	return s.logs, s.operationErr
}
func (s *fakeOperationsService) ClearLogs(actor string) error {
	s.clearedBy = actor
	return s.operationErr
}
func (s *fakeOperationsService) ResetStatistics(node string) error {
	s.resetNode = node
	return s.operationErr
}
func (s *fakeOperationsService) SetManualNAT64(_ context.Context, prefix netip.Prefix) (dns64.NAT64Status, error) {
	s.manualPrefix = prefix
	return dns64.NAT64Status{State: dns64.NAT64Healthy, Prefix: prefix, Source: "manual", Manual: prefix.IsValid()}, s.operationErr
}
func (s *fakeOperationsService) UpdateResolvers(_ context.Context, resolvers []config.Resolver) error {
	s.resolvers = append([]config.Resolver(nil), resolvers...)
	return s.operationErr
}
func (s *fakeOperationsService) TestConnectivity(context.Context) ([]ConnectivityCheck, error) {
	s.connectivityCalls++
	return s.connectivity, s.operationErr
}

func TestHTTPServerOperationsQueriesAndMutations(t *testing.T) {
	service := &fakeOperationsService{
		overview: OperationsSnapshot{
			Health:    HealthDegraded,
			NAT64:     dns64.NAT64Status{State: dns64.NAT64Healthy, Prefix: netip.MustParsePrefix("64:ff9b::/96"), Source: "Cloudflare 1", LastChecked: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)},
			Firewall:  firewall.Diagnosis{Degraded: true, Blockers: []string{"inet/filter/input policy drop"}},
			Resolvers: []config.Resolver{{Name: "Cloudflare", Address: "2606:4700:4700::64", Port: 853, ServerName: "cloudflare-dns.com", Enabled: true}},
		},
		statistics:   stats.Snapshot{Nodes: map[string]stats.NodeCounters{"node-a": {TotalConnections: 3}}},
		logs:         []eventlog.Event{{Kind: eventlog.KindProxy, Action: "connect", Node: "node-a", Success: false}},
		connectivity: []ConnectivityCheck{{Name: "nat64", Kind: "nat64", Success: true, Address: "64:ff9b::c000:aa"}},
	}
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "correct-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetOperationsService(service); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginForMutation(t, server)

	overview := authenticatedRequest(t, server, http.MethodGet, "http://manager.example:34466/api/overview", cookie)
	if overview.Code != http.StatusOK || !strings.Contains(overview.Body.String(), `"prefix":"64:ff9b::/96"`) || !strings.Contains(overview.Body.String(), `"health":"degraded"`) {
		t.Fatalf("overview = %d %s", overview.Code, overview.Body.String())
	}
	statistics := authenticatedRequest(t, server, http.MethodGet, "http://manager.example:34466/api/stats", cookie)
	if statistics.Code != http.StatusOK || !strings.Contains(statistics.Body.String(), `"total_connections":3`) {
		t.Fatalf("stats = %d %s", statistics.Code, statistics.Body.String())
	}
	logs := authenticatedRequest(t, server, http.MethodGet, "http://manager.example:34466/api/logs?kind=proxy&node=node-a&success=false&limit=20", cookie)
	if logs.Code != http.StatusOK || service.logFilter.Kind != eventlog.KindProxy || service.logFilter.Node != "node-a" || service.logFilter.Success == nil || *service.logFilter.Success || service.logLimit != 20 {
		t.Fatalf("logs = %d %s, filter %#v limit %d", logs.Code, logs.Body.String(), service.logFilter, service.logLimit)
	}

	connectivity := httptest.NewRecorder()
	server.Handler().ServeHTTP(connectivity, mutationRequest(http.MethodPost, "/api/network/test", `{}`, cookie, csrf))
	if connectivity.Code != http.StatusOK || service.connectivityCalls != 1 || !strings.Contains(connectivity.Body.String(), `"name":"nat64"`) {
		t.Fatalf("connectivity = %d %s", connectivity.Code, connectivity.Body.String())
	}
	nat64Update := httptest.NewRecorder()
	server.Handler().ServeHTTP(nat64Update, mutationRequest(http.MethodPut, "/api/network/nat64", `{"prefix":"2001:db8:64::/96"}`, cookie, csrf))
	if nat64Update.Code != http.StatusOK || service.manualPrefix.String() != "2001:db8:64::/96" {
		t.Fatalf("NAT64 update = %d %s, prefix=%s", nat64Update.Code, nat64Update.Body.String(), service.manualPrefix)
	}
	resolverUpdate := httptest.NewRecorder()
	server.Handler().ServeHTTP(resolverUpdate, mutationRequest(http.MethodPut, "/api/network/resolvers", `{"resolvers":[{"name":"custom","address":"2001:4860:4860::6464","port":853,"server_name":"dns.google","enabled":true}]}`, cookie, csrf))
	if resolverUpdate.Code != http.StatusNoContent || len(service.resolvers) != 1 || service.resolvers[0].Name != "custom" {
		t.Fatalf("resolver update = %d %s, resolvers=%#v", resolverUpdate.Code, resolverUpdate.Body.String(), service.resolvers)
	}

	reset := httptest.NewRecorder()
	server.Handler().ServeHTTP(reset, mutationRequest(http.MethodPost, "/api/stats/reset", `{"node":"node-a","confirm":true}`, cookie, csrf))
	if reset.Code != http.StatusNoContent || service.resetNode != "node-a" {
		t.Fatalf("stats reset = %d, node=%q", reset.Code, service.resetNode)
	}
	clear := httptest.NewRecorder()
	server.Handler().ServeHTTP(clear, mutationRequest(http.MethodPost, "/api/logs/clear", `{"confirm":true}`, cookie, csrf))
	if clear.Code != http.StatusNoContent || service.clearedBy != "admin" {
		t.Fatalf("log clear = %d, actor=%q", clear.Code, service.clearedBy)
	}
}

func TestHTTPServerOperationsRejectInvalidInputsAndSanitizeErrors(t *testing.T) {
	service := &fakeOperationsService{}
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "correct-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetOperationsService(service); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginForMutation(t, server)

	requests := []*http.Request{
		mutationRequest(http.MethodPost, "/api/logs/clear", `{"confirm":false}`, cookie, csrf),
		mutationRequest(http.MethodPost, "/api/stats/reset", `{"confirm":false}`, cookie, csrf),
		mutationRequest(http.MethodPut, "/api/network/nat64", `{"prefix":"2001:db8::/64"}`, cookie, csrf),
		mutationRequest(http.MethodPut, "/api/network/resolvers", `{"resolvers":[{"name":"bad","address":"1.1.1.1","port":853,"server_name":"dns.example","enabled":true}]}`, cookie, csrf),
	}
	for _, request := range requests {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusUnprocessableEntity && response.Code != http.StatusBadRequest {
			t.Fatalf("%s response = %d %s", request.URL.Path, response.Code, response.Body.String())
		}
	}

	service.operationErr = errors.New("sensitive resolver detail")
	failed := httptest.NewRecorder()
	server.Handler().ServeHTTP(failed, mutationRequest(http.MethodPost, "/api/network/test", `{}`, cookie, csrf))
	if failed.Code != http.StatusInternalServerError || strings.Contains(failed.Body.String(), "sensitive") {
		t.Fatalf("operation error = %d %s", failed.Code, failed.Body.String())
	}
}

func authenticatedRequest(t *testing.T, server *HTTPServer, method, target string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}
