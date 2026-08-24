package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/s12ryt/s12ryt-ipv6/internal/admin"
	"github.com/s12ryt/s12ryt-ipv6/internal/app"
	"gopkg.in/yaml.v3"
)

const agentCLIMaxDocumentBytes = 4 * 1024 * 1024

type agentCallFunc func(context.Context, string, json.RawMessage, time.Duration) (json.RawMessage, error)

type agentCLIResponse struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *agentCLIError  `json:"error,omitempty"`
}

type agentCLIError struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

type optionalBoolFlag struct {
	set   bool
	value bool
}

func (f *optionalBoolFlag) String() string {
	if !f.set {
		return ""
	}
	return strconv.FormatBool(f.value)
}

func (f *optionalBoolFlag) Set(raw string) error {
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return err
	}
	f.set = true
	f.value = value
	return nil
}

func runAgentCLI(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, call agentCallFunc) int {
	command, commandArgs, ok := resolveAgentCLICommand(args)
	if !ok {
		writeAgentCLIError(stdout, "invalid_usage", "invalid agent command")
		return 2
	}

	defaultTimeout := 30 * time.Second
	if command == "apply" {
		defaultTimeout = 10 * time.Minute
	}
	flags := flag.NewFlagSet("agent "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dataDirectory := flags.String("data-dir", defaultDataDirectory, "configuration and state directory")
	timeout := flags.Duration("timeout", defaultTimeout, "control request timeout")
	showSecrets := flags.Bool("show-secrets", false, "include proxy credentials")
	yes := flags.Bool("yes", false, "confirm destructive operation")

	var format string
	var file string
	var prune bool
	var dryRun bool
	var name string
	var batch string
	var id string
	var folder string
	var source string
	var target string
	var prefix string
	var kind string
	var nodeID string
	var action string
	var limit int
	var success optionalBoolFlag

	switch command {
	case "export":
		flags.StringVar(&format, "format", "", "json or yaml")
	case "apply":
		flags.StringVar(&format, "format", "", "json or yaml")
		flags.StringVar(&file, "file", "-", "input file or - for stdin")
		flags.BoolVar(&prune, "prune", false, "remove omitted objects from explicit sections")
		flags.BoolVar(&dryRun, "dry-run", false, "validate without modifying state")
	case "resources.template.create", "resources.fixed.create", "resources.pool.create",
		"nodes.create", "nodes.update", "nodes.batch-create", "network.resolvers.replace":
		flags.StringVar(&file, "file", "-", "JSON input file or - for stdin")
	case "resources.template.delete", "resources.fixed.delete", "resources.pool.delete", "resources.pool.refresh":
		flags.StringVar(&name, "name", "", "resource name")
	case "resources.pool.force-drain":
		flags.StringVar(&name, "name", "", "resource pool name")
		flags.StringVar(&batch, "batch", "", "drain batch ID")
	case "nodes.get", "nodes.delete", "nodes.start", "nodes.stop":
		flags.StringVar(&id, "id", "", "node ID")
	case "nodes.move":
		flags.StringVar(&id, "id", "", "node ID")
		flags.StringVar(&folder, "folder", "", "target folder")
	case "folders.rename":
		flags.StringVar(&source, "source", "", "source folder")
		flags.StringVar(&target, "target", "", "target folder")
	case "folders.start", "folders.stop", "folders.delete":
		flags.StringVar(&folder, "folder", "", "folder")
	case "network.nat64.set":
		flags.StringVar(&prefix, "prefix", "", "NAT64 /96 prefix")
	case "logs.tail":
		flags.StringVar(&kind, "kind", "", "log kind")
		flags.StringVar(&nodeID, "node", "", "node ID")
		flags.StringVar(&action, "action", "", "log action")
		flags.Var(&success, "success", "filter by success")
		flags.IntVar(&limit, "limit", 0, "maximum events")
	case "stats.reset":
		flags.StringVar(&nodeID, "node", "", "node ID")
	}

	if err := flags.Parse(commandArgs); err != nil || flags.NArg() != 0 || strings.TrimSpace(*dataDirectory) == "" || *timeout < time.Second || *timeout > 30*time.Minute {
		writeAgentCLIError(stdout, "invalid_usage", "invalid agent command options")
		return 2
	}

	request := map[string]any{"command": command}
	if *showSecrets {
		request["show_secrets"] = true
	}
	if *yes {
		request["yes"] = true
	}
	if prune {
		request["prune"] = true
	}
	if dryRun {
		request["dry_run"] = true
	}

	var arguments json.RawMessage
	var err error
	switch command {
	case "export":
		if !validAgentCLIFormat(format) {
			writeAgentCLIError(stdout, "invalid_usage", "export requires --format json or yaml")
			return 2
		}
	case "apply":
		if !validAgentCLIFormat(format) {
			writeAgentCLIError(stdout, "invalid_usage", "apply requires --format json or yaml")
			return 2
		}
		arguments, err = readAgentCLIDocument(stdin, file, format)
		if err == nil {
			request["input"] = arguments
		}
	case "resources.template.create", "resources.fixed.create", "resources.pool.create",
		"nodes.create", "nodes.update", "nodes.batch-create", "network.resolvers.replace":
		arguments, err = readAgentCLIDocument(stdin, file, "json")
	case "resources.template.delete", "resources.fixed.delete", "resources.pool.delete", "resources.pool.refresh":
		arguments, err = marshalAgentCLIArguments(map[string]any{"name": name}, name != "")
	case "resources.pool.force-drain":
		arguments, err = marshalAgentCLIArguments(map[string]any{"name": name, "batch": batch}, name != "" && batch != "")
	case "nodes.get", "nodes.delete", "nodes.start", "nodes.stop":
		arguments, err = marshalAgentCLIArguments(map[string]any{"id": id}, id != "")
	case "nodes.move":
		arguments, err = marshalAgentCLIArguments(map[string]any{"id": id, "folder": folder}, id != "")
	case "folders.rename":
		arguments, err = marshalAgentCLIArguments(map[string]any{"source": source, "target": target}, source != "" && target != "")
	case "folders.start", "folders.stop", "folders.delete":
		arguments, err = marshalAgentCLIArguments(map[string]any{"folder": folder}, folder != "")
	case "network.nat64.set":
		arguments, err = marshalAgentCLIArguments(map[string]any{"prefix": prefix}, prefix != "")
	case "logs.tail":
		values := map[string]any{}
		if kind != "" {
			values["kind"] = kind
		}
		if nodeID != "" {
			values["node"] = nodeID
		}
		if action != "" {
			values["action"] = action
		}
		if success.set {
			values["success"] = success.value
		}
		if limit != 0 {
			values["limit"] = limit
		}
		arguments, err = json.Marshal(values)
	case "stats.reset":
		values := map[string]any{}
		if nodeID != "" {
			values["node"] = nodeID
		}
		arguments, err = json.Marshal(values)
	}
	if err != nil {
		writeAgentCLIError(stdout, "invalid_usage", "invalid agent command input")
		return 2
	}
	if len(arguments) != 0 {
		request["arguments"] = arguments
	}

	payload, err := json.Marshal(request)
	if err != nil || len(payload) > agentCLIMaxDocumentBytes {
		writeAgentCLIError(stdout, "invalid_usage", "agent request exceeds size limit")
		return 2
	}
	paths, err := app.NewDataPaths(*dataDirectory)
	if err != nil {
		writeAgentCLIError(stdout, "invalid_usage", "invalid data directory")
		return 2
	}
	if call == nil {
		writeAgentCLIError(stdout, "internal_error", "agent control client is unavailable")
		return 1
	}
	responsePayload, err := call(ctx, paths.ControlSocket, payload, *timeout)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			writeAgentCLIError(stdout, "permission_denied", "control socket permission denied")
		} else {
			writeAgentCLIError(stdout, "unavailable", "control service is unavailable")
		}
		return 3
	}
	response, err := decodeAgentCLIResponse(responsePayload)
	if err != nil {
		writeAgentCLIError(stdout, "internal_error", "control service returned an invalid response")
		return 1
	}
	if !response.OK {
		writeAgentCLIRaw(stdout, responsePayload)
		return agentCLIExitCode(response.Error.Code)
	}
	if command == "schema" || command == "export" {
		if len(response.Data) == 0 {
			writeAgentCLIError(stdout, "internal_error", "control service returned an invalid response")
			return 1
		}
		if command == "export" && format == "yaml" {
			encoded, err := agentCLIJSONToYAML(response.Data)
			if err != nil {
				writeAgentCLIError(stdout, "internal_error", "control service returned an invalid document")
				return 1
			}
			_, _ = stdout.Write(encoded)
			return 0
		}
		writeAgentCLIRaw(stdout, response.Data)
		return 0
	}
	writeAgentCLIRaw(stdout, responsePayload)
	if command == "status" && agentCLIHealth(response.Data) != "healthy" {
		return 1
	}
	return 0
}

