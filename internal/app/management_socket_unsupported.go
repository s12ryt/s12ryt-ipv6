//go:build !linux

package app

import (
	"context"
	"errors"
	"net"
)

func ListenManagementSocket(context.Context, string, string) (net.Listener, error) {
	return nil, errors.New("management service is supported only on Linux")
}
