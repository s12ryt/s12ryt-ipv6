package node

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/policy"
	"github.com/s12ryt/s12ryt-ipv6/internal/proxy"
	"github.com/s12ryt/s12ryt-ipv6/internal/secret"
	"gopkg.in/yaml.v3"
)

const nodeStateSchemaVersion = 1

type State struct {
	Nodes []Node
}

func cloneState(state State) State {
	cloned := State{Nodes: make([]Node, len(state.Nodes))}
	for index, current := range state.Nodes {
		cloned.Nodes[index] = Node{Config: cloneConfig(current.Config), Status: current.Status}
	}
	return cloned
}

type nodeStateDocument struct {
	SchemaVersion int               `yaml:"schema_version"`
	Nodes         []nodeStateRecord `yaml:"nodes"`
}

type nodeStateRecord struct {
	ID                string             `yaml:"id"`
	Name              string             `yaml:"name"`
	Folder            string             `yaml:"folder,omitempty"`
	Protocol          string             `yaml:"protocol"`
	Status            string             `yaml:"status"`
	UsernameEncrypted string             `yaml:"username_encrypted,omitempty"`
	PasswordEncrypted string             `yaml:"password_encrypted,omitempty"`
	MaxTCP            int                `yaml:"max_tcp"`
	MaxUDP            int                `yaml:"max_udp"`
	DialTimeout       string             `yaml:"dial_timeout"`
	HandshakeTimeout  string             `yaml:"handshake_timeout"`
	TunnelIdleTimeout string             `yaml:"tunnel_idle_timeout"`
	UDPIdleTimeout    string             `yaml:"udp_idle_timeout"`
	ULAOverride       string             `yaml:"ula_override"`
	Outbound          string             `yaml:"outbound"`
	DedicatedPool     string             `yaml:"dedicated_pool,omitempty"`
	Port              uint16             `yaml:"port"`
	InboundMode       string             `yaml:"inbound_mode,omitempty"`
	InboundResource   string             `yaml:"inbound_resource,omitempty"`
	Inbound           []nodeBindDocument `yaml:"inbound,omitempty"`
}

type nodeBindDocument struct {
	Protocol  string `yaml:"protocol"`
	Family    string `yaml:"family"`
	Address   string `yaml:"address,omitempty"`
	Interface string `yaml:"interface,omitempty"`
	Freebind  bool   `yaml:"freebind"`
}

type FileStateStore struct {
	path  string
	vault *secret.Vault
	mu    sync.Mutex
}

func NewFileStateStore(path string, vault *secret.Vault) (*FileStateStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("node state path is required")
	}
	if vault == nil {
		return nil, errors.New("node credential vault is required")
	}
	return &FileStateStore{path: path, vault: vault}, nil
}

func (s *FileStateStore) Load() (State, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, fmt.Errorf("open node state: %w", err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var document nodeStateDocument
	if err := decoder.Decode(&document); err != nil {
		return State{}, false, fmt.Errorf("decode node state: %w", err)
	}
	if document.SchemaVersion != nodeStateSchemaVersion {
		return State{}, false, fmt.Errorf("unsupported node state schema version %d", document.SchemaVersion)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple YAML documents are not allowed")
		}
		return State{}, false, fmt.Errorf("decode node state trailer: %w", err)
	}

	state, err := s.decode(document)
	if err != nil {
		return State{}, false, err
	}
	return state, true, nil
}

func (s *FileStateStore) Save(state State) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := normalizeState(state)
	if err != nil {
		return fmt.Errorf("validate node state: %w", err)
	}
	document, err := s.encode(state)
	if err != nil {
		return err
	}
	contents, err := yaml.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode node state: %w", err)
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create node state directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".nodes-*")
	if err != nil {
		return fmt.Errorf("create temporary node state: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set node state permissions: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write node state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync node state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close node state: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace node state: %w", err)
	}
	committed = true
	return nil
}

func (s *FileStateStore) encode(state State) (nodeStateDocument, error) {
	document := nodeStateDocument{SchemaVersion: nodeStateSchemaVersion, Nodes: make([]nodeStateRecord, 0, len(state.Nodes))}
	for _, current := range state.Nodes {
		record := recordFromNode(current)
		if current.Config.Username != "" {
			username, err := s.vault.Encrypt([]byte(current.Config.Username))
			if err != nil {
				return nodeStateDocument{}, fmt.Errorf("encrypt node %q username: %w", current.Config.ID, err)
			}
			password, err := s.vault.Encrypt([]byte(current.Config.Password))
			if err != nil {
				return nodeStateDocument{}, fmt.Errorf("encrypt node %q password: %w", current.Config.ID, err)
			}
			record.UsernameEncrypted = username
			record.PasswordEncrypted = password
		}
		document.Nodes = append(document.Nodes, record)
	}
	return document, nil
}

