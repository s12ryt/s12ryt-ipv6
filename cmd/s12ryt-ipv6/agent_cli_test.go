package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunRoutesAgentCommandWithoutChangingLegacyOutput(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var captured []string
	exitCode := runWithDependencies([]string{"agent", "status", "--data-dir", "/srv/s12ryt"}, &stdout, &stderr, commandDependencies{
		agent: func(_ context.Context, args []string, output io.Writer) int {
			captured = append([]string(nil), args...)
			_, _ = io.WriteString(output, `{"ok":true}`+"\n")
			return 0
		},
	})
	if exitCode != 0 || strings.Join(captured, " ") != "status --data-dir /srv/s12ryt" || stdout.String() != `{"ok":true}`+"\n" || stderr.Len() != 0 {
		t.Fatalf("exit=%d args=%q stdout=%q stderr=%q", exitCode, captured, stdout.String(), stderr.String())
	}
}

func TestRunAgentCLIStatusUsesSocketTimeoutAndHealthExit(t *testing.T) {
	for _, test := range []struct {
		name     string
		health   string
		wantExit int
	}{
		{name: "healthy", health: "healthy", wantExit: 0},
		{name: "degraded", health: "degraded", wantExit: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			var gotPath string
			var gotTimeout time.Duration
			var gotRequest map[string]any
			exitCode := runAgentCLI(context.Background(), []string{"status", "--data-dir", "/srv/s12ryt", "--timeout", "5s"}, strings.NewReader(""), &output,
				func(_ context.Context, path string, request json.RawMessage, timeout time.Duration) (json.RawMessage, error) {
					gotPath, gotTimeout = path, timeout
					if err := json.Unmarshal(request, &gotRequest); err != nil {
						t.Fatal(err)
					}
					return json.RawMessage(`{"ok":true,"data":{"health":"` + test.health + `"}}`), nil
				})
			if exitCode != test.wantExit || gotPath != filepath.Join("/srv/s12ryt", "control.sock") || gotTimeout != 5*time.Second || gotRequest["command"] != "status" {
				t.Fatalf("exit=%d path=%q timeout=%s request=%#v output=%q", exitCode, gotPath, gotTimeout, gotRequest, output.String())
			}
			assertSingleJSONValue(t, output.Bytes())
		})
	}
}

func TestRunAgentCLIExportRequiresFormatAndSupportsYAML(t *testing.T) {
	call := func(_ context.Context, _ string, request json.RawMessage, _ time.Duration) (json.RawMessage, error) {
		if !bytes.Contains(request, []byte(`"show_secrets":true`)) {
			t.Fatalf("request = %s", request)
		}
		return json.RawMessage(`{"ok":true,"data":{"schema_version":1,"nodes":[]}}`), nil
	}
	var invalid bytes.Buffer
	if exitCode := runAgentCLI(context.Background(), []string{"export"}, strings.NewReader(""), &invalid, call); exitCode != 2 {
		t.Fatalf("export without format exit=%d output=%q", exitCode, invalid.String())
	}
	assertAgentErrorCode(t, invalid.Bytes(), "invalid_usage")

	var output bytes.Buffer
	if exitCode := runAgentCLI(context.Background(), []string{"export", "--format", "yaml", "--show-secrets"}, strings.NewReader(""), &output, call); exitCode != 0 {
		t.Fatalf("export yaml exit=%d output=%q", exitCode, output.String())
	}
	if !strings.Contains(output.String(), "schema_version: 1") || strings.Contains(output.String(), `"ok"`) {
		t.Fatalf("yaml output = %q", output.String())
	}
}

