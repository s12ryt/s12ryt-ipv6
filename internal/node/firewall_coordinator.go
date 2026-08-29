package node

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"

	"github.com/s12ryt/s12ryt-ipv6/internal/firewall"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
)

type FirewallRulesetReplacer interface {
	Replace(context.Context, []firewall.Opening) error
}

// relayScope identifies the address a UDP relay association binds to. UDP
// relay ports are always allocated from the fixed port range passed to
// NewFirewallCoordinator, so all associations on the same (family, address)
// pair can share one port-range opening instead of rewriting the whole
// nftables ruleset per association. While at least one association is active
// the entire relay port range stays open on that address; the range is
// reserved for this program's own allocator and incoming packets still need a
// bound socket to be delivered.
type relayScope struct {
	family  proxy.BindFamily
	address netip.Addr
}

func relayScopeOf(endpoint proxy.BindEndpoint) relayScope {
	return relayScope{family: endpoint.Family, address: endpoint.Address}
}

type FirewallCoordinator struct {
	mu           sync.Mutex
	replacer     FirewallRulesetReplacer
	management   []proxy.BindEndpoint
	nodes        map[string][]proxy.BindEndpoint
	relayScopes  map[relayScope]int
	relayPortMin uint16
	relayPortMax uint16
}

func NewFirewallCoordinator(ctx context.Context, replacer FirewallRulesetReplacer, management []proxy.BindEndpoint, relayPortMin, relayPortMax uint16) (*FirewallCoordinator, error) {
	if replacer == nil {
		return nil, errors.New("firewall ruleset replacer is required")
	}
	if relayPortMin == 0 || relayPortMax == 0 || relayPortMin > relayPortMax {
		return nil, fmt.Errorf("invalid UDP relay port range %d-%d", relayPortMin, relayPortMax)
	}
	normalized, err := normalizeFirewallEndpoints(management, proxy.BindTCP, true)
	if err != nil {
		return nil, fmt.Errorf("validate management firewall endpoints: %w", err)
	}
	coordinator := &FirewallCoordinator{
		replacer: replacer, management: normalized,
		nodes:        make(map[string][]proxy.BindEndpoint),
		relayScopes:  make(map[relayScope]int),
		relayPortMin: relayPortMin, relayPortMax: relayPortMax,
	}
	if err := replacer.Replace(ctx, coordinator.openings(normalized, coordinator.nodes, coordinator.relayScopes)); err != nil {
		return nil, fmt.Errorf("apply initial firewall rules: %w", err)
	}
	return coordinator, nil
}

