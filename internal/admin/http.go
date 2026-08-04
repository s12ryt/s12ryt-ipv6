package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/auth"
	"github.com/s12ryt/s12ryt-ipv6/internal/secret"
)

const (
	SessionCookieName = "s12ryt_session"
	CSRFHeaderName    = "X-CSRF-Token"
	maxJSONBodyBytes  = 4 * 1024
)

type HealthState string

const (
	HealthHealthy   HealthState = "healthy"
	HealthDegraded  HealthState = "degraded"
	HealthUnhealthy HealthState = "unhealthy"
)

type PasswordService interface {
	Authenticate(password string) (bool, error)
	Change(current, replacement string) error
}

type HTTPServerOptions struct {
	Passwords    PasswordService
	Sessions     *auth.SessionManager
	Limiter      *auth.LoginLimiter
	Health       func() HealthState
	Events       *EventHub
	SSEHeartbeat time.Duration
}

type HTTPServer struct {
	mu            sync.Mutex
	passwords     PasswordService
	sessions      *auth.SessionManager
	limiter       *auth.LoginLimiter
	health        func() HealthState
	events        *EventHub
	mux           *http.ServeMux
	handler       http.Handler
	nodesSet      bool
	resourcesSet  bool
	operationsSet bool
	discoverySet  bool
	frontendSet   bool
}

func NewHTTPServer(options HTTPServerOptions) (*HTTPServer, error) {
	if options.Passwords == nil {
		return nil, errors.New("password authenticator is required")
	}
	if options.Sessions == nil {
		return nil, errors.New("session manager is required")
	}
	if options.Limiter == nil {
		return nil, errors.New("login limiter is required")
	}
	if options.Health == nil {
		return nil, errors.New("health provider is required")
	}
	if options.Events == nil {
		return nil, errors.New("event hub is required")
	}
	stream, err := NewSSEHandler(options.Events, options.SSEHeartbeat)
	if err != nil {
		return nil, err
	}
	server := &HTTPServer{
		passwords: options.Passwords,
		sessions:  options.Sessions,
		limiter:   options.Limiter,
		health:    options.Health,
		events:    options.Events,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.handleHealth)
	mux.HandleFunc("POST /api/session", server.handleLogin)
	mux.Handle("GET /api/session", server.RequireSession(http.HandlerFunc(server.handleCurrentSession)))
	mux.Handle("POST /api/session/logout", server.RequireMutation(http.HandlerFunc(server.handleLogout)))
	mux.Handle("POST /api/admin/password", server.RequireMutation(http.HandlerFunc(server.handleChangePassword)))
	mux.Handle("GET /api/events", server.RequireSession(stream))
	server.mux = mux
	server.handler = mux
	return server, nil
}

func (s *HTTPServer) Handler() http.Handler {
	return s.handler
}

func (s *HTTPServer) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		cookie, err := request.Cookie(SessionCookieName)
		if err != nil || cookie.Value == "" {
			writeAPIError(response, http.StatusUnauthorized, "authentication required")
			return
		}
		if _, err := s.sessions.Validate(cookie.Value); err != nil {
			writeAPIError(response, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (s *HTTPServer) RequireMutation(next http.Handler) http.Handler {
	return s.RequireSession(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Origin") != "http://"+request.Host {
			writeAPIError(response, http.StatusForbidden, "origin rejected")
			return
		}
		if !isJSONContentType(request.Header.Get("Content-Type")) {
			writeAPIError(response, http.StatusUnsupportedMediaType, "application/json required")
			return
		}
		cookie, _ := request.Cookie(SessionCookieName)
		if err := s.sessions.ValidateCSRF(cookie.Value, request.Header.Get(CSRFHeaderName)); err != nil {
			writeAPIError(response, http.StatusForbidden, "CSRF token rejected")
			return
		}
		next.ServeHTTP(response, request)
	}))
}

