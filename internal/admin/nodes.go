package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/node"
	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
	"github.com/s12ryt/s12ryt-ipv6/internal/secret"
)

type NodeService interface {
	Create(context.Context, node.Config, bool) (node.Node, error)
	CreateBatch(context.Context, []node.Config, bool) ([]node.Node, error)
	Update(context.Context, string, node.Config, bool) (node.Node, error)
	Start(context.Context, string) (node.Node, error)
	Stop(context.Context, string) (node.Node, error)
	Delete(context.Context, string) error
	MoveToFolder(context.Context, string, string) (node.Node, error)
	RenameFolder(context.Context, string, string) ([]node.Node, error)
	Get(string) (node.Node, bool)
	List() []node.Node
}

type nodeDTO struct {
	ID                     string             `json:"id"`
	Name                   string             `json:"name"`
	Folder                 string             `json:"folder,omitempty"`
	Protocol               node.Protocol      `json:"protocol"`
	Authentication         string             `json:"authentication,omitempty"`
	Username               string             `json:"username,omitempty"`
	Password               string             `json:"password,omitempty"`
	MaxTCP                 int                `json:"max_tcp"`
	MaxUDP                 int                `json:"max_udp"`
	DialTimeout            string             `json:"dial_timeout"`
	HandshakeTimeout       string             `json:"handshake_timeout"`
	TunnelIdleTimeout      string             `json:"tunnel_idle_timeout"`
	UDPIdleTimeout         string             `json:"udp_idle_timeout"`
	ULAOverride            policy.ULAOverride `json:"ula_override"`
	Outbound               string             `json:"outbound"`
	DedicatedPool          string             `json:"dedicated_pool,omitempty"`
	Port                   uint16             `json:"port"`
	InboundMode            node.InboundMode   `json:"inbound_mode"`
	InboundResource        string             `json:"inbound_resource,omitempty"`
	ConfirmUnauthenticated bool               `json:"confirm_unauthenticated,omitempty"`
	Status                 node.Status        `json:"status,omitempty"`
	Warning                string             `json:"warning,omitempty"`
}

type nodeBatchDTO struct {
	Folder                 string    `json:"folder"`
	ConfirmUnauthenticated bool      `json:"confirm_unauthenticated,omitempty"`
	Nodes                  []nodeDTO `json:"nodes"`
}

type folderMoveDTO struct {
	Folder string `json:"folder"`
}

type folderRenameDTO struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type folderActionDTO struct {
	Folder  string `json:"folder"`
	Action  string `json:"action"`
	Confirm bool   `json:"confirm,omitempty"`
}

type folderActionFailure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

type folderActionResponse struct {
	Succeeded []string              `json:"succeeded"`
	Failed    []folderActionFailure `json:"failed"`
}

