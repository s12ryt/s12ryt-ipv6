package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/auth"
)

type fakePasswordAuthenticator struct {
	password string
	err      error
	calls    int
}

func (a *fakePasswordAuthenticator) Authenticate(password string) (bool, error) {
	a.calls++
	if a.err != nil {
		return false, a.err
	}
	return password == a.password, nil
}

func (a *fakePasswordAuthenticator) Change(current, replacement string) error {
	if a.err != nil {
		return a.err
	}
	if current != a.password {
		return ErrInvalidCurrentPassword
	}
	a.password = replacement
	return nil
}

func newTestHTTPServer(t *testing.T, authenticator PasswordService, perLimit, globalLimit int, health func() HealthState) *HTTPServer {
	t.Helper()
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	entropy := make([]byte, 1024)
	for index := range entropy {
		entropy[index] = byte(index)
	}
	sessions := auth.NewSessionManager(func() time.Time { return now }, bytes.NewReader(entropy), 30*time.Minute, 12*time.Hour)
	limiter := auth.NewLoginLimiter(func() time.Time { return now }, perLimit, globalLimit, 15*time.Minute)
	events, err := NewEventHub(8)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewHTTPServer(HTTPServerOptions{
		Passwords:    authenticator,
		Sessions:     sessions,
		Limiter:      limiter,
		Health:       health,
		Events:       events,
		SSEHeartbeat: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}
	return server
}

func performLogin(t *testing.T, handler http.Handler, remote, host, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"password": password})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/session", bytes.NewReader(body))
	request.Host = host
	request.RemoteAddr = remote
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestHTTPServerLoginCreatesStrictSingleSession(t *testing.T) {
	authenticator := &fakePasswordAuthenticator{password: "correct-password-value"}
	server := newTestHTTPServer(t, authenticator, 5, 500, func() HealthState { return HealthHealthy })

	first := performLogin(t, server.Handler(), "[2001:db8:1:2::1]:1234", "manager.example:34466", authenticator.password)
	if first.Code != http.StatusOK {
		t.Fatalf("first login status = %d, body = %s", first.Code, first.Body.String())
	}
	if got := first.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	cookies := first.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != SessionCookieName || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure || cookie.Path != "/" {
		t.Fatalf("session cookie = %#v", cookie)
	}
	var payload struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &payload); err != nil || payload.CSRFToken == "" {
		t.Fatalf("login payload = %#v, error = %v", payload, err)
	}

	current := httptest.NewRequest(http.MethodGet, "http://manager.example:34466/api/session", nil)
	current.Host = "manager.example:34466"
	current.RemoteAddr = "[2001:db8:1:2::1]:1234"
	current.AddCookie(cookie)
	currentResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(currentResponse, current)
	if currentResponse.Code != http.StatusOK {
		t.Fatalf("authenticated current session status = %d", currentResponse.Code)
	}
	var currentPayload struct {
		Authenticated bool   `json:"authenticated"`
		CSRFToken     string `json:"csrf_token"`
	}
	if err := json.Unmarshal(currentResponse.Body.Bytes(), &currentPayload); err != nil {
		t.Fatal(err)
	}
	if !currentPayload.Authenticated || currentPayload.CSRFToken == "" || currentPayload.CSRFToken == payload.CSRFToken {
		t.Fatalf("current session payload = %#v", currentPayload)
	}
	if err := server.sessions.ValidateCSRF(cookie.Value, payload.CSRFToken); err == nil {
		t.Fatal("login CSRF token remained valid after current session refresh")
	}
	if err := server.sessions.ValidateCSRF(cookie.Value, currentPayload.CSRFToken); err != nil {
		t.Fatalf("current session CSRF token rejected: %v", err)
	}

	second := performLogin(t, server.Handler(), "[2001:db8:1:3::1]:1234", "manager.example:34466", authenticator.password)
	if second.Code != http.StatusOK {
		t.Fatalf("second login status = %d", second.Code)
	}
	oldResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(oldResponse, current)
	if oldResponse.Code != http.StatusUnauthorized {
		t.Fatalf("old session after second login status = %d, want 401", oldResponse.Code)
	}
}