func TestRunAgentCLIApplyReadsStrictYAMLAndPassesOptions(t *testing.T) {
	var output bytes.Buffer
	var request map[string]any
	exitCode := runAgentCLI(context.Background(), []string{"apply", "--format", "yaml", "--file", "-", "--prune", "--yes", "--dry-run"}, strings.NewReader("schema_version: 1\nnodes: []\n"), &output,
		func(_ context.Context, _ string, payload json.RawMessage, timeout time.Duration) (json.RawMessage, error) {
			if timeout != 10*time.Minute {
				t.Fatalf("timeout = %s", timeout)
			}
			if err := json.Unmarshal(payload, &request); err != nil {
				t.Fatal(err)
			}
			return json.RawMessage(`{"ok":true,"data":{"dry_run":true}}`), nil
		})
	if exitCode != 0 || request["command"] != "apply" || request["prune"] != true || request["yes"] != true || request["dry_run"] != true {
		t.Fatalf("exit=%d request=%#v output=%q", exitCode, request, output.String())
	}
	input, ok := request["input"].(map[string]any)
	if !ok || input["schema_version"] != float64(1) {
		t.Fatalf("input = %#v", request["input"])
	}
}

func TestRunAgentCLIMapsTreeFlagsAndCompoundInput(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		stdin       string
		wantCommand string
		wantArg     string
		wantValue   any
	}{
		{name: "selector", args: []string{"nodes", "get", "--id", "node-1", "--show-secrets"}, wantCommand: "nodes.get", wantArg: "id", wantValue: "node-1"},
		{name: "folder", args: []string{"folders", "rename", "--source", "old", "--target", "new"}, wantCommand: "folders.rename", wantArg: "target", wantValue: "new"},
		{name: "logs", args: []string{"logs", "tail", "--kind", "proxy", "--limit", "25"}, wantCommand: "logs.tail", wantArg: "limit", wantValue: float64(25)},
		{name: "compound", args: []string{"nodes", "create", "--file", "-"}, stdin: `{"id":"node-1"}`, wantCommand: "nodes.create", wantArg: "id", wantValue: "node-1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			var request map[string]any
			exitCode := runAgentCLI(context.Background(), test.args, strings.NewReader(test.stdin), &output,
				func(_ context.Context, _ string, payload json.RawMessage, _ time.Duration) (json.RawMessage, error) {
					if err := json.Unmarshal(payload, &request); err != nil {
						t.Fatal(err)
					}
					return json.RawMessage(`{"ok":true,"data":{}}`), nil
				})
			if exitCode != 0 || request["command"] != test.wantCommand {
				t.Fatalf("exit=%d request=%#v output=%q", exitCode, request, output.String())
			}
			arguments, _ := request["arguments"].(map[string]any)
			if arguments[test.wantArg] != test.wantValue {
				t.Fatalf("arguments = %#v", arguments)
			}
		})
	}
}

func TestRunAgentCLIClassifiesBusinessAndTransportFailures(t *testing.T) {
	var business bytes.Buffer
	businessExit := runAgentCLI(context.Background(), []string{"nodes", "get", "--id", "missing"}, strings.NewReader(""), &business,
		func(context.Context, string, json.RawMessage, time.Duration) (json.RawMessage, error) {
			return json.RawMessage(`{"ok":false,"error":{"code":"not_found","message":"node not found"}}`), nil
		})
	if businessExit != 4 {
		t.Fatalf("business exit=%d output=%q", businessExit, business.String())
	}

	var transport bytes.Buffer
	transportExit := runAgentCLI(context.Background(), []string{"status"}, strings.NewReader(""), &transport,
		func(context.Context, string, json.RawMessage, time.Duration) (json.RawMessage, error) {
			return nil, errors.New("sensitive socket detail")
		})
	if transportExit != 3 || strings.Contains(transport.String(), "sensitive socket detail") {
		t.Fatalf("transport exit=%d output=%q", transportExit, transport.String())
	}
	assertAgentErrorCode(t, transport.Bytes(), "unavailable")

	var permission bytes.Buffer
	permissionExit := runAgentCLI(context.Background(), []string{"status"}, strings.NewReader(""), &permission,
		func(context.Context, string, json.RawMessage, time.Duration) (json.RawMessage, error) {
			return nil, fmt.Errorf("dial control socket: %w", os.ErrPermission)
		})
	if permissionExit != 3 {
		t.Fatalf("permission exit=%d output=%q", permissionExit, permission.String())
	}
	assertAgentErrorCode(t, permission.Bytes(), "permission_denied")
}

