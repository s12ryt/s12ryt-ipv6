//go:build linux

package admin

import (
	"context"
	"net"
)

func DialControlSocket(ctx context.Context, path string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", path)
}
