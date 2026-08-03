//go:build !linux

package network

import "errors"

func NewLinuxKernel() (Kernel, error) {
	return nil, errors.New("Linux network management is only supported on Linux")
}
