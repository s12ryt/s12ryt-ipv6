package admin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const maxControlRequestBytes = 4 * 1024

type PasswordResetter interface {
	Reset(replacement string) (string, error)
}

type ControlResponse struct {
	OK       bool   `json:"ok"`
	Password string `json:"password,omitempty"`
	Error    string `json:"error,omitempty"`
}

type ControlServer struct {
	resetter PasswordResetter
	timeout  time.Duration
}

func NewControlServer(resetter PasswordResetter, timeout time.Duration) (*ControlServer, error) {
	if resetter == nil {
		return nil, errors.New("password resetter is required")
	}
	if timeout <= 0 {
		return nil, errors.New("control timeout must be positive")
	}
	return &ControlServer{resetter: resetter, timeout: timeout}, nil
}

func (s *ControlServer) Serve(ctx context.Context, listener net.Listener) error {
	if listener == nil {
		return errors.New("control listener is required")
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-done:
		}
	}()
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept control connection: %w", err)
		}
		_ = s.HandleConn(connection)
	}
}

func (s *ControlServer) HandleConn(connection net.Conn) error {
	if connection == nil {
		return errors.New("control connection is required")
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(s.timeout)); err != nil {
		return fmt.Errorf("set control deadline: %w", err)
	}
	line, err := bufio.NewReader(io.LimitReader(connection, maxControlRequestBytes+1)).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return s.reject(connection, "invalid control request", fmt.Errorf("read control request: %w", err))
	}
	if len(line) == 0 || len(line) > maxControlRequestBytes {
		return s.reject(connection, "invalid control request", errors.New("control request exceeds size limit"))
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	var request struct {
		Action      string `json:"action"`
		NewPassword string `json:"new_password"`
	}
	if err := decoder.Decode(&request); err != nil {
		return s.reject(connection, "invalid control request", fmt.Errorf("decode control request: %w", err))
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values are not allowed")
		}
		return s.reject(connection, "invalid control request", fmt.Errorf("decode trailing control data: %w", err))
	}
	if request.Action != "reset_password" {
		return s.reject(connection, "unsupported control action", errors.New("unsupported control action"))
	}
	password, err := s.resetter.Reset(request.NewPassword)
	if err != nil {
		return s.reject(connection, "password reset failed", fmt.Errorf("reset password: %w", err))
	}
	if err := json.NewEncoder(connection).Encode(ControlResponse{OK: true, Password: password}); err != nil {
		return fmt.Errorf("encode control response: %w", err)
	}
	return nil
}

func (s *ControlServer) reject(connection net.Conn, message string, cause error) error {
	if err := json.NewEncoder(connection).Encode(ControlResponse{Error: message}); err != nil {
		return errors.Join(cause, fmt.Errorf("encode control error: %w", err))
	}
	return cause
}
