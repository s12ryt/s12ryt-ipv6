//go:build !linux

package admin

import (
	"context"
	"errors"
	"net"
)

func DialControlSocket(context.Context, string) (net.Conn, error) {
	return nil, errors.New("Unix control sockets are only supported on Linux")
}
