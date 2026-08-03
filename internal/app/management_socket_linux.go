//go:build linux

package app

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func ListenManagementSocket(ctx context.Context, network, address string) (net.Listener, error) {
	config := net.ListenConfig{}
	if network == "tcp6" {
		config.Control = func(_, _ string, raw syscall.RawConn) error {
			var socketErr error
			if err := raw.Control(func(fileDescriptor uintptr) {
				socketErr = unix.SetsockoptInt(int(fileDescriptor), unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, 1)
			}); err != nil {
				return err
			}
			return socketErr
		}
	}
	return config.Listen(ctx, network, address)
}
