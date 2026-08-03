package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

type ManagementListenFunc func(context.Context, string, string) (net.Listener, error)

type ManagementHTTPOptions struct {
	Handler         http.Handler
	Listen          ManagementListenFunc
	ShutdownTimeout time.Duration
}

type ManagementHTTP struct {
	handler         http.Handler
	listen          ManagementListenFunc
	shutdownTimeout time.Duration
}

func NewManagementHTTP(options ManagementHTTPOptions) (*ManagementHTTP, error) {
	if options.Handler == nil {
		return nil, errors.New("management HTTP handler is required")
	}
	if options.Listen == nil {
		return nil, errors.New("management listen function is required")
	}
	if options.ShutdownTimeout <= 0 {
		return nil, errors.New("management shutdown timeout must be positive")
	}
	return &ManagementHTTP{
		handler: options.Handler, listen: options.Listen, shutdownTimeout: options.ShutdownTimeout,
	}, nil
}

func (m *ManagementHTTP) Listen(ctx context.Context, port uint16) ([]net.Listener, error) {
	if port == 0 {
		return nil, errors.New("management port must be non-zero")
	}
	portText := strconv.FormatUint(uint64(port), 10)
	ipv4, err := m.listen(ctx, "tcp4", net.JoinHostPort("0.0.0.0", portText))
	if err != nil {
		return nil, fmt.Errorf("listen on IPv4 management address: %w", err)
	}
	ipv6, err := m.listen(ctx, "tcp6", net.JoinHostPort("::", portText))
	if err != nil {
		_ = ipv4.Close()
		return nil, fmt.Errorf("listen on IPv6 management address: %w", err)
	}
	return []net.Listener{ipv4, ipv6}, nil
}

func (m *ManagementHTTP) Serve(ctx context.Context, listeners []net.Listener) error {
	if len(listeners) == 0 {
		return errors.New("management listeners are required")
	}
	server := &http.Server{
		Handler:           m.handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	results := make(chan error, len(listeners))
	for _, listener := range listeners {
		if listener == nil {
			closeListeners(listeners)
			return errors.New("management listener is nil")
		}
		go func(current net.Listener) {
			results <- server.Serve(current)
		}(listener)
	}

	var serveErr error
	select {
	case <-ctx.Done():
	case err := <-results:
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			serveErr = err
		} else if ctx.Err() == nil {
			serveErr = errors.New("management HTTP listener stopped unexpectedly")
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.shutdownTimeout)
	defer cancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	closeListeners(listeners)
	return errors.Join(serveErr, shutdownErr)
}
