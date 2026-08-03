package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-ipv6/internal/node"
)

type fakeNodeService struct {
	nodes         map[string]node.Node
	lastConfig    node.Config
	lastConfirmed bool
	operationErr  error
}

func (s *fakeNodeService) Create(_ context.Context, config node.Config, confirmed bool) (node.Node, error) {
	s.lastConfig, s.lastConfirmed = config, confirmed
	if s.operationErr != nil {
		return node.Node{}, s.operationErr
	}
	created := node.Node{Config: config, Status: node.StatusRunning}
	s.nodes[config.ID] = created
	return created, nil
}

func (s *fakeNodeService) Update(_ context.Context, id string, config node.Config, confirmed bool) (node.Node, error) {
	s.lastConfig, s.lastConfirmed = config, confirmed
	if s.operationErr != nil {
		return node.Node{}, s.operationErr
	}
	updated := node.Node{Config: config, Status: node.StatusRunning}
	s.nodes[id] = updated
	return updated, nil
}

func (s *fakeNodeService) Start(_ context.Context, id string) (node.Node, error) {
	if s.operationErr != nil {
		return node.Node{}, s.operationErr
	}
	current, exists := s.nodes[id]
	if !exists {
		return node.Node{}, node.ErrNodeNotFound
	}
	current.Status = node.StatusRunning
	s.nodes[id] = current
	return current, nil
}

func (s *fakeNodeService) Stop(_ context.Context, id string) (node.Node, error) {
	if s.operationErr != nil {
		return node.Node{}, s.operationErr
	}
	current, exists := s.nodes[id]
	if !exists {
		return node.Node{}, node.ErrNodeNotFound
	}
	current.Status = node.StatusStopped
	s.nodes[id] = current
	return current, nil
}

func (s *fakeNodeService) Delete(_ context.Context, id string) error {
	if s.operationErr != nil {
		return s.operationErr
	}
	if _, exists := s.nodes[id]; !exists {
		return node.ErrNodeNotFound
	}
	delete(s.nodes, id)
	return nil
}

func (s *fakeNodeService) Get(id string) (node.Node, bool) {
	value, exists := s.nodes[id]
	return value, exists
}

func (s *fakeNodeService) List() []node.Node {
	result := make([]node.Node, 0, len(s.nodes))
	for _, value := range s.nodes {
		result = append(result, value)
	}
	return result
}

func loginForMutation(t *testing.T, server *HTTPServer) (*http.Cookie, string) {
	t.Helper()
	response := performLogin(t, server.Handler(), "192.0.2.1:1234", "manager.example:34466", "correct-password-value")
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d", response.Code)
	}
	var payload struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return response.Result().Cookies()[0], payload.CSRF
}

func mutationRequest(method, path, body string, cookie *http.Cookie, csrf string) *http.Request {
	request := httptest.NewRequest(method, "http://manager.example:34466"+path, strings.NewReader(body))
	request.Host = "manager.example:34466"
	request.RemoteAddr = "192.0.2.1:1234"
	request.Header.Set("Origin", "http://manager.example:34466")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(CSRFHeaderName, csrf)
	request.AddCookie(cookie)
	return request
}

func TestHTTPServerNodeCRUDAndActions(t *testing.T) {
	service := &fakeNodeService{nodes: make(map[string]node.Node)}
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "correct-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetNodeService(service); err != nil {
		t.Fatalf("SetNodeService() error = %v", err)
	}
	cookie, csrf := loginForMutation(t, server)
	createBody := `{
		"id":"node-1","name":"主要節點","protocol":"mixed",
		"username":"proxy-user","password":"proxy-password-value",
		"max_tcp":4096,"max_udp":1024,"dial_timeout":"10s","handshake_timeout":"30s",
		"tunnel_idle_timeout":"0s","udp_idle_timeout":"5m","ula_override":"inherit",
		"outbound":"shared-out","dedicated_pool":"","port":55000,
		"inbound_mode":"ipv6","inbound_resource":"fixed-in",
		"confirm_unauthenticated":false
	}`
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, mutationRequest(http.MethodPost, "/api/nodes", createBody, cookie, csrf))
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.lastConfig.ID != "node-1" || service.lastConfig.InboundMode != node.InboundIPv6 || service.lastConfig.InboundResource != "fixed-in" || len(service.lastConfig.Inbound) != 0 {
		t.Fatalf("created config = %#v", service.lastConfig)
	}
	if service.lastConfirmed {
		t.Fatal("authenticated create unexpectedly confirmed unauthenticated risk")
	}

	listRequest := httptest.NewRequest(http.MethodGet, "http://manager.example:34466/api/nodes", nil)
	listRequest.AddCookie(cookie)
	listResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), "proxy-password-value") {
		t.Fatalf("list response = %d %s", listResponse.Code, listResponse.Body.String())
	}

	stopResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(stopResponse, mutationRequest(http.MethodPost, "/api/nodes/node-1/stop", `{}`, cookie, csrf))
	if stopResponse.Code != http.StatusOK || !strings.Contains(stopResponse.Body.String(), `"status":"stopped"`) {
		t.Fatalf("stop response = %d %s", stopResponse.Code, stopResponse.Body.String())
	}
	startResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(startResponse, mutationRequest(http.MethodPost, "/api/nodes/node-1/start", `{}`, cookie, csrf))
	if startResponse.Code != http.StatusOK || !strings.Contains(startResponse.Body.String(), `"status":"running"`) {
		t.Fatalf("start response = %d %s", startResponse.Code, startResponse.Body.String())
	}

	deleteResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteResponse, mutationRequest(http.MethodDelete, "/api/nodes/node-1", `{}`, cookie, csrf))
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("delete response = %d %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if _, exists := service.nodes["node-1"]; exists {
		t.Fatal("node remained after delete")
	}
}

