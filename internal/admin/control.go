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

const maxControlMessageBytes = 4 * 1024 * 1024

const maxControlRequestBytes = maxControlMessageBytes

type PasswordResetter interface {
	Reset(replacement string) (string, error)
}

type AgentControlHandler interface {
	HandleAgent(context.Context, json.RawMessage) (json.RawMessage, error)
}

type ControlResponse struct {
	OK       bool            `json:"ok"`
	Password string          `json:"password,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    string          `json:"error,omitempty"`
}

type ControlServer struct {
	resetter        PasswordResetter
	agent           AgentControlHandler
	readTimeout     time.Duration
	maxAgentTimeout time.Duration
}

func NewControlServer(resetter PasswordResetter, timeout time.Duration) (*ControlServer, error) {
	return newControlServer(resetter, nil, timeout, timeout)
}

func NewControlServerWithAgent(resetter PasswordResetter, agent AgentControlHandler, timeout time.Duration) (*ControlServer, error) {
	if agent == nil {
		return nil, errors.New("agent control handler is required")
	}
	return newControlServer(resetter, agent, timeout, timeout)
}

func NewControlServerWithAgentLimits(
	resetter PasswordResetter,
	agent AgentControlHandler,
	readTimeout time.Duration,
	maxAgentTimeout time.Duration,
) (*ControlServer, error) {
	if agent == nil {
		return nil, errors.New("agent control handler is required")
	}
	return newControlServer(resetter, agent, readTimeout, maxAgentTimeout)
}

func newControlServer(
	resetter PasswordResetter,
	agent AgentControlHandler,
	readTimeout time.Duration,
	maxAgentTimeout time.Duration,
) (*ControlServer, error) {
	if resetter == nil {
		return nil, errors.New("password resetter is required")
	}
	if readTimeout <= 0 {
		return nil, errors.New("control timeout must be positive")
	}
	if maxAgentTimeout <= 0 {
		return nil, errors.New("maximum agent timeout must be positive")
	}
	if maxAgentTimeout < readTimeout {
		return nil, errors.New("maximum agent timeout must not be shorter than control timeout")
	}
	return &ControlServer{
		resetter: resetter, agent: agent, readTimeout: readTimeout, maxAgentTimeout: maxAgentTimeout,
	}, nil
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
		_ = s.handleConn(ctx, connection)
	}
}

func (s *ControlServer) HandleConn(connection net.Conn) error {
	return s.handleConn(context.Background(), connection)
}

func (s *ControlServer) handleConn(ctx context.Context, connection net.Conn) error {
	if connection == nil {
		return errors.New("control connection is required")
	}
	defer connection.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-done:
		}
	}()
	if err := connection.SetDeadline(time.Now().Add(s.readTimeout)); err != nil {
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
		Action      string          `json:"action"`
		NewPassword *string         `json:"new_password,omitempty"`
		Request     json.RawMessage `json:"request,omitempty"`
		TimeoutMS   *int64          `json:"timeout_ms,omitempty"`
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
	switch request.Action {
	case "reset_password":
		if request.Request != nil || request.TimeoutMS != nil {
			return s.reject(connection, "invalid control request", errors.New("reset password request contains agent data"))
		}
		replacement := ""
		if request.NewPassword != nil {
			replacement = *request.NewPassword
		}
		password, err := s.resetter.Reset(replacement)
		if err != nil {
			return s.reject(connection, "password reset failed", fmt.Errorf("reset password: %w", err))
		}
		return writeControlResponse(connection, ControlResponse{OK: true, Password: password})
	case "agent":
		if s.agent == nil {
			return s.reject(connection, "unsupported control action", errors.New("agent control handler is unavailable"))
		}
		if request.NewPassword != nil || len(request.Request) == 0 || !json.Valid(request.Request) {
			return s.reject(connection, "invalid control request", errors.New("invalid agent control request"))
		}
		requestTimeout := s.readTimeout
		if request.TimeoutMS != nil {
			maxMilliseconds := int64(s.maxAgentTimeout / time.Millisecond)
			if *request.TimeoutMS <= 0 || *request.TimeoutMS > maxMilliseconds {
				return s.reject(connection, "invalid control request", errors.New("invalid agent timeout"))
			}
			requestTimeout = time.Duration(*request.TimeoutMS) * time.Millisecond
		}
		if err := connection.SetDeadline(time.Now().Add(requestTimeout)); err != nil {
			return s.reject(connection, "agent request failed", fmt.Errorf("set agent deadline: %w", err))
		}
		requestContext, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()
		result, err := s.agent.HandleAgent(requestContext, request.Request)
		if err != nil {
			return s.reject(connection, "agent request failed", fmt.Errorf("handle agent request: %w", err))
		}
		if len(result) == 0 || !json.Valid(result) {
			return s.reject(connection, "agent request failed", errors.New("agent handler returned invalid JSON"))
		}
		return writeControlResponse(connection, ControlResponse{OK: true, Result: result})
	default:
		return s.reject(connection, "unsupported control action", errors.New("unsupported control action"))
	}
}

func (s *ControlServer) reject(connection net.Conn, message string, cause error) error {
	if err := writeControlResponse(connection, ControlResponse{Error: message}); err != nil {
		return errors.Join(cause, fmt.Errorf("encode control error: %w", err))
	}
	return cause
}

func writeControlResponse(connection io.Writer, response ControlResponse) error {
	encoded, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode control response: %w", err)
	}
	if len(encoded)+1 > maxControlMessageBytes {
		return errors.New("control response exceeds size limit")
	}
	encoded = append(encoded, '\n')
	if _, err := connection.Write(encoded); err != nil {
		return fmt.Errorf("write control response: %w", err)
	}
	return nil
}