func (s *HTTPServer) SetNodeService(service NodeService) error {
	if service == nil {
		return errors.New("node service is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nodesSet {
		return errors.New("node service is already registered")
	}
	s.nodesSet = true
	s.mux.Handle("GET /api/nodes", s.RequireSession(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		nodes := service.List()
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Config.ID < nodes[j].Config.ID })
		result := make([]nodeDTO, 0, len(nodes))
		for _, current := range nodes {
			result = append(result, nodeToDTO(current))
		}
		writeJSON(response, http.StatusOK, result)
	})))
	s.mux.Handle("POST /api/nodes", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		input, err := decodeNodeRequest(response, request)
		if err != nil {
			writeAPIError(response, http.StatusBadRequest, "invalid node configuration")
			return
		}
		created, err := service.Create(request.Context(), input.Config, input.ConfirmUnauthenticated)
		if err != nil {
			writeNodeError(response, err)
			return
		}
		s.publishNodeEvent(created, "created")
		writeJSON(response, http.StatusCreated, nodeToDTO(created))
	})))
	s.mux.Handle("POST /api/nodes/batch", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var input nodeBatchDTO
		if err := decodeJSON(response, request, &input); err != nil {
			writeAPIError(response, http.StatusBadRequest, "invalid node batch")
			return
		}
		folder, err := node.NormalizeFolderName(input.Folder)
		if err != nil || folder == "" || len(input.Nodes) == 0 || len(input.Nodes) > node.MaxBatchCreate {
			writeAPIError(response, http.StatusBadRequest, "invalid node batch")
			return
		}
		configs := make([]node.Config, 0, len(input.Nodes))
		for index, item := range input.Nodes {
			if item.Folder != "" || item.Username != "" || item.Password != "" {
				writeAPIError(response, http.StatusBadRequest, "invalid node batch")
				return
			}
			item.Folder = folder
			decoded, err := decodeNodeDTO(item)
			if err != nil || (index > 0 && !sameBatchSettings(configs[0], decoded.Config)) {
				writeAPIError(response, http.StatusBadRequest, "invalid node batch")
				return
			}
			configs = append(configs, decoded.Config)
		}
		created, err := service.CreateBatch(request.Context(), configs, input.ConfirmUnauthenticated)
		if err != nil {
			writeNodeError(response, err)
			return
		}
		result := make([]nodeDTO, 0, len(created))
		for _, current := range created {
			s.publishNodeEvent(current, "created")
			result = append(result, nodeToDTO(current))
		}
		writeJSON(response, http.StatusCreated, result)
	})))
	s.mux.Handle("GET /api/nodes/{id}", s.RequireSession(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		current, exists := service.Get(request.PathValue("id"))
		if !exists {
			writeAPIError(response, http.StatusNotFound, "node not found")
			return
		}
		writeJSON(response, http.StatusOK, nodeToDTO(current))
	})))
	s.mux.Handle("PUT /api/nodes/{id}", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		input, err := decodeNodeRequest(response, request)
		if err != nil {
			writeAPIError(response, http.StatusBadRequest, "invalid node configuration")
			return
		}
		id := request.PathValue("id")
		if input.Config.ID == "" {
			input.Config.ID = id
		}
		if input.Config.ID != id {
			writeAPIError(response, http.StatusBadRequest, "node ID cannot be changed")
			return
		}
		updated, err := service.Update(request.Context(), id, input.Config, input.ConfirmUnauthenticated)
		if err != nil && !errors.Is(err, node.ErrPreviousRuntimeCleanup) {
			writeNodeError(response, err)
			return
		}
		result := nodeToDTO(updated)
		if err != nil {
			result.Warning = "previous runtime cleanup failed"
		}
		s.publishNodeEvent(updated, "updated")
		writeJSON(response, http.StatusOK, result)
	})))
	s.mux.Handle("PUT /api/nodes/{id}/folder", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var input folderMoveDTO
		if err := decodeJSON(response, request, &input); err != nil {
			writeAPIError(response, http.StatusBadRequest, "invalid node folder")
			return
		}
		folder, err := node.NormalizeFolderName(input.Folder)
		if err != nil {
			writeAPIError(response, http.StatusBadRequest, "invalid node folder")
			return
		}
		moved, err := service.MoveToFolder(request.Context(), request.PathValue("id"), folder)
		if err != nil {
			writeNodeError(response, err)
			return
		}
		s.publishNodeEvent(moved, "moved")
		writeJSON(response, http.StatusOK, nodeToDTO(moved))
	})))
	s.mux.Handle("PUT /api/node-folders/rename", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var input folderRenameDTO
		if err := decodeJSON(response, request, &input); err != nil {
			writeAPIError(response, http.StatusBadRequest, "invalid node folder")
			return
		}
		renamed, err := service.RenameFolder(request.Context(), input.Source, input.Target)
		if err != nil {
			writeNodeError(response, err)
			return
		}
		result := make([]nodeDTO, 0, len(renamed))
		for _, current := range renamed {
			s.publishNodeEvent(current, "moved")
			result = append(result, nodeToDTO(current))
		}
		writeJSON(response, http.StatusOK, result)
	})))
	s.mux.Handle("POST /api/node-folders/action", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var input folderActionDTO
		if err := decodeJSON(response, request, &input); err != nil {
			writeAPIError(response, http.StatusBadRequest, "invalid folder action")
			return
		}
		folder, err := node.NormalizeFolderName(input.Folder)
		if err != nil || folder == "" || (input.Action != "start" && input.Action != "stop" && input.Action != "delete") {
			writeAPIError(response, http.StatusBadRequest, "invalid folder action")
			return
		}
		if input.Action == "delete" && !input.Confirm {
			writeAPIError(response, http.StatusUnprocessableEntity, "folder deletion confirmation required")
			return
		}
		members := make([]node.Node, 0)
		for _, current := range service.List() {
			if current.Config.Folder == folder {
				members = append(members, current)
			}
		}
		sort.Slice(members, func(i, j int) bool { return members[i].Config.ID < members[j].Config.ID })
		if len(members) == 0 {
			writeAPIError(response, http.StatusNotFound, "node folder not found")
			return
		}
		result := folderActionResponse{Succeeded: make([]string, 0), Failed: make([]folderActionFailure, 0)}
		for _, current := range members {
			var actionErr error
			switch input.Action {
			case "start":
				var updated node.Node
				updated, actionErr = service.Start(request.Context(), current.Config.ID)
				if actionErr == nil {
					s.publishNodeEvent(updated, "started")
				}
			case "stop":
				var updated node.Node
				updated, actionErr = service.Stop(request.Context(), current.Config.ID)
				if actionErr == nil {
					s.publishNodeEvent(updated, "stopped")
				}
			case "delete":
				actionErr = service.Delete(request.Context(), current.Config.ID)
				if actionErr == nil {
					_ = s.events.Publish(Event{Type: "node.changed", Resource: "node", ID: current.Config.ID, Action: "deleted", State: "deleted", Time: time.Now()})
				}
			}
			if actionErr != nil {
				result.Failed = append(result.Failed, folderActionFailure{ID: current.Config.ID, Error: "node operation failed"})
			} else {
				result.Succeeded = append(result.Succeeded, current.Config.ID)
			}
		}
		status := http.StatusOK
		if len(result.Failed) != 0 {
			status = http.StatusMultiStatus
		}
		writeJSON(response, status, result)
	})))
	s.mux.Handle("POST /api/nodes/{id}/start", s.RequireMutation(s.nodeActionHandler(service.Start, "started")))
	s.mux.Handle("POST /api/nodes/{id}/stop", s.RequireMutation(s.nodeActionHandler(service.Stop, "stopped")))
	s.mux.Handle("DELETE /api/nodes/{id}", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := decodeEmptyJSON(response, request); err != nil {
			writeAPIError(response, http.StatusBadRequest, "invalid request")
			return
		}
		id := request.PathValue("id")
		if err := service.Delete(request.Context(), id); err != nil {
			writeNodeError(response, err)
			return
		}
		_ = s.events.Publish(Event{Type: "node.changed", Resource: "node", ID: id, Action: "deleted", State: "deleted", Time: time.Now()})
		response.WriteHeader(http.StatusNoContent)
	})))
	return nil
}

