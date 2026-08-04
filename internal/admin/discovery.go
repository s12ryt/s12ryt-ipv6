package admin

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"sort"

	"github.com/s12ryt/s12ryt-ipv6/internal/ipv6resource"
	"github.com/s12ryt/s12ryt-ipv6/internal/network"
)

type PrefixConflictReason string

const (
	PrefixConflictExact   PrefixConflictReason = "exact"
	PrefixConflictOverlap PrefixConflictReason = "overlap"
)

type PrefixConflict struct {
	Template string
	Reason   PrefixConflictReason
}

type NetworkPrefixCandidate struct {
	Interface string
	Prefix    netip.Prefix
	Sources   []network.PrefixSource
	Available bool
	Conflicts []PrefixConflict
}

type NetworkCandidateSnapshot struct {
	Interfaces []network.DiscoveredInterface
	Prefixes   []NetworkPrefixCandidate
}

type NetworkCandidateProvider interface {
	Snapshot(context.Context) (NetworkCandidateSnapshot, error)
}

type NetworkCandidateService struct {
	discovery network.NetworkDiscovery
	resources func() ResourceSnapshot
}

func NewNetworkCandidateService(discovery network.NetworkDiscovery, resources func() ResourceSnapshot) (*NetworkCandidateService, error) {
	if discovery == nil {
		return nil, errors.New("network discovery is required")
	}
	if resources == nil {
		return nil, errors.New("resource snapshot provider is required")
	}
	return &NetworkCandidateService{discovery: discovery, resources: resources}, nil
}

func (s *NetworkCandidateService) Snapshot(ctx context.Context) (NetworkCandidateSnapshot, error) {
	discovered, err := s.discovery.Discover(ctx)
	if err != nil {
		return NetworkCandidateSnapshot{}, err
	}
	templates := append([]ipv6resource.PrefixTemplate(nil), s.resources().Templates...)
	sort.Slice(templates, func(left, right int) bool {
		return templates[left].Name < templates[right].Name
	})

	result := NetworkCandidateSnapshot{
		Interfaces: append([]network.DiscoveredInterface(nil), discovered.Interfaces...),
		Prefixes:   make([]NetworkPrefixCandidate, 0, len(discovered.Prefixes)),
	}
	for _, discoveredPrefix := range discovered.Prefixes {
		candidate := NetworkPrefixCandidate{
			Interface: discoveredPrefix.Interface,
			Prefix:    discoveredPrefix.Prefix,
			Sources:   append([]network.PrefixSource(nil), discoveredPrefix.Sources...),
		}
		for _, template := range templates {
			if reason, conflicts := prefixConflict(candidate.Prefix, template.Prefix); conflicts {
				candidate.Conflicts = append(candidate.Conflicts, PrefixConflict{Template: template.Name, Reason: reason})
			}
		}
		candidate.Available = len(candidate.Conflicts) == 0
		result.Prefixes = append(result.Prefixes, candidate)
	}
	return result, nil
}

func prefixConflict(candidate, existing netip.Prefix) (PrefixConflictReason, bool) {
	if candidate == existing {
		return PrefixConflictExact, true
	}
	if candidate.Contains(existing.Addr()) || existing.Contains(candidate.Addr()) {
		return PrefixConflictOverlap, true
	}
	return "", false
}

type discoveredInterfaceDTO struct {
	Name  string `json:"name"`
	Index int    `json:"index"`
}

type prefixConflictDTO struct {
	Template string               `json:"template"`
	Reason   PrefixConflictReason `json:"reason"`
}

type networkPrefixCandidateDTO struct {
	Interface string                 `json:"interface"`
	Prefix    string                 `json:"prefix"`
	Sources   []network.PrefixSource `json:"sources"`
	Available bool                   `json:"available"`
	Conflicts []prefixConflictDTO    `json:"conflicts"`
}

type networkCandidateSnapshotDTO struct {
	Interfaces []discoveredInterfaceDTO    `json:"interfaces"`
	Prefixes   []networkPrefixCandidateDTO `json:"prefixes"`
}

func (s *HTTPServer) SetNetworkCandidateService(service NetworkCandidateProvider) error {
	if service == nil {
		return errors.New("network candidate service is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.discoverySet {
		return errors.New("network candidate service is already registered")
	}
	s.discoverySet = true
	s.mux.Handle("GET /api/discovery/network", s.RequireSession(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		snapshot, err := service.Snapshot(request.Context())
		if err != nil {
			writeAPIError(response, http.StatusServiceUnavailable, "network discovery unavailable")
			return
		}
		writeJSON(response, http.StatusOK, networkCandidateSnapshotToDTO(snapshot))
	})))
	return nil
}

func networkCandidateSnapshotToDTO(snapshot NetworkCandidateSnapshot) networkCandidateSnapshotDTO {
	result := networkCandidateSnapshotDTO{
		Interfaces: make([]discoveredInterfaceDTO, 0, len(snapshot.Interfaces)),
		Prefixes:   make([]networkPrefixCandidateDTO, 0, len(snapshot.Prefixes)),
	}
	for _, value := range snapshot.Interfaces {
		result.Interfaces = append(result.Interfaces, discoveredInterfaceDTO{Name: value.Name, Index: value.Index})
	}
	for _, value := range snapshot.Prefixes {
		candidate := networkPrefixCandidateDTO{
			Interface: value.Interface,
			Prefix:    value.Prefix.String(),
			Sources:   append([]network.PrefixSource(nil), value.Sources...),
			Available: value.Available,
			Conflicts: make([]prefixConflictDTO, 0, len(value.Conflicts)),
		}
		for _, conflict := range value.Conflicts {
			candidate.Conflicts = append(candidate.Conflicts, prefixConflictDTO{Template: conflict.Template, Reason: conflict.Reason})
		}
		result.Prefixes = append(result.Prefixes, candidate)
	}
	return result
}

var _ NetworkCandidateProvider = (*NetworkCandidateService)(nil)