func (c *FirewallCoordinator) OpenNode(ctx context.Context, id string, endpoints []proxy.BindEndpoint) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("node ID is required for firewall opening")
	}
	normalized, err := normalizeFirewallEndpoints(endpoints, proxy.BindTCP, false)
	if err != nil {
		return fmt.Errorf("validate node firewall endpoints: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	next := cloneNodeEndpoints(c.nodes)
	next[id] = normalized
	if err := c.replacer.Replace(ctx, c.openings(c.management, next, c.relayScopes)); err != nil {
		return fmt.Errorf("apply node firewall opening: %w", err)
	}
	c.nodes = next
	return nil
}

func (c *FirewallCoordinator) CloseNode(ctx context.Context, id string, endpoints []proxy.BindEndpoint) error {
	normalized, err := normalizeFirewallEndpoints(endpoints, proxy.BindTCP, false)
	if err != nil {
		return fmt.Errorf("validate node firewall endpoints: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	current, exists := c.nodes[id]
	if !exists || !sameEndpointSet(current, normalized) {
		return nil
	}
	next := cloneNodeEndpoints(c.nodes)
	delete(next, id)
	if err := c.replacer.Replace(ctx, c.openings(c.management, next, c.relayScopes)); err != nil {
		return fmt.Errorf("close node firewall opening: %w", err)
	}
	c.nodes = next
	return nil
}

func (c *FirewallCoordinator) Open(ctx context.Context, endpoint proxy.BindEndpoint) error {
	normalized, err := normalizeFirewallEndpoints([]proxy.BindEndpoint{endpoint}, proxy.BindUDP, false)
	if err != nil {
		return fmt.Errorf("validate UDP relay firewall endpoint: %w", err)
	}
	scope := relayScopeOf(normalized[0])
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.relayScopes[scope] > 0 {
		c.relayScopes[scope]++
		return nil
	}
	next := cloneRelayScopes(c.relayScopes)
	next[scope] = 1
	if err := c.replacer.Replace(ctx, c.openings(c.management, c.nodes, next)); err != nil {
		return fmt.Errorf("apply UDP relay firewall opening: %w", err)
	}
	c.relayScopes = next
	return nil
}

func (c *FirewallCoordinator) Close(ctx context.Context, endpoint proxy.BindEndpoint) error {
	normalized, err := normalizeFirewallEndpoints([]proxy.BindEndpoint{endpoint}, proxy.BindUDP, false)
	if err != nil {
		return fmt.Errorf("validate UDP relay firewall endpoint: %w", err)
	}
	scope := relayScopeOf(normalized[0])
	c.mu.Lock()
	defer c.mu.Unlock()
	count := c.relayScopes[scope]
	if count == 0 {
		return nil
	}
	if count > 1 {
		c.relayScopes[scope]--
		return nil
	}
	next := cloneRelayScopes(c.relayScopes)
	delete(next, scope)
	if err := c.replacer.Replace(ctx, c.openings(c.management, c.nodes, next)); err != nil {
		return fmt.Errorf("close UDP relay firewall opening: %w", err)
	}
	c.relayScopes = next
	return nil
}

func (c *FirewallCoordinator) State() []firewall.Opening {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.openings(c.management, c.nodes, c.relayScopes)
}

func (c *FirewallCoordinator) openings(management []proxy.BindEndpoint, nodes map[string][]proxy.BindEndpoint, relays map[relayScope]int) []firewall.Opening {
	result := make([]firewall.Opening, 0, len(management)+len(relays))
	for _, endpoint := range management {
		result = append(result, firewallOpening(endpoint, "management"))
	}
	nodeIDs := make([]string, 0, len(nodes))
	for id := range nodes {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)
	for _, id := range nodeIDs {
		for _, endpoint := range nodes[id] {
			result = append(result, firewallOpening(endpoint, "node:"+id))
		}
	}
	for scope := range relays {
		family := firewall.FamilyIPv4
		if scope.family == proxy.BindIPv6 {
			family = firewall.FamilyIPv6
		}
		result = append(result, firewall.Opening{
			Protocol: firewall.ProtocolUDP,
			Family:   family,
			Address:  scope.address,
			Port:     c.relayPortMin,
			PortEnd:  c.relayPortMax,
			Purpose:  "udp-relay",
		})
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i], result[j]
		if left.Family != right.Family {
			return left.Family < right.Family
		}
		if left.Protocol != right.Protocol {
			return left.Protocol < right.Protocol
		}
		if left.Address != right.Address {
			return left.Address.Compare(right.Address) < 0
		}
		if left.Port != right.Port {
			return left.Port < right.Port
		}
		if left.PortEnd != right.PortEnd {
			return left.PortEnd < right.PortEnd
		}
		return left.Purpose < right.Purpose
	})
	return result
}

func normalizeFirewallEndpoints(endpoints []proxy.BindEndpoint, protocol proxy.BindProtocol, allowEmpty bool) ([]proxy.BindEndpoint, error) {
	if len(endpoints) == 0 && !allowEmpty {
		return nil, errors.New("at least one firewall endpoint is required")
	}
	result := make([]proxy.BindEndpoint, 0, len(endpoints))
	seen := make(map[proxy.BindEndpoint]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint.Protocol != protocol {
			return nil, fmt.Errorf("endpoint protocol %q must be %q", endpoint.Protocol, protocol)
		}
		if endpoint.Family != proxy.BindIPv4 && endpoint.Family != proxy.BindIPv6 {
			return nil, fmt.Errorf("unsupported endpoint family %q", endpoint.Family)
		}
		if endpoint.Port == 0 {
			return nil, errors.New("firewall endpoint port must be non-zero")
		}
		if endpoint.Address.IsValid() {
			endpoint.Address = endpoint.Address.Unmap()
			if endpoint.Family == proxy.BindIPv4 && !endpoint.Address.Is4() {
				return nil, errors.New("firewall endpoint address does not match IPv4 family")
			}
			if endpoint.Family == proxy.BindIPv6 && (!endpoint.Address.Is6() || endpoint.Address.Is4In6()) {
				return nil, errors.New("firewall endpoint address does not match IPv6 family")
			}
		}
		if _, exists := seen[endpoint]; exists {
			return nil, errors.New("duplicate firewall endpoint")
		}
		seen[endpoint] = struct{}{}
		result = append(result, endpoint)
	}
	sort.Slice(result, func(i, j int) bool { return compareBindEndpoint(result[i], result[j]) < 0 })
	return result, nil
}

func firewallOpening(endpoint proxy.BindEndpoint, purpose string) firewall.Opening {
	protocol := firewall.ProtocolTCP
	if endpoint.Protocol == proxy.BindUDP {
		protocol = firewall.ProtocolUDP
	}
	family := firewall.FamilyIPv4
	if endpoint.Family == proxy.BindIPv6 {
		family = firewall.FamilyIPv6
	}
	return firewall.Opening{
		Protocol: protocol, Family: family, Address: endpoint.Address,
		Port: endpoint.Port, Purpose: purpose,
	}
}

func compareBindEndpoint(left, right proxy.BindEndpoint) int {
	if left.Family != right.Family {
		return strings.Compare(string(left.Family), string(right.Family))
	}
	if left.Protocol != right.Protocol {
		return strings.Compare(string(left.Protocol), string(right.Protocol))
	}
	if left.Address != right.Address {
		return left.Address.Compare(right.Address)
	}
	if left.Port < right.Port {
		return -1
	}
	if left.Port > right.Port {
		return 1
	}
	if left.Interface != right.Interface {
		return strings.Compare(left.Interface, right.Interface)
	}
	if left.Freebind == right.Freebind {
		return 0
	}
	if !left.Freebind {
		return -1
	}
	return 1
}

func sameEndpointSet(left, right []proxy.BindEndpoint) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cloneNodeEndpoints(source map[string][]proxy.BindEndpoint) map[string][]proxy.BindEndpoint {
	result := make(map[string][]proxy.BindEndpoint, len(source))
	for id, endpoints := range source {
		result[id] = append([]proxy.BindEndpoint(nil), endpoints...)
	}
	return result
}

func cloneRelayScopes(source map[relayScope]int) map[relayScope]int {
	result := make(map[relayScope]int, len(source))
	for scope, count := range source {
		result[scope] = count
	}
	return result
}

var _ NodeFirewall = (*FirewallCoordinator)(nil)
var _ proxy.UDPRelayFirewall = (*FirewallCoordinator)(nil)