func (s *HTTPServer) nodeActionHandler(action func(context.Context, string) (node.Node, error), eventAction string) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := decodeEmptyJSON(response, request); err != nil {
			writeAPIError(response, http.StatusBadRequest, "invalid request")
			return
		}
		current, err := action(request.Context(), request.PathValue("id"))
		if err != nil {
			writeNodeError(response, err)
			return
		}
		s.publishNodeEvent(current, eventAction)
		writeJSON(response, http.StatusOK, nodeToDTO(current))
	})
}

type decodedNodeRequest struct {
	Config                 node.Config
	ConfirmUnauthenticated bool
}

func decodeNodeRequest(response http.ResponseWriter, request *http.Request) (decodedNodeRequest, error) {
	var input nodeDTO
	if err := decodeJSON(response, request, &input); err != nil {
		return decodedNodeRequest{}, err
	}
	return decodeNodeDTO(input)
}

func decodeNodeDTO(input nodeDTO) (decodedNodeRequest, error) {
	authentication := input.Authentication
	if authentication == "" {
		if input.Username != "" || input.Password != "" {
			authentication = "credentials"
		} else {
			authentication = "none"
		}
	}
	switch authentication {
	case "credentials":
		credentials, err := secret.NewProxyCredentials(input.Username, input.Password, nil)
		if err != nil {
			return decodedNodeRequest{}, err
		}
		input.Username, input.Password = credentials.Username, credentials.Password
		input.ConfirmUnauthenticated = false
	case "none":
		if input.Username != "" || input.Password != "" {
			return decodedNodeRequest{}, errors.New("unauthenticated nodes cannot include credentials")
		}
	default:
		return decodedNodeRequest{}, errors.New("invalid authentication mode")
	}
	dialTimeout, err := parseDurationField("dial_timeout", input.DialTimeout)
	if err != nil {
		return decodedNodeRequest{}, err
	}
	handshakeTimeout, err := parseDurationField("handshake_timeout", input.HandshakeTimeout)
	if err != nil {
		return decodedNodeRequest{}, err
	}
	tunnelIdleTimeout, err := parseDurationField("tunnel_idle_timeout", input.TunnelIdleTimeout)
	if err != nil {
		return decodedNodeRequest{}, err
	}
	udpIdleTimeout, err := parseDurationField("udp_idle_timeout", input.UDPIdleTimeout)
	if err != nil {
		return decodedNodeRequest{}, err
	}
	config := node.Config{
		ID: input.ID, Name: input.Name, Folder: input.Folder, Protocol: input.Protocol,
		Username: input.Username, Password: input.Password,
		MaxTCP: input.MaxTCP, MaxUDP: input.MaxUDP,
		DialTimeout: dialTimeout, HandshakeTimeout: handshakeTimeout,
		TunnelIdleTimeout: tunnelIdleTimeout, UDPIdleTimeout: udpIdleTimeout,
		ULAOverride: input.ULAOverride, Outbound: input.Outbound,
		DedicatedPool: input.DedicatedPool, Port: input.Port,
		InboundMode: input.InboundMode, InboundResource: input.InboundResource,
	}
	if err := config.Validate(); err != nil {
		return decodedNodeRequest{}, err
	}
	return decodedNodeRequest{Config: config, ConfirmUnauthenticated: input.ConfirmUnauthenticated}, nil
}

