package admin

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
)

type ResourceSnapshot struct {
	Templates []ipv6resource.PrefixTemplate
	Fixed     []ipv6resource.FixedAddress
	Addresses []ipv6resource.CanonicalAddress
	Pools     []*ipv6resource.Pool
}

type ResourceService interface {
	Snapshot() ResourceSnapshot
	CreateTemplate(context.Context, ipv6resource.PrefixTemplate) error
	DeleteTemplate(context.Context, string) error
	CreateFixedAddress(context.Context, string, string, *netip.Addr) (ipv6resource.FixedAddress, error)
	DeleteFixedAddress(context.Context, string) error
	CreatePool(context.Context, string, ipv6resource.PoolKind, string, int, []string) (*ipv6resource.Pool, error)
	DeletePool(context.Context, string) error
	RefreshPool(context.Context, string) (*ipv6resource.Pool, error)
	ForceDrain(context.Context, string, string) error
}

type prefixTemplateDTO struct {
	Name      string                  `json:"name"`
	Prefix    string                  `json:"prefix"`
	Interface string                  `json:"interface"`
	Mode      ipv6resource.ConfigMode `json:"mode"`
}

type fixedAddressDTO struct {
	Name      string                 `json:"name"`
	Template  string                 `json:"template"`
	Address   string                 `json:"address"`
	Ownership ipv6resource.Ownership `json:"ownership"`
}

type canonicalAddressDTO struct {
	Address    string                 `json:"address"`
	Template   string                 `json:"template"`
	Ownership  ipv6resource.Ownership `json:"ownership"`
	References int                    `json:"references"`
}

type drainBatchDTO struct {
	ID        string   `json:"id"`
	Addresses []string `json:"addresses"`
}

type poolDTO struct {
	Name     string                `json:"name"`
	Kind     ipv6resource.PoolKind `json:"kind"`
	Template string                `json:"template"`
	Capacity int                   `json:"capacity"`
	Pinned   []string              `json:"pinned"`
	Active   []string              `json:"active"`
	Draining []drainBatchDTO       `json:"draining"`
}

type resourceSnapshotDTO struct {
	Templates []prefixTemplateDTO   `json:"templates"`
	Fixed     []fixedAddressDTO     `json:"fixed"`
	Addresses []canonicalAddressDTO `json:"addresses"`
	Pools     []poolDTO             `json:"pools"`
}

func (s *HTTPServer) SetResourceService(service ResourceService) error {
	if service == nil {
		return errors.New("resource service is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resourcesSet {
		return errors.New("resource service is already registered")
	}
	s.resourcesSet = true

	s.mux.Handle("GET /api/resources", s.RequireSession(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, resourceSnapshotToDTO(service.Snapshot()))
	})))
	s.mux.Handle("POST /api/resources/templates", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var input prefixTemplateDTO
		if err := decodeJSON(response, request, &input); err != nil {
			writeResourceError(response)
			return
		}
		template, err := ipv6resource.NewPrefixTemplate(input.Name, input.Prefix, input.Interface, input.Mode)
		if err != nil || service.CreateTemplate(request.Context(), template) != nil {
			writeResourceError(response)
			return
		}
		s.publishResourceEvent("template", template.Name, "created", "active")
		writeJSON(response, http.StatusCreated, prefixTemplateToDTO(template))
	})))
	s.mux.Handle("DELETE /api/resources/templates/{name}", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := decodeEmptyJSON(response, request); err != nil || service.DeleteTemplate(request.Context(), request.PathValue("name")) != nil {
			writeResourceError(response)
			return
		}
		name := request.PathValue("name")
		s.publishResourceEvent("template", name, "deleted", "deleted")
		response.WriteHeader(http.StatusNoContent)
	})))
	s.mux.Handle("POST /api/resources/fixed", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var input struct {
			Name     string  `json:"name"`
			Template string  `json:"template"`
			Address  *string `json:"address,omitempty"`
		}
		if err := decodeJSON(response, request, &input); err != nil {
			writeResourceError(response)
			return
		}
		address, err := parseOptionalResourceAddress(input.Address)
		if err != nil {
			writeResourceError(response)
			return
		}
		fixed, err := service.CreateFixedAddress(request.Context(), input.Name, input.Template, address)
		if err != nil {
			writeResourceError(response)
			return
		}
		s.publishResourceEvent("fixed-address", fixed.Name, "created", "active")
		writeJSON(response, http.StatusCreated, fixedAddressToDTO(fixed))
	})))
	s.mux.Handle("DELETE /api/resources/fixed/{name}", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := decodeEmptyJSON(response, request); err != nil || service.DeleteFixedAddress(request.Context(), request.PathValue("name")) != nil {
			writeResourceError(response)
			return
		}
		name := request.PathValue("name")
		s.publishResourceEvent("fixed-address", name, "deleted", "deleted")
		response.WriteHeader(http.StatusNoContent)
	})))
	s.mux.Handle("POST /api/resources/pools", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var input struct {
			Name     string                `json:"name"`
			Kind     ipv6resource.PoolKind `json:"kind"`
			Template string                `json:"template"`
			Capacity int                   `json:"capacity"`
			Pinned   []string              `json:"pinned"`
		}
		if err := decodeJSON(response, request, &input); err != nil {
			writeResourceError(response)
			return
		}
		pool, err := service.CreatePool(request.Context(), input.Name, input.Kind, input.Template, input.Capacity, input.Pinned)
		if err != nil {
			writeResourceError(response)
			return
		}
		s.publishResourceEvent("pool", pool.Name, "created", "active")
		writeJSON(response, http.StatusCreated, poolToDTO(pool))
	})))
	s.mux.Handle("DELETE /api/resources/pools/{name}", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := decodeEmptyJSON(response, request); err != nil || service.DeletePool(request.Context(), request.PathValue("name")) != nil {
			writeResourceError(response)
			return
		}
		name := request.PathValue("name")
		s.publishResourceEvent("pool", name, "deleted", "deleted")
		response.WriteHeader(http.StatusNoContent)
	})))
	s.mux.Handle("POST /api/resources/pools/{name}/refresh", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := decodeEmptyJSON(response, request); err != nil {
			writeResourceError(response)
			return
		}
		pool, err := service.RefreshPool(request.Context(), request.PathValue("name"))
		if err != nil {
			writeResourceError(response)
			return
		}
		s.publishResourceEvent("pool", pool.Name, "refreshed", "draining")
		writeJSON(response, http.StatusOK, poolToDTO(pool))
	})))
	s.mux.Handle("POST /api/resources/pools/{name}/drains/{batch}/force", s.RequireMutation(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var input struct {
			Confirm bool `json:"confirm"`
		}
		if err := decodeJSON(response, request, &input); err != nil || !input.Confirm {
			writeAPIError(response, http.StatusUnprocessableEntity, "force drain confirmation required")
			return
		}
		name, batch := request.PathValue("name"), request.PathValue("batch")
		if err := service.ForceDrain(request.Context(), name, batch); err != nil {
			writeResourceError(response)
			return
		}
		s.publishResourceEvent("pool", name, "drain-forced", "active")
		response.WriteHeader(http.StatusNoContent)
	})))
	return nil
}

