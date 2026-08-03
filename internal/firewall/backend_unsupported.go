//go:build !linux

package firewall

import "errors"

func NewNftBackend() (Backend, error) {
	return nil, errors.New("nftables management is only supported on Linux")
}