func sameBatchSettings(first, next node.Config) bool {
	return first.Protocol == next.Protocol &&
		(first.Username == "") == (next.Username == "") &&
		first.MaxTCP == next.MaxTCP && first.MaxUDP == next.MaxUDP &&
		first.DialTimeout == next.DialTimeout && first.HandshakeTimeout == next.HandshakeTimeout &&
		first.TunnelIdleTimeout == next.TunnelIdleTimeout && first.UDPIdleTimeout == next.UDPIdleTimeout &&
		first.ULAOverride == next.ULAOverride && first.Outbound == next.Outbound &&
		first.DedicatedPool == next.DedicatedPool && first.InboundMode == next.InboundMode &&
		first.InboundResource == next.InboundResource
}

func parseDurationField(name, value string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return duration, nil
}

func nodeToDTO(current node.Node) nodeDTO {
	return nodeDTO{
		ID: current.Config.ID, Name: current.Config.Name, Folder: current.Config.Folder, Protocol: current.Config.Protocol,
		Authentication: nodeAuthentication(current.Config),
		Username:       current.Config.Username, Password: current.Config.Password,
		MaxTCP: current.Config.MaxTCP, MaxUDP: current.Config.MaxUDP,
		DialTimeout: current.Config.DialTimeout.String(), HandshakeTimeout: current.Config.HandshakeTimeout.String(),
		TunnelIdleTimeout: current.Config.TunnelIdleTimeout.String(), UDPIdleTimeout: current.Config.UDPIdleTimeout.String(),
		ULAOverride: current.Config.ULAOverride, Outbound: current.Config.Outbound,
		DedicatedPool: current.Config.DedicatedPool, Port: current.Config.Port,
		InboundMode: current.Config.InboundMode, InboundResource: current.Config.InboundResource,
		Status: current.Status,
	}
}

func nodeAuthentication(config node.Config) string {
	if config.Username == "" && config.Password == "" {
		return "none"
	}
	return "credentials"
}

func decodeEmptyJSON(response http.ResponseWriter, request *http.Request) error {
	var value struct{}
	return decodeJSON(response, request, &value)
}

func (s *HTTPServer) publishNodeEvent(current node.Node, action string) {
	_ = s.events.Publish(Event{
		Type: "node.changed", Resource: "node", ID: current.Config.ID,
		Action: action, State: string(current.Status), Time: time.Now(),
	})
}

func writeNodeError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, node.ErrNodeNotFound):
		writeAPIError(response, http.StatusNotFound, "node not found")
	case errors.Is(err, node.ErrNodeExists), errors.Is(err, node.ErrNodeLimit):
		writeAPIError(response, http.StatusConflict, "node conflicts with existing state")
	case errors.Is(err, node.ErrFolderExists):
		writeAPIError(response, http.StatusConflict, "node folder conflicts with existing state")
	case errors.Is(err, node.ErrFolderNotFound):
		writeAPIError(response, http.StatusNotFound, "node folder not found")
	case errors.Is(err, node.ErrBatchSize):
		writeAPIError(response, http.StatusBadRequest, "invalid node batch")
	case errors.Is(err, node.ErrUnauthenticatedRiskConfirmation):
		writeAPIError(response, http.StatusUnprocessableEntity, "unauthenticated proxy risk confirmation required")
	default:
		writeAPIError(response, http.StatusInternalServerError, "node operation failed")
	}
}
