//go:build !linux

package admin

import (
	"errors"
	"net"
)

func ListenControlSocket(string) (net.Listener, error) {
	return nil, errors.New("Unix control sockets are only supported on Linux")
}