func parseOptionalResourceAddress(value *string) (*netip.Addr, error) {
	if value == nil {
		return nil, nil
	}
	address, err := netip.ParseAddr(*value)
	if err != nil || address.Zone() != "" || !address.Is6() || address.Is4In6() {
		return nil, errors.New("address must be a native IPv6 literal")
	}
	return &address, nil
}

func resourceSnapshotToDTO(snapshot ResourceSnapshot) resourceSnapshotDTO {
	result := resourceSnapshotDTO{
		Templates: make([]prefixTemplateDTO, 0, len(snapshot.Templates)),
		Fixed:     make([]fixedAddressDTO, 0, len(snapshot.Fixed)),
		Addresses: make([]canonicalAddressDTO, 0, len(snapshot.Addresses)),
		Pools:     make([]poolDTO, 0, len(snapshot.Pools)),
	}
	for _, template := range snapshot.Templates {
		result.Templates = append(result.Templates, prefixTemplateToDTO(template))
	}
	for _, fixed := range snapshot.Fixed {
		result.Fixed = append(result.Fixed, fixedAddressToDTO(fixed))
	}
	for _, address := range snapshot.Addresses {
		result.Addresses = append(result.Addresses, canonicalAddressDTO{
			Address: address.Address.String(), Template: address.Template,
			Ownership: address.Ownership, References: address.References,
		})
	}
	for _, pool := range snapshot.Pools {
		result.Pools = append(result.Pools, poolToDTO(pool))
	}
	return result
}

func prefixTemplateToDTO(template ipv6resource.PrefixTemplate) prefixTemplateDTO {
	return prefixTemplateDTO{Name: template.Name, Prefix: template.Prefix.String(), Interface: template.Interface, Mode: template.Mode}
}

func fixedAddressToDTO(fixed ipv6resource.FixedAddress) fixedAddressDTO {
	return fixedAddressDTO{Name: fixed.Name, Template: fixed.Template, Address: fixed.Address.String(), Ownership: fixed.Ownership}
}

func poolToDTO(pool *ipv6resource.Pool) poolDTO {
	if pool == nil {
		return poolDTO{}
	}
	result := poolDTO{
		Name: pool.Name, Kind: pool.Kind, Template: pool.Template, Capacity: pool.Capacity,
		Pinned: addressesToStrings(pool.Pinned), Active: addressesToStrings(pool.Active),
		Draining: make([]drainBatchDTO, 0, len(pool.Draining)),
	}
	for _, batch := range pool.Draining {
		result.Draining = append(result.Draining, drainBatchDTO{ID: batch.ID, Addresses: addressesToStrings(batch.Addresses)})
	}
	return result
}

func addressesToStrings(addresses []netip.Addr) []string {
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		result = append(result, address.String())
	}
	return result
}

func (s *HTTPServer) publishResourceEvent(resource, id, action, state string) {
	_ = s.events.Publish(Event{
		Type: "resource.changed", Resource: resource, ID: id,
		Action: action, State: state, Time: time.Now(),
	})
}

func writeResourceError(response http.ResponseWriter) {
	writeAPIError(response, http.StatusBadRequest, "resource operation rejected")
}
