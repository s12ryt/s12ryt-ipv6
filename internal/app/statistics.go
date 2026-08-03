package app

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/s12ryt/s12ryt-ipv6/internal/stats"
)

func LoadStatistics(path string) (*stats.Registry, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("statistics path is required")
	}
	snapshot, err := stats.Load(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return stats.NewRegistry(), nil
		}
		return nil, fmt.Errorf("load statistics: %w", err)
	}
	registry, err := stats.NewRegistryFromSnapshot(snapshot)
	if err != nil {
		return nil, fmt.Errorf("restore statistics: %w", err)
	}
	return registry, nil
}