func callAgentControl(ctx context.Context, path string, request json.RawMessage, timeout time.Duration) (json.RawMessage, error) {
	client, err := admin.NewControlClient(admin.ControlClientOptions{Dial: admin.DialControlSocket, Timeout: timeout})
	if err != nil {
		return nil, err
	}
	return client.CallAgent(ctx, path, request)
}

func resolveAgentCLICommand(args []string) (string, []string, bool) {
	commands := [][]string{
		{"resources", "template", "create"}, {"resources", "template", "delete"},
		{"resources", "fixed", "create"}, {"resources", "fixed", "delete"},
		{"resources", "pool", "create"}, {"resources", "pool", "delete"},
		{"resources", "pool", "refresh"}, {"resources", "pool", "force-drain"},
		{"network", "nat64", "set"}, {"network", "nat64", "clear"},
		{"network", "resolvers", "replace"},
		{"resources", "list"},
		{"nodes", "list"}, {"nodes", "get"}, {"nodes", "create"}, {"nodes", "update"},
		{"nodes", "delete"}, {"nodes", "start"}, {"nodes", "stop"}, {"nodes", "batch-create"}, {"nodes", "move"},
		{"folders", "rename"}, {"folders", "start"}, {"folders", "stop"}, {"folders", "delete"},
		{"network", "show"}, {"network", "test"},
		{"logs", "tail"}, {"logs", "clear"}, {"stats", "show"}, {"stats", "reset"},
		{"status"}, {"schema"}, {"export"}, {"apply"},
	}
	for _, path := range commands {
		if len(args) < len(path) {
			continue
		}
		matched := true
		for index := range path {
			if args[index] != path[index] {
				matched = false
				break
			}
		}
		if matched {
			return strings.Join(path, "."), args[len(path):], true
		}
	}
	return "", nil, false
}

