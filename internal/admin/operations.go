package admin

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/config"
	"github.com/s12ryt/s12ryt-ipv6/internal/dns64"
	"github.com/s12ryt/s12ryt-ipv6/internal/eventlog"
	"github.com/s12ryt/s12ryt-ipv6/internal/firewall"
	"github.com/s12ryt/s12ryt-ipv6/internal/stats"
)

type OperationsSnapshot struct {
	Health    HealthState
	NAT64     dns64.NAT64Status
	Firewall  firewall.Diagnosis
	Resolvers []config.Resolver
}

type ConnectivityCheck struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Success bool   `json:"success"`
	Address string `json:"address,omitempty"`
	Error   string `json:"error,omitempty"`
}

type OperationsService interface {
	Overview() OperationsSnapshot
	Statistics() stats.Snapshot
	TailLogs(eventlog.Filter, int) ([]eventlog.Event, error)
	ClearLogs(string) error
	ResetStatistics(string) error
	SetManualNAT64(context.Context, netip.Prefix) (dns64.NAT64Status, error)
	UpdateResolvers(context.Context, []config.Resolver) error
	TestConnectivity(context.Context) ([]ConnectivityCheck, error)
}

type nat64StatusDTO struct {
	State       dns64.NAT64State `json:"state"`
	Prefix      string           `json:"prefix,omitempty"`
	Source      string           `json:"source,omitempty"`
	Conflict    bool             `json:"conflict"`
	Manual      bool             `json:"manual"`
	LastChecked time.Time        `json:"last_checked,omitempty"`
	Error       string           `json:"error,omitempty"`
}

type operationsSnapshotDTO struct {
	Health    HealthState        `json:"health"`
	NAT64     nat64StatusDTO     `json:"nat64"`
	Firewall  firewall.Diagnosis `json:"firewall"`
	Resolvers []resolverDTO      `json:"resolvers"`
}

type resolverDTO struct {
	Name       string `json:"name"`
	Address    string `json:"address"`
	Port       uint16 `json:"port"`
	ServerName string `json:"server_name"`
	Enabled    bool   `json:"enabled"`
}

func (s *HTTPServer) SetOperationsService(service OperationsService) error {
	if service == nil {
		return errors.New("operations service is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.operationsSet {
		return errors.New("operations service is already registered")
	}
	s.operationsSet = true

	s.mux.Handle("GET /api/overview", s.RequireSession(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, operationsSnapshotToDTO(service.Overview()))
	})))
	s.mux.Handle("GET /api/stats", s.RequireSession(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, service.Statistics())
	})))
	s.mux.Handle("POST /api/stats/reset", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var input struct {
			Node    string `json:"node"`
			Confirm bool   `json:"confirm"`
		}
		if err := decodeJSON(response, request, &input); err != nil || !input.Confirm || len(input.Node) > 128 {
			writeAPIError(response, http.StatusUnprocessableEntity, "statistics reset confirmation required")
			return
		}
		if err := service.ResetStatistics(input.Node); err != nil {
			writeOperationsError(response)
			return
		}
		id := input.Node
		if id == "" {
			id = "all"
		}
		s.publishOperationsEvent("statistics", id, "reset")
		response.WriteHeader(http.StatusNoContent)
	})))
	s.mux.Handle("GET /api/logs", s.RequireSession(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		filter, limit, err := parseLogQuery(request)
		if err != nil {
			writeAPIError(response, http.StatusBadRequest, "invalid log filter")
			return
		}
		events, err := service.TailLogs(filter, limit)
		if err != nil {
			writeOperationsError(response)
			return
		}
		writeJSON(response, http.StatusOK, events)
	})))
	s.mux.Handle("POST /api/logs/clear", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var input struct {
			Confirm bool `json:"confirm"`
		}
		if err := decodeJSON(response, request, &input); err != nil || !input.Confirm {
			writeAPIError(response, http.StatusUnprocessableEntity, "log clear confirmation required")
			return
		}
		if err := service.ClearLogs("admin"); err != nil {
			writeOperationsError(response)
			return
		}
		s.publishOperationsEvent("log", "all", "cleared")
		response.WriteHeader(http.StatusNoContent)
	})))
	s.mux.Handle("POST /api/network/test", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := decodeEmptyJSON(response, request); err != nil {
			writeAPIError(response, http.StatusBadRequest, "invalid request")
			return
		}
		checks, err := service.TestConnectivity(request.Context())
		if err != nil {
			writeOperationsError(response)
			return
		}
		writeJSON(response, http.StatusOK, checks)
	})))
	s.mux.Handle("PUT /api/network/nat64", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var input struct {
			Prefix string `json:"prefix"`
		}
		if err := decodeJSON(response, request, &input); err != nil {
			writeAPIError(response, http.StatusBadRequest, "invalid NAT64 configuration")
			return
		}
		prefix, err := parseNAT64Prefix(input.Prefix)
		if err != nil {
			writeAPIError(response, http.StatusBadRequest, "invalid NAT64 configuration")
			return
		}
		status, err := service.SetManualNAT64(request.Context(), prefix)
		if err != nil {
			writeOperationsError(response)
			return
		}
		s.publishOperationsEvent("nat64", "configuration", "updated")
		writeJSON(response, http.StatusOK, nat64StatusToDTO(status))
	})))
	s.mux.Handle("PUT /api/network/resolvers", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var input struct {
			Resolvers []resolverDTO `json:"resolvers"`
		}
		if err := decodeJSON(response, request, &input); err != nil {
			writeAPIError(response, http.StatusBadRequest, "invalid resolver configuration")
			return
		}
		resolvers := resolversFromDTO(input.Resolvers)
		if validateResolvers(resolvers) != nil {
			writeAPIError(response, http.StatusBadRequest, "invalid resolver configuration")
			return
		}
		if err := service.UpdateResolvers(request.Context(), resolvers); err != nil {
			writeOperationsError(response)
			return
		}
		s.publishOperationsEvent("resolver", "configuration", "updated")
		response.WriteHeader(http.StatusNoContent)
	})))
	return nil
}

