//go:build linux

package proxy

import (
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

func makeSocketControl(interfaceName string, freebind bool) func(string, string, syscall.RawConn) error {
	if interfaceName == "" && !freebind {
		return nil
	}
	return func(_, _ string, raw syscall.RawConn) error {
		var optionErrors []error
		controlErr := raw.Control(func(fd uintptr) {
			if interfaceName != "" {
				if err := unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, interfaceName); err != nil {
					optionErrors = append(optionErrors, err)
				}
			}
			if freebind {
				if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_FREEBIND, 1); err != nil {
					optionErrors = append(optionErrors, err)
				}
			}
		})
		return errors.Join(controlErr, errors.Join(optionErrors...))
	}
}