func (s *HTTPServer) handleLogin(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	if !isJSONContentType(request.Header.Get("Content-Type")) {
		writeAPIError(response, http.StatusUnsupportedMediaType, "application/json required")
		return
	}
	source, err := remoteAddress(request.RemoteAddr)
	if err != nil {
		writeAPIError(response, http.StatusBadRequest, "invalid client address")
		return
	}
	if !s.limiter.Allow(source) {
		response.Header().Set("Retry-After", "900")
		writeAPIError(response, http.StatusTooManyRequests, "login temporarily blocked")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(response, request, &body); err != nil || body.Password == "" {
		writeAPIError(response, http.StatusBadRequest, "invalid request")
		return
	}
	valid, err := s.passwords.Authenticate(body.Password)
	if err != nil {
		writeAPIError(response, http.StatusInternalServerError, "authentication unavailable")
		return
	}
	if !valid {
		s.limiter.RecordFailure(source)
		writeAPIError(response, http.StatusUnauthorized, "invalid credentials")
		return
	}
	s.limiter.Reset(source)
	session, err := s.sessions.Create()
	if err != nil {
		writeAPIError(response, http.StatusInternalServerError, "session unavailable")
		return
	}
	http.SetCookie(response, &http.Cookie{
		Name:     SessionCookieName,
		Value:    session.Token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(response, http.StatusOK, map[string]string{"csrf_token": session.CSRFToken})
}

func (s *HTTPServer) handleCurrentSession(response http.ResponseWriter, request *http.Request) {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		writeAPIError(response, http.StatusUnauthorized, "authentication required")
		return
	}
	csrfToken, err := s.sessions.RotateCSRF(cookie.Value)
	if err != nil {
		writeAPIError(response, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSON(response, http.StatusOK, struct {
		Authenticated bool   `json:"authenticated"`
		CSRFToken     string `json:"csrf_token"`
	}{Authenticated: true, CSRFToken: csrfToken})
}

func (s *HTTPServer) handleLogout(response http.ResponseWriter, _ *http.Request) {
	s.sessions.Revoke()
	expireSessionCookie(response)
	response.WriteHeader(http.StatusNoContent)
}

func (s *HTTPServer) handleChangePassword(response http.ResponseWriter, request *http.Request) {
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeAPIError(response, http.StatusBadRequest, "invalid request")
		return
	}
	if err := secret.ValidateAdminPassword(body.NewPassword); err != nil {
		writeAPIError(response, http.StatusBadRequest, "invalid new password")
		return
	}
	if err := s.passwords.Change(body.CurrentPassword, body.NewPassword); err != nil {
		if errors.Is(err, ErrInvalidCurrentPassword) {
			writeAPIError(response, http.StatusForbidden, "current password rejected")
			return
		}
		writeAPIError(response, http.StatusInternalServerError, "password update unavailable")
		return
	}
	s.sessions.Revoke()
	expireSessionCookie(response)
	response.WriteHeader(http.StatusNoContent)
}

func expireSessionCookie(response http.ResponseWriter) {
	http.SetCookie(response, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func (s *HTTPServer) handleHealth(response http.ResponseWriter, _ *http.Request) {
	state := s.health()
	status := http.StatusOK
	switch state {
	case HealthHealthy, HealthDegraded:
	case HealthUnhealthy:
		status = http.StatusServiceUnavailable
	default:
		state = HealthUnhealthy
		status = http.StatusServiceUnavailable
	}
	writeJSON(response, status, map[string]HealthState{"status": state})
}

func decodeJSON(response http.ResponseWriter, request *http.Request, destination any) error {
	request.Body = http.MaxBytesReader(response, request.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func isJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func remoteAddress(value string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parse remote address: %w", err)
	}
	address, err := netip.ParseAddr(host)
	if err != nil || !address.IsValid() {
		return netip.Addr{}, errors.New("remote address is not an IP address")
	}
	return address.Unmap(), nil
}

func writeAPIError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]string{"error": message})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
