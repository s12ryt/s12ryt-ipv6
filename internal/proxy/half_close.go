package proxy

import "net"

// closeWriter is implemented by connections that support half-closing the
// write side, such as *net.TCPConn.
type closeWriter interface {
	CloseWrite() error
}

// forwardCloseWrite half-closes the write side of conn when the underlying
// transport supports it.
//
// Wrappers that embed the net.Conn interface (bufferedConn, leasedConn) do not
// promote CloseWrite, because net.Conn does not declare it. relayConnections
// type-asserts the destination for CloseWrite to signal EOF to the peer, so
// those wrappers must forward the call explicitly; otherwise a half-close is
// silently dropped and a peer waiting for EOF can hang until the tunnel idle
// timeout (which defaults to 0, i.e. disabled) or context cancellation.
//
// Transports without half-close support are a no-op so callers can always
// invoke this safely.
func forwardCloseWrite(conn net.Conn) error {
	if writer, ok := conn.(closeWriter); ok {
		return writer.CloseWrite()
	}
	return nil
}

// CloseWrite forwards the half-close to the wrapped client connection.
func (c *bufferedConn) CloseWrite() error {
	return forwardCloseWrite(c.Conn)
}

// CloseWrite forwards the half-close to the wrapped upstream connection.
func (c *leasedConn) CloseWrite() error {
	return forwardCloseWrite(c.Conn)
}
