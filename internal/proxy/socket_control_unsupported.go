//go:build !linux

package proxy

import (
	"errors"
	"syscall"
)

func makeSocketControl(interfaceName string, freebind bool) func(string, string, syscall.RawConn) error {
	if interfaceName == "" && !freebind {
		return nil
	}
	return func(string, string, syscall.RawConn) error {
		return errors.New("socket interface binding and IPv6 freebind require Linux")
	}
}
