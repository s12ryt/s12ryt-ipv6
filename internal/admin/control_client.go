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
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/secret"
)

var ErrControlUnavailable = errors.New("control service is unavailable")

type ControlDialFunc func(context.Context, string) (net.Conn, error)

type ControlClientOptions struct {
	Dial    ControlDialFunc
	Timeout time.Duration
}

type ControlClient struct {
	dial    ControlDialFunc
	timeout time.Duration
}

func NewControlClient(options ControlClientOptions) (*ControlClient, error) {
	if options.Dial == nil {
		return nil, errors.New("control dialer is required")
	}
	if options.Timeout <= 0 {
		return nil, errors.New("control timeout must be positive")
	}
	return &ControlClient{dial: options.Dial, timeout: options.Timeout}, nil
}

func (c *ControlClient) ResetPassword(ctx context.Context, path, replacement string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("control socket path is required")
	}
	if replacement != "" {
		if err := secret.ValidateAdminPassword(replacement); err != nil {
			return "", err
		}
	}
	connection, err := c.dial(ctx, path)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrControlUnavailable, err)
	}
	defer connection.Close()
	deadline := time.Now().Add(c.timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return "", fmt.Errorf("set control deadline: %w", err)
	}
	request := struct {
		Action      string `json:"action"`
		NewPassword string `json:"new_password"`
	}{Action: "reset_password", NewPassword: replacement}
	if err := writeControlRequest(connection, request); err != nil {
		return "", fmt.Errorf("write control request: %w", err)
	}
	response, err := readControlResponse(connection)
	if err != nil {
		return "", err
	}
	if !response.OK {
		if response.Error == "" || response.Password != "" || len(response.Result) != 0 {
			return "", errors.New("invalid control failure response")
		}
		return "", errors.New(response.Error)
	}
	if response.Error != "" || response.Password == "" || len(response.Result) != 0 {
		return "", errors.New("invalid control success response")
	}
	if err := secret.ValidateAdminPassword(response.Password); err != nil {
		return "", errors.New("control service returned an invalid password")
	}
	return response.Password, nil
}

func (c *ControlClient) CallAgent(ctx context.Context, path string, request json.RawMessage) (json.RawMessage, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("control socket path is required")
	}
	if len(request) == 0 || !json.Valid(request) {
		return nil, errors.New("agent request must be one JSON value")
	}
	connection, err := c.dial(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrControlUnavailable, err)
	}
	defer connection.Close()
	deadline := time.Now().Add(c.timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set control deadline: %w", err)
	}
	envelope := struct {
		Action    string          `json:"action"`
		Request   json.RawMessage `json:"request"`
		TimeoutMS int64           `json:"timeout_ms"`
	}{Action: "agent", Request: request, TimeoutMS: controlTimeoutMilliseconds(time.Until(deadline))}
	if err := writeControlRequest(connection, envelope); err != nil {
		return nil, fmt.Errorf("write control request: %w", err)
	}
	response, err := readControlResponse(connection)
	if err != nil {
		return nil, err
	}
	if !response.OK {
		if response.Error == "" || response.Password != "" || len(response.Result) != 0 {
			return nil, errors.New("invalid control failure response")
		}
		return nil, errors.New(response.Error)
	}
	if response.Error != "" || response.Password != "" || len(response.Result) == 0 || !json.Valid(response.Result) {
		return nil, errors.New("invalid control success response")
	}
	return append(json.RawMessage(nil), response.Result...), nil
}

func controlTimeoutMilliseconds(timeout time.Duration) int64 {
	milliseconds := timeout / time.Millisecond
	if timeout%time.Millisecond != 0 {
		milliseconds++
	}
	if milliseconds < 1 {
		return 1
	}
	return int64(milliseconds)
}

func writeControlRequest(writer io.Writer, request any) error {
	encoded, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if len(encoded)+1 > maxControlMessageBytes {
		return errors.New("control request exceeds size limit")
	}
	encoded = append(encoded, '\n')
	_, err = writer.Write(encoded)
	return err
}

func readControlResponse(reader io.Reader) (ControlResponse, error) {
	line, err := bufio.NewReader(io.LimitReader(reader, maxControlMessageBytes+1)).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return ControlResponse{}, fmt.Errorf("read control response: %w", err)
	}
	if len(line) == 0 || len(line) > maxControlMessageBytes {
		return ControlResponse{}, errors.New("invalid control response size")
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	var response ControlResponse
	if err := decoder.Decode(&response); err != nil {
		return ControlResponse{}, fmt.Errorf("decode control response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values are not allowed")
		}
		return ControlResponse{}, fmt.Errorf("decode trailing control response: %w", err)
	}
	return response, nil
}