func operationsSnapshotToDTO(snapshot OperationsSnapshot) operationsSnapshotDTO {
	return operationsSnapshotDTO{
		Health: snapshot.Health, NAT64: nat64StatusToDTO(snapshot.NAT64), Firewall: snapshot.Firewall,
		Resolvers: resolversToDTO(snapshot.Resolvers),
	}
}

func resolversFromDTO(values []resolverDTO) []config.Resolver {
	result := make([]config.Resolver, 0, len(values))
	for _, value := range values {
		result = append(result, config.Resolver{
			Name: value.Name, Address: value.Address, Port: value.Port,
			ServerName: value.ServerName, Enabled: value.Enabled,
		})
	}
	return result
}

func resolversToDTO(values []config.Resolver) []resolverDTO {
	result := make([]resolverDTO, 0, len(values))
	for _, value := range values {
		result = append(result, resolverDTO{
			Name: value.Name, Address: value.Address, Port: value.Port,
			ServerName: value.ServerName, Enabled: value.Enabled,
		})
	}
	return result
}

func nat64StatusToDTO(status dns64.NAT64Status) nat64StatusDTO {
	prefix := ""
	if status.Prefix.IsValid() {
		prefix = status.Prefix.String()
	}
	return nat64StatusDTO{
		State: status.State, Prefix: prefix, Source: status.Source, Conflict: status.Conflict,
		Manual: status.Manual, LastChecked: status.LastChecked, Error: status.Error,
	}
}

func parseNAT64Prefix(value string) (netip.Prefix, error) {
	if strings.TrimSpace(value) == "" {
		return netip.Prefix{}, nil
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil || dns64.ValidateNAT64Prefix(prefix) != nil {
		return netip.Prefix{}, errors.New("invalid NAT64 prefix")
	}
	return prefix, nil
}

func validateResolvers(resolvers []config.Resolver) error {
	candidate := config.Default()
	candidate.Resolvers = append([]config.Resolver(nil), resolvers...)
	return candidate.Validate()
}

func parseLogQuery(request *http.Request) (eventlog.Filter, int, error) {
	query := request.URL.Query()
	filter := eventlog.Filter{Kind: eventlog.Kind(query.Get("kind")), Node: query.Get("node"), Action: query.Get("action")}
	if len(filter.Node) > 128 || len(filter.Action) > 128 {
		return eventlog.Filter{}, 0, errors.New("log filter is too long")
	}
	switch filter.Kind {
	case "", eventlog.KindProxy, eventlog.KindSystem, eventlog.KindAudit:
	default:
		return eventlog.Filter{}, 0, errors.New("invalid log kind")
	}
	if raw := query.Get("success"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return eventlog.Filter{}, 0, err
		}
		filter.Success = &value
	}
	limit := 200
	if raw := query.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 1000 {
			return eventlog.Filter{}, 0, errors.New("invalid log limit")
		}
		limit = value
	}
	return filter, limit, nil
}

func (s *HTTPServer) publishOperationsEvent(resource, id, action string) {
	_ = s.events.Publish(Event{
		Type: "operations.changed", Resource: resource, ID: id,
		Action: action, State: "updated", Time: time.Now(),
	})
}

func writeOperationsError(response http.ResponseWriter) {
	writeAPIError(response, http.StatusInternalServerError, "operation failed")
}