func TestHTTPServerNodeAPIValidatesInputAndMapsDomainErrors(t *testing.T) {
	service := &fakeNodeService{nodes: make(map[string]node.Node)}
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "correct-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetNodeService(service); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginForMutation(t, server)

	invalid := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalid, mutationRequest(http.MethodPost, "/api/nodes", `{"id":"node-1","unknown":true}`, cookie, csrf))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status = %d, want 400", invalid.Code)
	}
	service.operationErr = node.ErrUnauthenticatedRiskConfirmation
	risk := httptest.NewRecorder()
	server.Handler().ServeHTTP(risk, mutationRequest(http.MethodPost, "/api/nodes", `{
		"id":"node-1","name":"open","protocol":"http","max_tcp":1,"max_udp":1,
		"dial_timeout":"1s","handshake_timeout":"1s","tunnel_idle_timeout":"0s","udp_idle_timeout":"1s",
		"outbound":"fixed","inbound_mode":"ipv4"
	}`, cookie, csrf))
	if risk.Code != http.StatusUnprocessableEntity {
		t.Fatalf("risk confirmation status = %d, want 422", risk.Code)
	}
	service.operationErr = node.ErrNodeExists
	conflict := httptest.NewRecorder()
	server.Handler().ServeHTTP(conflict, mutationRequest(http.MethodPost, "/api/nodes", `{
		"id":"node-1","name":"node","protocol":"http","username":"user","password":"password-value",
		"max_tcp":1,"max_udp":1,"dial_timeout":"1s","handshake_timeout":"1s","tunnel_idle_timeout":"0s",
		"udp_idle_timeout":"1s","outbound":"fixed","inbound_mode":"ipv4"
	}`, cookie, csrf))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("duplicate node status = %d, want 409", conflict.Code)
	}
	service.operationErr = errors.New("runtime exploded")
	failed := httptest.NewRecorder()
	server.Handler().ServeHTTP(failed, mutationRequest(http.MethodPost, "/api/nodes/node-1/start", `{}`, cookie, csrf))
	if failed.Code != http.StatusInternalServerError || strings.Contains(failed.Body.String(), "runtime exploded") {
		t.Fatalf("internal error response = %d %s", failed.Code, failed.Body.String())
	}
}

func TestHTTPServerNodeCredentialsModeGeneratesMissingCredentials(t *testing.T) {
	service := &fakeNodeService{nodes: make(map[string]node.Node)}
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "correct-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetNodeService(service); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginForMutation(t, server)
	body := `{
		"id":"generated","name":"generated","protocol":"mixed","authentication":"credentials",
		"max_tcp":4096,"max_udp":1024,"dial_timeout":"10s","handshake_timeout":"30s",
		"tunnel_idle_timeout":"0s","udp_idle_timeout":"5m","ula_override":"inherit",
		"outbound":"shared-out","port":0,
		"inbound_mode":"ipv6","inbound_resource":"pool-in"
	}`
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, mutationRequest(http.MethodPost, "/api/nodes", body, cookie, csrf))
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.lastConfig.Username == "" || service.lastConfig.Password == "" {
		t.Fatalf("generated credentials = %q/%q", service.lastConfig.Username, service.lastConfig.Password)
	}
	if service.lastConfirmed {
		t.Fatal("credential generation unexpectedly confirmed unauthenticated risk")
	}
	var result struct {
		Authentication string `json:"authentication"`
		Username       string `json:"username"`
		Password       string `json:"password"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Authentication != "credentials" || result.Username == "" || result.Password == "" {
		t.Fatalf("response credentials = %#v", result)
	}
}

func TestHTTPServerNodeNoneAuthenticationRequiresExplicitEmptyCredentials(t *testing.T) {
	service := &fakeNodeService{nodes: make(map[string]node.Node)}
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "correct-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetNodeService(service); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginForMutation(t, server)
	body := `{
		"id":"open","name":"open","protocol":"http","authentication":"none",
		"username":"must-not-remain","password":"password-value","confirm_unauthenticated":true,
		"max_tcp":1,"max_udp":1,"dial_timeout":"1s","handshake_timeout":"1s",
		"tunnel_idle_timeout":"0s","udp_idle_timeout":"1s","ula_override":"inherit",
		"outbound":"fixed","inbound_mode":"ipv4"
	}`
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, mutationRequest(http.MethodPost, "/api/nodes", body, cookie, csrf))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.lastConfig.ID != "" {
		t.Fatal("invalid authentication mode reached node service")
	}
}

func TestHTTPServerNodeAPIRejectsConcreteInboundBindings(t *testing.T) {
	service := &fakeNodeService{nodes: make(map[string]node.Node)}
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "correct-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetNodeService(service); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginForMutation(t, server)
	body := `{
		"id":"manual","name":"manual","protocol":"http","authentication":"credentials",
		"username":"proxy-user","password":"proxy-password-value","max_tcp":1,"max_udp":1,
		"dial_timeout":"1s","handshake_timeout":"1s","tunnel_idle_timeout":"0s","udp_idle_timeout":"1s",
		"outbound":"fixed","inbound_mode":"ipv6","inbound_resource":"fixed-in",
		"inbound":[{"protocol":"tcp","family":"ipv6","address":"2001:4860::1"}]
	}`
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, mutationRequest(http.MethodPost, "/api/nodes", body, cookie, csrf))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("manual inbound status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.lastConfig.ID != "" {
		t.Fatal("manual inbound reached node service")
	}
}
