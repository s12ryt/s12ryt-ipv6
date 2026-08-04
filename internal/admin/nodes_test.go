package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/node"
)

type fakeNodeService struct {
	nodes         map[string]node.Node
	lastConfig    node.Config
	lastConfigs   []node.Config
	lastConfirmed bool
	operationErr  error
	startErrors   map[string]error
	stopErrors    map[string]error
	deleteErrors  map[string]error
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

func (s *fakeNodeService) CreateBatch(_ context.Context, configs []node.Config, confirmed bool) ([]node.Node, error) {
	s.lastConfigs, s.lastConfirmed = append([]node.Config(nil), configs...), confirmed
	if s.operationErr != nil {
		return nil, s.operationErr
	}
	created := make([]node.Node, 0, len(configs))
	for _, config := range configs {
		current := node.Node{Config: config, Status: node.StatusRunning}
		s.nodes[config.ID] = current
		created = append(created, current)
	}
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
	if err := s.startErrors[id]; err != nil {
		return node.Node{}, err
	}
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
	if err := s.stopErrors[id]; err != nil {
		return node.Node{}, err
	}
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
	if err := s.deleteErrors[id]; err != nil {
		return err
	}
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

func (s *fakeNodeService) MoveToFolder(_ context.Context, id, folder string) (node.Node, error) {
	current, exists := s.nodes[id]
	if !exists {
		return node.Node{}, node.ErrNodeNotFound
	}
	current.Config.Folder = folder
	s.nodes[id] = current
	return current, nil
}

func (s *fakeNodeService) RenameFolder(_ context.Context, source, target string) ([]node.Node, error) {
	if s.operationErr != nil {
		return nil, s.operationErr
	}
	result := make([]node.Node, 0)
	for id, current := range s.nodes {
		if current.Config.Folder != source {
			continue
		}
		current.Config.Folder = target
		s.nodes[id] = current
		result = append(result, current)
	}
	if len(result) == 0 {
		return nil, node.ErrFolderNotFound
	}
	return result, nil
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

func TestHTTPServerCreatesNodeBatchWithIndependentCredentials(t *testing.T) {
	service := &fakeNodeService{nodes: make(map[string]node.Node)}
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "correct-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetNodeService(service); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginForMutation(t, server)
	body := `{
		"folder":"  批次 1  ","confirm_unauthenticated":false,
		"nodes":[
			{"id":"node-001","name":"節點 1","protocol":"mixed","authentication":"credentials","max_tcp":4096,"max_udp":1024,"dial_timeout":"10s","handshake_timeout":"30s","tunnel_idle_timeout":"0s","udp_idle_timeout":"5m","ula_override":"inherit","outbound":"shared-out","port":0,"inbound_mode":"ipv6","inbound_resource":"pool-in"},
			{"id":"node-002","name":"節點 2","protocol":"mixed","authentication":"credentials","max_tcp":4096,"max_udp":1024,"dial_timeout":"10s","handshake_timeout":"30s","tunnel_idle_timeout":"0s","udp_idle_timeout":"5m","ula_override":"inherit","outbound":"shared-out","port":0,"inbound_mode":"ipv6","inbound_resource":"pool-in"}
		]
	}`
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, mutationRequest(http.MethodPost, "/api/nodes/batch", body, cookie, csrf))
	if response.Code != http.StatusCreated {
		t.Fatalf("batch status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(service.lastConfigs) != 2 || service.lastConfigs[0].Folder != "批次 1" || service.lastConfigs[1].Folder != "批次 1" {
		t.Fatalf("batch configs = %#v", service.lastConfigs)
	}
	first, second := service.lastConfigs[0], service.lastConfigs[1]
	if first.Username == "" || first.Password == "" || second.Username == "" || second.Password == "" ||
		(first.Username == second.Username && first.Password == second.Password) {
		t.Fatalf("batch credentials were not independently generated: %#v", service.lastConfigs)
	}
	if service.lastConfirmed {
		t.Fatal("authenticated batch unexpectedly confirmed public proxy risk")
	}
}

func TestHTTPServerRejectsInvalidNodeBatchBeforeCallingService(t *testing.T) {
	service := &fakeNodeService{nodes: make(map[string]node.Node)}
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "correct-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetNodeService(service); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginForMutation(t, server)
	for name, body := range map[string]string{
		"empty":          `{"folder":"批次 1","nodes":[]}`,
		"missing folder": `{"nodes":[{}]}`,
		"explicit credentials": `{"folder":"批次 1","nodes":[
			{"id":"node-1","name":"one","protocol":"mixed","authentication":"credentials","username":"manual","password":"manual-password-value","max_tcp":1,"max_udp":1,"dial_timeout":"1s","handshake_timeout":"1s","tunnel_idle_timeout":"0s","udp_idle_timeout":"1s","outbound":"fixed","inbound_mode":"ipv4"}
		]}`,
		"mixed settings": `{"folder":"批次 1","nodes":[
			{"id":"node-1","name":"one","protocol":"http","authentication":"none","max_tcp":1,"max_udp":1,"dial_timeout":"1s","handshake_timeout":"1s","tunnel_idle_timeout":"0s","udp_idle_timeout":"1s","outbound":"fixed","inbound_mode":"ipv4"},
			{"id":"node-2","name":"two","protocol":"socks","authentication":"none","max_tcp":1,"max_udp":1,"dial_timeout":"1s","handshake_timeout":"1s","tunnel_idle_timeout":"0s","udp_idle_timeout":"1s","outbound":"fixed","inbound_mode":"ipv4"}
		],"confirm_unauthenticated":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, mutationRequest(http.MethodPost, "/api/nodes/batch", body, cookie, csrf))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
	if len(service.lastConfigs) != 0 {
		t.Fatalf("service received invalid batch = %#v", service.lastConfigs)
	}
}

func TestHTTPServerMovesAndRenamesNodeFolders(t *testing.T) {
	config := validAdminNodeConfig("node-1", "one")
	config.Folder = "來源"
	service := &fakeNodeService{nodes: map[string]node.Node{"node-1": {Config: config, Status: node.StatusRunning}}}
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "correct-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetNodeService(service); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginForMutation(t, server)

	move := httptest.NewRecorder()
	server.Handler().ServeHTTP(move, mutationRequest(http.MethodPut, "/api/nodes/node-1/folder", `{"folder":"目標"}`, cookie, csrf))
	if move.Code != http.StatusOK || service.nodes["node-1"].Config.Folder != "目標" {
		t.Fatalf("move response = %d %s", move.Code, move.Body.String())
	}
	rename := httptest.NewRecorder()
	server.Handler().ServeHTTP(rename, mutationRequest(http.MethodPut, "/api/node-folders/rename", `{"source":"目標","target":"新名稱"}`, cookie, csrf))
	if rename.Code != http.StatusOK || service.nodes["node-1"].Config.Folder != "新名稱" {
		t.Fatalf("rename response = %d %s", rename.Code, rename.Body.String())
	}
}

func TestHTTPServerFolderActionsAttemptEveryNodeAndReportFailures(t *testing.T) {
	first := validAdminNodeConfig("node-1", "one")
	first.Folder = "批次 1"
	second := validAdminNodeConfig("node-2", "two")
	second.Folder = "批次 1"
	service := &fakeNodeService{
		nodes: map[string]node.Node{
			"node-1": {Config: first, Status: node.StatusStopped},
			"node-2": {Config: second, Status: node.StatusStopped},
		},
		startErrors: map[string]error{"node-2": errors.New("secret runtime detail")},
	}
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "correct-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetNodeService(service); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := loginForMutation(t, server)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, mutationRequest(http.MethodPost, "/api/node-folders/action", `{"folder":"批次 1","action":"start"}`, cookie, csrf))
	if response.Code != http.StatusMultiStatus || strings.Contains(response.Body.String(), "secret runtime detail") ||
		!strings.Contains(response.Body.String(), `"succeeded":["node-1"]`) || !strings.Contains(response.Body.String(), `"id":"node-2"`) {
		t.Fatalf("folder action response = %d %s", response.Code, response.Body.String())
	}
	if service.nodes["node-1"].Status != node.StatusRunning {
		t.Fatalf("successful node status = %q", service.nodes["node-1"].Status)
	}
}

func validAdminNodeConfig(id, name string) node.Config {
	return node.Config{
		ID: id, Name: name, Protocol: node.ProtocolMixed,
		Username: "proxy-user", Password: "proxy-password-value",
		MaxTCP: 4096, MaxUDP: 1024,
		DialTimeout: 10 * time.Second, HandshakeTimeout: 30 * time.Second,
		UDPIdleTimeout: 5 * time.Minute, ULAOverride: "inherit", Outbound: "shared-out",
		InboundMode: node.InboundIPv6, InboundResource: "pool-in",
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
