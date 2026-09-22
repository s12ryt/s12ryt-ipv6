package proxy

import (
	"bufio"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// TestBufferedConnSupportsCloseWrite guards the mixed proxy path: relayConnections
// type-asserts the client connection against CloseWrite to forward a half-close,
// but bufferedConn embeds the net.Conn interface (which has no CloseWrite method),
// so the wrapper must forward the call explicitly.
func TestBufferedConnSupportsCloseWrite(t *testing.T) {
	client, peer := newTCPConnPair(t)
	defer peer.Close()

	wrapped := &bufferedConn{Conn: client, reader: bufio.NewReader(client)}
	closer, ok := any(wrapped).(interface{ CloseWrite() error })
	if !ok {
		t.Fatal("bufferedConn must implement CloseWrite so the mixed proxy can half-close the client")
	}
	if err := closer.CloseWrite(); err != nil {
		t.Fatalf("bufferedConn.CloseWrite() error = %v", err)
	}
	assertPeerEOF(t, peer)
}

// TestLeasedConnSupportsCloseWrite guards the upstream path: every proxy protocol
// relays through a leasedConn returned by Dial, so a missing CloseWrite means the
// client's FIN is never forwarded to the upstream.
func TestLeasedConnSupportsCloseWrite(t *testing.T) {
	client, peer := newTCPConnPair(t)
	defer peer.Close()

	wrapped := &leasedConn{Conn: client}
	closer, ok := any(wrapped).(interface{ CloseWrite() error })
	if !ok {
		t.Fatal("leasedConn must implement CloseWrite so relayConnections can half-close the upstream")
	}
	if err := closer.CloseWrite(); err != nil {
		t.Fatalf("leasedConn.CloseWrite() error = %v", err)
	}
	assertPeerEOF(t, peer)
}

func assertPeerEOF(t *testing.T, peer net.Conn) {
	t.Helper()
	if err := peer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1)
	read, err := peer.Read(buffer)
	if read != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("peer read after half-close = (%d, %v), want (0, EOF)", read, err)
	}
}
