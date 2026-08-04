//go:build !linux

package network

import "errors"

func NewLinuxNetworkDiscovery() (NetworkDiscovery, error) {
	return nil, errors.New("Linux network discovery is only supported on Linux")
}
