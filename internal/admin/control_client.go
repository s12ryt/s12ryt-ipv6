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
		return "", fmt.Errorf("%w: %v", ErrControlUnavailable, err)
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
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return "", fmt.Errorf("write control request: %w", err)
	}
	line, err := bufio.NewReader(io.LimitReader(connection, maxControlRequestBytes+1)).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read control response: %w", err)
	}
	if len(line) == 0 || len(line) > maxControlRequestBytes {
		return "", errors.New("invalid control response size")
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	var response ControlResponse
	if err := decoder.Decode(&response); err != nil {
		return "", fmt.Errorf("decode control response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values are not allowed")
		}
		return "", fmt.Errorf("decode trailing control response: %w", err)
	}
	if !response.OK {
		if response.Error == "" || response.Password != "" {
			return "", errors.New("invalid control failure response")
		}
		return "", errors.New(response.Error)
	}
	if response.Error != "" || response.Password == "" {
		return "", errors.New("invalid control success response")
	}
	if err := secret.ValidateAdminPassword(response.Password); err != nil {
		return "", errors.New("control service returned an invalid password")
	}
	return response.Password, nil
}