func (s *FileStateStore) decode(document nodeStateDocument) (State, error) {
	state := State{Nodes: make([]Node, 0, len(document.Nodes))}
	for _, record := range document.Nodes {
		if (record.UsernameEncrypted == "") != (record.PasswordEncrypted == "") {
			return State{}, fmt.Errorf("node %q encrypted credentials are incomplete", record.ID)
		}
		username, password := "", ""
		if record.UsernameEncrypted != "" {
			plainUsername, err := s.vault.Decrypt(record.UsernameEncrypted)
			if err != nil {
				return State{}, fmt.Errorf("decrypt node %q username: %w", record.ID, err)
			}
			plainPassword, err := s.vault.Decrypt(record.PasswordEncrypted)
			if err != nil {
				return State{}, fmt.Errorf("decrypt node %q password: %w", record.ID, err)
			}
			username, password = string(plainUsername), string(plainPassword)
		}
		current, err := record.toNode(username, password)
		if err != nil {
			return State{}, fmt.Errorf("decode node %q: %w", record.ID, err)
		}
		state.Nodes = append(state.Nodes, current)
	}
	normalized, err := normalizeState(state)
	if err != nil {
		return State{}, fmt.Errorf("validate node state: %w", err)
	}
	return normalized, nil
}

func recordFromNode(current Node) nodeStateRecord {
	config := current.Config
	record := nodeStateRecord{
		ID: config.ID, Name: config.Name, Folder: config.Folder, Protocol: string(config.Protocol), Status: string(current.Status),
		MaxTCP: config.MaxTCP, MaxUDP: config.MaxUDP, DialTimeout: config.DialTimeout.String(),
		HandshakeTimeout: config.HandshakeTimeout.String(), TunnelIdleTimeout: config.TunnelIdleTimeout.String(),
		UDPIdleTimeout: config.UDPIdleTimeout.String(), ULAOverride: string(config.ULAOverride), Outbound: config.Outbound,
		DedicatedPool: config.DedicatedPool, Port: config.Port,
		InboundMode: string(config.InboundMode), InboundResource: config.InboundResource,
	}
	if config.InboundMode == "" {
		record.Inbound = make([]nodeBindDocument, 0, len(config.Inbound))
	}
	for _, inbound := range config.Inbound {
		if config.InboundMode != "" {
			break
		}
		address := ""
		if inbound.Address.IsValid() {
			address = inbound.Address.String()
		}
		record.Inbound = append(record.Inbound, nodeBindDocument{
			Protocol: string(inbound.Protocol), Family: string(inbound.Family), Address: address,
			Interface: inbound.Interface, Freebind: inbound.Freebind,
		})
	}
	return record
}

func (r nodeStateRecord) toNode(username, password string) (Node, error) {
	dial, err := parseNodeDuration("dial", r.DialTimeout)
	if err != nil {
		return Node{}, err
	}
	handshake, err := parseNodeDuration("handshake", r.HandshakeTimeout)
	if err != nil {
		return Node{}, err
	}
	tunnelIdle, err := parseNodeDuration("tunnel idle", r.TunnelIdleTimeout)
	if err != nil {
		return Node{}, err
	}
	udpIdle, err := parseNodeDuration("UDP idle", r.UDPIdleTimeout)
	if err != nil {
		return Node{}, err
	}
	inbound := make([]proxy.BindSpec, 0, len(r.Inbound))
	for index, persisted := range r.Inbound {
		var address netip.Addr
		if persisted.Address != "" {
			parsed, err := netip.ParseAddr(persisted.Address)
			if err != nil || parsed.Zone() != "" {
				return Node{}, fmt.Errorf("inbound %d address is invalid", index)
			}
			address = parsed.Unmap()
		}
		inbound = append(inbound, proxy.BindSpec{
			Protocol: proxy.BindProtocol(persisted.Protocol), Family: proxy.BindFamily(persisted.Family),
			Address: address, Interface: persisted.Interface, Freebind: persisted.Freebind,
		})
	}
	return Node{Config: Config{
		ID: r.ID, Name: r.Name, Folder: r.Folder, Protocol: Protocol(r.Protocol), Username: username, Password: password,
		MaxTCP: r.MaxTCP, MaxUDP: r.MaxUDP, DialTimeout: dial, HandshakeTimeout: handshake,
		TunnelIdleTimeout: tunnelIdle, UDPIdleTimeout: udpIdle, ULAOverride: policy.ULAOverride(r.ULAOverride),
		Outbound: r.Outbound, DedicatedPool: r.DedicatedPool, Port: r.Port,
		InboundMode: InboundMode(r.InboundMode), InboundResource: r.InboundResource, Inbound: inbound,
	}, Status: Status(r.Status)}, nil
}

func parseNodeDuration(name, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s timeout: %w", name, err)
	}
	return duration, nil
}

func normalizeState(state State) (State, error) {
	normalized := State{Nodes: make([]Node, len(state.Nodes))}
	seen := make(map[string]struct{}, len(state.Nodes))
	for index, current := range state.Nodes {
		folder, err := NormalizeFolderName(current.Config.Folder)
		if err != nil {
			return State{}, fmt.Errorf("node %q folder: %w", current.Config.ID, err)
		}
		current.Config.Folder = folder
		if current.Status != StatusRunning && current.Status != StatusStopped {
			return State{}, fmt.Errorf("node %q has invalid status %q", current.Config.ID, current.Status)
		}
		if err := current.Config.Validate(); err != nil {
			return State{}, fmt.Errorf("node %q config: %w", current.Config.ID, err)
		}
		if _, exists := seen[current.Config.ID]; exists {
			return State{}, fmt.Errorf("duplicate node ID %q", current.Config.ID)
		}
		seen[current.Config.ID] = struct{}{}
		normalized.Nodes[index] = Node{Config: cloneConfig(current.Config), Status: current.Status}
	}
	sort.Slice(normalized.Nodes, func(i, j int) bool {
		return normalized.Nodes[i].Config.ID < normalized.Nodes[j].Config.ID
	})
	return normalized, nil
}