func TestHTTPServerLoginUsesSocketSourceAndRateLimitsFailures(t *testing.T) {
	authenticator := &fakePasswordAuthenticator{password: "correct-password-value"}
	server := newTestHTTPServer(t, authenticator, 2, 500, func() HealthState { return HealthHealthy })
	for attempt := range 2 {
		response := performLogin(t, server.Handler(), "[2001:db8:1:2::1]:1234", "manager.example:34466", "wrong-password-value")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", attempt+1, response.Code)
		}
		if strings.Contains(response.Body.String(), "wrong-password-value") {
			t.Fatal("login response leaked submitted password")
		}
	}

	body := `{"password":"correct-password-value"}`
	request := httptest.NewRequest(http.MethodPost, "http://manager.example:34466/api/session", strings.NewReader(body))
	request.Host = "manager.example:34466"
	request.RemoteAddr = "[2001:db8:1:2::ffff]:5678"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-For", "2001:db8:ffff::1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("same /64 login status = %d, want 429", response.Code)
	}
	if authenticator.calls != 2 {
		t.Fatalf("authenticator calls = %d, want 2", authenticator.calls)
	}
}

func TestHTTPServerRejectsUnsafeLoginBodies(t *testing.T) {
	authenticator := &fakePasswordAuthenticator{password: "correct-password-value"}
	server := newTestHTTPServer(t, authenticator, 5, 500, func() HealthState { return HealthHealthy })
	tests := []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
	}{
		{name: "not JSON", contentType: "text/plain", body: `{}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "unknown field", contentType: "application/json", body: `{"password":"correct-password-value","admin":true}`, wantStatus: http.StatusBadRequest},
		{name: "trailing JSON", contentType: "application/json", body: `{"password":"correct-password-value"}{}`, wantStatus: http.StatusBadRequest},
		{name: "empty password", contentType: "application/json", body: `{"password":""}`, wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://manager.example:34466/api/session", strings.NewReader(test.body))
			request.Host = "manager.example:34466"
			request.RemoteAddr = "192.0.2.1:1234"
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestHTTPServerMutationGuardRequiresSessionOriginJSONAndCSRF(t *testing.T) {
	authenticator := &fakePasswordAuthenticator{password: "correct-password-value"}
	server := newTestHTTPServer(t, authenticator, 5, 500, func() HealthState { return HealthHealthy })
	login := performLogin(t, server.Handler(), "192.0.2.1:1234", "manager.example:34466", authenticator.password)
	cookie := login.Result().Cookies()[0]
	var loginPayload struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &loginPayload); err != nil {
		t.Fatal(err)
	}
	called := 0
	guarded := server.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		called++
		response.WriteHeader(http.StatusNoContent)
	}))

	makeRequest := func(origin, contentType, csrf string, withCookie bool) int {
		request := httptest.NewRequest(http.MethodPost, "http://manager.example:34466/api/mutate", strings.NewReader(`{}`))
		request.Host = "manager.example:34466"
		request.RemoteAddr = "192.0.2.1:1234"
		request.Header.Set("Origin", origin)
		request.Header.Set("Content-Type", contentType)
		request.Header.Set(CSRFHeaderName, csrf)
		if withCookie {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		guarded.ServeHTTP(response, request)
		return response.Code
	}

	if got := makeRequest("http://manager.example:34466", "application/json", loginPayload.CSRFToken, true); got != http.StatusNoContent {
		t.Fatalf("valid mutation status = %d, want 204", got)
	}
	if got := makeRequest("https://manager.example:34466", "application/json", loginPayload.CSRFToken, true); got != http.StatusNoContent {
		t.Fatalf("https same-host origin status = %d, want 204", got)
	}
	if got := makeRequest("", "application/json", loginPayload.CSRFToken, true); got != http.StatusForbidden {
		t.Fatalf("missing origin status = %d, want 403", got)
	}
	if got := makeRequest("http://manager.example:34466", "text/plain", loginPayload.CSRFToken, true); got != http.StatusUnsupportedMediaType {
		t.Fatalf("non-JSON mutation status = %d, want 415", got)
	}
	if got := makeRequest("http://manager.example:34466", "application/json", "wrong", true); got != http.StatusForbidden {
		t.Fatalf("invalid CSRF status = %d, want 403", got)
	}
	if got := makeRequest("http://manager.example:34466", "application/json", loginPayload.CSRFToken, false); got != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d, want 401", got)
	}
	// Both the http and https same-host origins (see above) reach the handler.
	if called != 2 {
		t.Fatalf("guarded handler calls = %d, want 2", called)
	}
}

func TestHTTPServerMutationGuardAcceptsHTTPSSameHostOrigin(t *testing.T) {
	authenticator := &fakePasswordAuthenticator{password: "correct-password-value"}
	server := newTestHTTPServer(t, authenticator, 5, 500, func() HealthState { return HealthHealthy })
	login := performLogin(t, server.Handler(), "192.0.2.1:1234", "manager.example:34466", authenticator.password)
	cookie := login.Result().Cookies()[0]
	var payload struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	called := 0
	guarded := server.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		called++
		response.WriteHeader(http.StatusNoContent)
	}))

	makeRequest := func(origin string) int {
		request := httptest.NewRequest(http.MethodPost, "http://manager.example:34466/api/mutate", strings.NewReader(`{}`))
		request.Host = "manager.example:34466"
		request.RemoteAddr = "192.0.2.1:1234"
		request.Header.Set("Origin", origin)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(CSRFHeaderName, payload.CSRFToken)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		guarded.ServeHTTP(response, request)
		return response.Code
	}

	// A TLS-terminating reverse proxy (the documented trusted-channel setup)
	// presents an https Origin while the panel itself serves plain HTTP.
	if got := makeRequest("https://manager.example:34466"); got != http.StatusNoContent {
		t.Fatalf("https same-host origin status = %d, want 204", got)
	}

	rejected := []string{
		"ftp://manager.example:34466",                 // scheme outside the allow list
		"https://manager.example:34466.evil.example",  // host mismatch
		"http://evil.example",                         // host mismatch
		"null",                                        // unparseable / opaque origin
		"",                                            // missing origin
	}
	for _, origin := range rejected {
		if got := makeRequest(origin); got != http.StatusForbidden {
			t.Fatalf("origin %q status = %d, want 403", origin, got)
		}
	}
	if called != 1 {
		t.Fatalf("guarded handler calls = %d, want 1", called)
	}
}

func TestHTTPServerLogoutRevokesSessionAndExpiresCookie(t *testing.T) {
	authenticator := &fakePasswordAuthenticator{password: "correct-password-value"}
	server := newTestHTTPServer(t, authenticator, 5, 500, func() HealthState { return HealthHealthy })
	login := performLogin(t, server.Handler(), "192.0.2.1:1234", "manager.example:34466", authenticator.password)
	cookie := login.Result().Cookies()[0]
	var payload struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "http://manager.example:34466/api/session/logout", strings.NewReader(`{}`))
	request.Host = "manager.example:34466"
	request.RemoteAddr = "192.0.2.1:1234"
	request.Header.Set("Origin", "http://manager.example:34466")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(CSRFHeaderName, payload.CSRFToken)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", response.Code)
	}
	expired := response.Result().Cookies()
	if len(expired) != 1 || expired[0].MaxAge >= 0 {
		t.Fatalf("logout cookie = %#v, want expired", expired)
	}

	current := httptest.NewRequest(http.MethodGet, "http://manager.example:34466/api/session", nil)
	current.Host = "manager.example:34466"
	current.RemoteAddr = "192.0.2.1:1234"
	current.AddCookie(cookie)
	currentResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(currentResponse, current)
	if currentResponse.Code != http.StatusUnauthorized {
		t.Fatalf("session after logout status = %d, want 401", currentResponse.Code)
	}
}

func TestHTTPServerChangesPasswordAndRevokesCurrentSession(t *testing.T) {
	authenticator := &fakePasswordAuthenticator{password: "correct-password-value"}
	server := newTestHTTPServer(t, authenticator, 5, 500, func() HealthState { return HealthHealthy })
	login := performLogin(t, server.Handler(), "192.0.2.1:1234", "manager.example:34466", authenticator.password)
	cookie := login.Result().Cookies()[0]
	var payload struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "http://manager.example:34466/api/admin/password", strings.NewReader(`{"current_password":"correct-password-value","new_password":"replacement-password-value"}`))
	request.Host = "manager.example:34466"
	request.RemoteAddr = "192.0.2.1:1234"
	request.Header.Set("Origin", "http://manager.example:34466")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(CSRFHeaderName, payload.CSRFToken)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("change password status = %d, body = %s", response.Code, response.Body.String())
	}
	if authenticator.password != "replacement-password-value" {
		t.Fatalf("password after change = %q", authenticator.password)
	}
	if cookies := response.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatalf("change password cookie = %#v, want expired", cookies)
	}

	current := httptest.NewRequest(http.MethodGet, "http://manager.example:34466/api/session", nil)
	current.Host = "manager.example:34466"
	current.RemoteAddr = "192.0.2.1:1234"
	current.AddCookie(cookie)
	currentResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(currentResponse, current)
	if currentResponse.Code != http.StatusUnauthorized {
		t.Fatalf("session after password change status = %d, want 401", currentResponse.Code)
	}
}

func TestHTTPServerRejectsInvalidPasswordChanges(t *testing.T) {
	authenticator := &fakePasswordAuthenticator{password: "correct-password-value"}
	server := newTestHTTPServer(t, authenticator, 5, 500, func() HealthState { return HealthHealthy })
	login := performLogin(t, server.Handler(), "192.0.2.1:1234", "manager.example:34466", authenticator.password)
	cookie := login.Result().Cookies()[0]
	var payload struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	request := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "http://manager.example:34466/api/admin/password", strings.NewReader(body))
		req.Host = "manager.example:34466"
		req.RemoteAddr = "192.0.2.1:1234"
		req.Header.Set("Origin", "http://manager.example:34466")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(CSRFHeaderName, payload.CSRFToken)
		req.AddCookie(cookie)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, req)
		return response.Code
	}
	if got := request(`{"current_password":"wrong-password-value","new_password":"replacement-password-value"}`); got != http.StatusForbidden {
		t.Fatalf("wrong current password status = %d, want 403", got)
	}
	if got := request(`{"current_password":"correct-password-value","new_password":"short"}`); got != http.StatusBadRequest {
		t.Fatalf("short new password status = %d, want 400", got)
	}
}

func TestHTTPServerHealthIsPublicAndOnlyExposesState(t *testing.T) {
	state := HealthDegraded
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{}, 5, 500, func() HealthState { return state })
	request := httptest.NewRequest(http.MethodGet, "http://manager.example:34466/healthz", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "{\"status\":\"degraded\"}\n" {
		t.Fatalf("degraded health response = %d %q", response.Code, response.Body.String())
	}

	state = HealthUnhealthy
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || response.Body.String() != "{\"status\":\"unhealthy\"}\n" {
		t.Fatalf("unhealthy health response = %d %q", response.Code, response.Body.String())
	}
}

func TestHTTPServerRequiresAuthenticationForEventStream(t *testing.T) {
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{}, 5, 500, func() HealthState { return HealthHealthy })
	request := httptest.NewRequest(http.MethodGet, "http://manager.example:34466/api/events", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous SSE status = %d, want 401", response.Code)
	}
}

func TestHTTPServerRejectsInvalidOptions(t *testing.T) {
	valid := HTTPServerOptions{
		Passwords:    &fakePasswordAuthenticator{},
		Sessions:     auth.NewSessionManager(nil, nil, time.Minute, time.Hour),
		Limiter:      auth.NewLoginLimiter(nil, 5, 500, time.Minute),
		Health:       func() HealthState { return HealthHealthy },
		Events:       func() *EventHub { hub, _ := NewEventHub(1); return hub }(),
		SSEHeartbeat: time.Minute,
	}
	tests := []struct {
		name   string
		mutate func(*HTTPServerOptions)
	}{
		{name: "passwords", mutate: func(options *HTTPServerOptions) { options.Passwords = nil }},
		{name: "sessions", mutate: func(options *HTTPServerOptions) { options.Sessions = nil }},
		{name: "limiter", mutate: func(options *HTTPServerOptions) { options.Limiter = nil }},
		{name: "health", mutate: func(options *HTTPServerOptions) { options.Health = nil }},
		{name: "events", mutate: func(options *HTTPServerOptions) { options.Events = nil }},
		{name: "SSE heartbeat", mutate: func(options *HTTPServerOptions) { options.SSEHeartbeat = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := valid
			test.mutate(&options)
			if _, err := NewHTTPServer(options); err == nil {
				t.Fatal("NewHTTPServer() succeeded")
			}
		})
	}

	valid.Passwords = &fakePasswordAuthenticator{err: errors.New("backend unavailable")}
	server, err := NewHTTPServer(valid)
	if err != nil {
		t.Fatal(err)
	}
	response := performLogin(t, server.Handler(), "192.0.2.1:1234", "manager.example:34466", "anything-at-least-long")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("backend error login status = %d, want 500", response.Code)
	}
}