func TestResolveAgentCLICommandMatchesPublishedTree(t *testing.T) {
	valid := [][]string{
		{"status"}, {"schema"}, {"export"}, {"apply"}, {"resources", "list"},
		{"resources", "template", "create"}, {"resources", "template", "delete"},
		{"resources", "fixed", "create"}, {"resources", "fixed", "delete"},
		{"resources", "pool", "create"}, {"resources", "pool", "delete"},
		{"resources", "pool", "refresh"}, {"resources", "pool", "force-drain"},
		{"nodes", "list"}, {"nodes", "get"}, {"nodes", "create"}, {"nodes", "update"},
		{"nodes", "delete"}, {"nodes", "start"}, {"nodes", "stop"}, {"nodes", "batch-create"}, {"nodes", "move"},
		{"folders", "rename"}, {"folders", "start"}, {"folders", "stop"}, {"folders", "delete"},
		{"network", "show"}, {"network", "test"}, {"network", "nat64", "set"}, {"network", "nat64", "clear"},
		{"network", "resolvers", "replace"}, {"logs", "tail"}, {"logs", "clear"}, {"stats", "show"}, {"stats", "reset"},
	}
	for _, args := range valid {
		command, remaining, ok := resolveAgentCLICommand(args)
		if !ok || len(remaining) != 0 || command != strings.Join(args, ".") {
			t.Fatalf("resolve(%q) = %q, %q, %v", args, command, remaining, ok)
		}
	}
	if command, _, ok := resolveAgentCLICommand([]string{"resources", "template", "force-drain"}); ok {
		t.Fatalf("unpublished command resolved as %q", command)
	}
}

func TestRunAgentCLIRejectsTimeoutDocumentAndResponseBoundaries(t *testing.T) {
	callCount := 0
	call := func(context.Context, string, json.RawMessage, time.Duration) (json.RawMessage, error) {
		callCount++
		return json.RawMessage(`{"ok":true,"data":{}}`), nil
	}
	tests := []struct {
		name  string
		args  []string
		stdin string
	}{
		{name: "short timeout", args: []string{"status", "--timeout", "999ms"}},
		{name: "long timeout", args: []string{"status", "--timeout", "31m"}},
		{name: "multiple JSON", args: []string{"apply", "--format", "json"}, stdin: `{"schema_version":1} {}`},
		{name: "multiple YAML", args: []string{"apply", "--format", "yaml"}, stdin: "schema_version: 1\n---\nschema_version: 1\n"},
		{name: "oversized", args: []string{"apply", "--format", "json"}, stdin: strings.Repeat("x", agentCLIMaxDocumentBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if exit := runAgentCLI(context.Background(), test.args, strings.NewReader(test.stdin), &output, call); exit != 2 {
				t.Fatalf("exit=%d output=%q", exit, output.String())
			}
			assertAgentErrorCode(t, output.Bytes(), "invalid_usage")
		})
	}
	if callCount != 0 {
		t.Fatalf("control calls = %d", callCount)
	}

	var output bytes.Buffer
	exit := runAgentCLI(context.Background(), []string{"status"}, strings.NewReader(""), &output,
		func(context.Context, string, json.RawMessage, time.Duration) (json.RawMessage, error) {
			return json.RawMessage(`{"ok":true,"data":{}} {}`), nil
		})
	if exit != 1 {
		t.Fatalf("invalid response exit=%d output=%q", exit, output.String())
	}
	assertAgentErrorCode(t, output.Bytes(), "internal_error")
}

func assertSingleJSONValue(t *testing.T, payload []byte) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode JSON: %v; payload=%q", err, payload)
	}
	if err := decoder.Decode(&value); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout contains multiple JSON values: %q", payload)
	}
}

func assertAgentErrorCode(t *testing.T, payload []byte, want string) {
	t.Helper()
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("decode error response: %v; payload=%q", err, payload)
	}
	if response.Error.Code != want {
		t.Fatalf("error code=%q want=%q payload=%q", response.Error.Code, want, payload)
	}
}