func readAgentCLIDocument(stdin io.Reader, path, format string) (json.RawMessage, error) {
	var reader io.Reader = stdin
	var file *os.File
	var err error
	if path != "" && path != "-" {
		file, err = os.Open(filepath.Clean(path))
		if err != nil {
			return nil, err
		}
		defer file.Close()
		reader = file
	}
	payload, err := io.ReadAll(io.LimitReader(reader, agentCLIMaxDocumentBytes+1))
	if err != nil || len(payload) == 0 || len(payload) > agentCLIMaxDocumentBytes {
		return nil, errors.New("invalid input document")
	}
	if format == "yaml" {
		return agentCLIYAMLToJSON(payload)
	}
	return decodeAgentCLIJSONDocument(payload)
}

func decodeAgentCLIJSONDocument(payload []byte) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil || len(value) == 0 {
		return nil, errors.New("invalid JSON document")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("multiple JSON documents are not allowed")
	}
	return append(json.RawMessage(nil), value...), nil
}

func agentCLIYAMLToJSON(payload []byte) (json.RawMessage, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(payload))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil || len(document.Content) == 0 {
		return nil, errors.New("invalid YAML document")
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("multiple YAML documents are not allowed")
	}
	var value any
	if err := document.Decode(&value); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil || !json.Valid(encoded) {
		return nil, errors.New("invalid YAML document")
	}
	return encoded, nil
}

func agentCLIJSONToYAML(payload json.RawMessage) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("multiple JSON values are not allowed")
	}
	return yaml.Marshal(value)
}

func decodeAgentCLIResponse(payload json.RawMessage) (agentCLIResponse, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var response agentCLIResponse
	if err := decoder.Decode(&response); err != nil {
		return agentCLIResponse{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentCLIResponse{}, errors.New("multiple JSON values are not allowed")
	}
	if response.OK {
		if response.Error != nil || len(response.Data) == 0 {
			return agentCLIResponse{}, errors.New("invalid success response")
		}
		return response, nil
	}
	if response.Error == nil || response.Error.Code == "" || response.Error.Message == "" || len(response.Data) != 0 {
		return agentCLIResponse{}, errors.New("invalid failure response")
	}
	return response, nil
}

func marshalAgentCLIArguments(values map[string]any, valid bool) (json.RawMessage, error) {
	if !valid {
		return nil, errors.New("required selector is missing")
	}
	return json.Marshal(values)
}

func validAgentCLIFormat(format string) bool {
	return format == "json" || format == "yaml"
}

func agentCLIHealth(payload json.RawMessage) string {
	var data struct {
		Health string `json:"health"`
	}
	if json.Unmarshal(payload, &data) != nil {
		return ""
	}
	return data.Health
}

func agentCLIExitCode(code string) int {
	switch code {
	case "invalid_usage", "invalid_document":
		return 2
	case "unavailable", "permission_denied":
		return 3
	case "confirmation_required", "not_found", "conflict":
		return 4
	default:
		return 1
	}
}

func writeAgentCLIError(output io.Writer, code, message string) {
	payload, _ := json.Marshal(map[string]any{
		"ok": false,
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
	writeAgentCLIRaw(output, payload)
}

func writeAgentCLIRaw(output io.Writer, payload []byte) {
	_, _ = fmt.Fprintln(output, string(bytes.TrimSpace(payload)))
}
