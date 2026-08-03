package admin

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHTTPServerServesEmbeddedFrontendAndSPARoutes(t *testing.T) {
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "test-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	frontend := fstest.MapFS{
		"index.html":        {Data: []byte("<html><body>管理中心</body></html>")},
		"assets/app-123.js": {Data: []byte("console.log('app')")},
	}
	if err := server.SetFrontend(frontend); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path         string
		status       int
		body         string
		cacheControl string
	}{
		{path: "/", status: http.StatusOK, body: "管理中心", cacheControl: "no-store"},
		{path: "/nodes", status: http.StatusOK, body: "管理中心", cacheControl: "no-store"},
		{path: "/assets/app-123.js", status: http.StatusOK, body: "console.log('app')", cacheControl: "public, max-age=31536000, immutable"},
		{path: "/assets/missing.js", status: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if test.body != "" && !strings.Contains(response.Body.String(), test.body) {
				t.Fatalf("body = %q", response.Body.String())
			}
			if got := response.Header().Get("Cache-Control"); got != test.cacheControl {
				t.Fatalf("Cache-Control = %q, want %q", got, test.cacheControl)
			}
		})
	}

	health := httptest.NewRecorder()
	server.Handler().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), "healthy") {
		t.Fatalf("health response = %d %s", health.Code, health.Body.String())
	}
	if err := server.SetFrontend(frontend); err == nil {
		t.Fatal("second SetFrontend() error = nil")
	}
	if err := server.SetFrontend(nil); err == nil {
		t.Fatal("SetFrontend(nil) error = nil")
	}
}

func TestFrontendHandlerRejectsInvalidFilesystem(t *testing.T) {
	invalid := fstest.MapFS{"other.html": {Data: []byte("missing index")}}
	if _, err := fs.Stat(invalid, "index.html"); err == nil {
		t.Fatal("fixture unexpectedly contains index.html")
	}
	server := newTestHTTPServer(t, &fakePasswordAuthenticator{password: "test-password-value"}, 5, 500, func() HealthState { return HealthHealthy })
	if err := server.SetFrontend(invalid); err == nil {
		t.Fatal("SetFrontend(missing index) error = nil")
	}
}
