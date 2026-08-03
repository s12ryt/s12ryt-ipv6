//go:build !linux

package admin

import (
	"errors"
	"io"
)

func AcquireServiceLock(string) (io.Closer, error) {
	return nil, errors.New("service file locks are only supported on Linux")
}
