package app

import (
	"errors"
	"path/filepath"
	"strings"
)

type DataPaths struct {
	Directory        string
	Configuration    string
	Resources        string
	Nodes            string
	NetworkOwnership string
	Statistics       string
	MasterKey        string
	AdminPassword    string
	EventLog         string
	ControlSocket    string
	ServiceLock      string
}

func NewDataPaths(directory string) (DataPaths, error) {
	if strings.TrimSpace(directory) == "" {
		return DataPaths{}, errors.New("data directory is required")
	}

	root := filepath.Clean(directory)
	return DataPaths{
		Directory:        root,
		Configuration:    filepath.Join(root, "config.yaml"),
		Resources:        filepath.Join(root, "resources.yaml"),
		Nodes:            filepath.Join(root, "nodes.yaml"),
		NetworkOwnership: filepath.Join(root, "network-ownership.yaml"),
		Statistics:       filepath.Join(root, "statistics.json"),
		MasterKey:        filepath.Join(root, "master.key"),
		AdminPassword:    filepath.Join(root, "admin-password.yaml"),
		EventLog:         filepath.Join(root, "events.jsonl"),
		ControlSocket:    filepath.Join(root, "control.sock"),
		ServiceLock:      filepath.Join(root, "service.lock"),
	}, nil
}
