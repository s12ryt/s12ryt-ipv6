package cicheck_test

import (
	"fmt"
	goversion "go/version"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
	"gopkg.in/yaml.v3"
)

var (
	fullCommitSHA  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	versionComment = regexp.MustCompile(`^#?\s*v[0-9]+\.[0-9]+\.[0-9]+\s*$`)
)

type workflow struct {
	Permissions map[string]string `yaml:"permissions"`
	Env         map[string]string `yaml:"env"`
	Jobs        map[string]job    `yaml:"jobs"`
}

type job struct {
	Condition      string            `yaml:"if"`
	RunsOn         string            `yaml:"runs-on"`
	TimeoutMinutes int               `yaml:"timeout-minutes"`
	Permissions    map[string]string `yaml:"permissions"`
	Steps          []step            `yaml:"steps"`
}

type step struct {
	Name             string         `yaml:"name"`
	Uses             string         `yaml:"uses"`
	Run              string         `yaml:"run"`
	WorkingDirectory string         `yaml:"working-directory"`
	With             map[string]any `yaml:"with"`
}

type workflowFile struct {
	path     string
	config   workflow
	document yaml.Node
}

func TestWorkflowActionsUseImmutableCommitSHAs(t *testing.T) {
	for _, file := range loadWorkflows(t) {
		for _, use := range actionUseNodes(&file.document) {
			if strings.HasPrefix(use.Value, "./") {
				continue
			}
			at := strings.LastIndex(use.Value, "@")
			if at < 1 || !fullCommitSHA.MatchString(use.Value[at+1:]) {
				t.Errorf("%s:%d action must use a full commit SHA: %q", filepath.Base(file.path), use.Line, use.Value)
				continue
			}
			if !versionComment.MatchString(use.LineComment) {
				t.Errorf("%s:%d pinned action must retain an exact version comment: %q", filepath.Base(file.path), use.Line, use.LineComment)
			}
		}
	}
}

func TestWorkflowJobsUseBoundedPinnedRunners(t *testing.T) {
	for _, file := range loadWorkflows(t) {
		for name, job := range file.config.Jobs {
			if job.RunsOn != "ubuntu-24.04" {
				t.Errorf("%s job %q runs-on = %q, want ubuntu-24.04", filepath.Base(file.path), name, job.RunsOn)
			}
			if job.TimeoutMinutes < 1 || job.TimeoutMinutes > 60 {
				t.Errorf("%s job %q timeout-minutes = %d, want 1..60", filepath.Base(file.path), name, job.TimeoutMinutes)
			}
		}
	}
}

func TestCIEnforcesQualityAndSupplyChainGates(t *testing.T) {
	workflows := loadWorkflows(t)
	ci := workflows["ci.yml"]
	if ci == nil {
		t.Fatal("ci.yml is missing")
	}

	if ci.config.Env["GOFLAGS"] != "-mod=readonly" {
		t.Errorf("CI GOFLAGS = %q, want -mod=readonly", ci.config.Env["GOFLAGS"])
	}

	for _, command := range []string{
		"npm audit --audit-level=high",
		"go mod download",
		"go mod verify",
		"go mod tidy -diff",
		"go test -race -shuffle=on",
		"github.com/rhysd/actionlint/cmd/actionlint@v1.7.12",
	} {
		if !workflowRuns(ci.config, command) {
			t.Errorf("CI must run %q", command)
		}
	}

	if !workflowRunsInDirectory(ci.config, "web", "go test .") {
		t.Error("CI must test the nested web Go module with `go test .`")
	}
	assertReadOnlyCheckouts(t, "ci.yml", ci.config)

	dependencyReview, ok := ci.config.Jobs["dependency-review"]
	if !ok {
		t.Fatal("CI must include a dependency-review job")
	}
	if !strings.Contains(dependencyReview.Condition, "github.event_name == 'pull_request'") {
		t.Errorf("dependency-review must be pull-request-only, got if = %q", dependencyReview.Condition)
	}
	if !jobUsesAction(dependencyReview, "actions/dependency-review-action@") {
		t.Error("dependency-review job must use actions/dependency-review-action")
	}
}

func TestCIGofmtCheckAvoidsUnquotedCommandSubstitution(t *testing.T) {
	ci := loadWorkflows(t)["ci.yml"]
	if ci == nil {
		t.Fatal("ci.yml is missing")
	}

	for _, job := range ci.config.Jobs {
		for _, step := range job.Steps {
			if step.Name != "Check gofmt" {
				continue
			}
			if strings.Contains(step.Run, "gofmt -l $(") {
				t.Fatal("Check gofmt must not pass an unquoted command substitution to gofmt (ShellCheck SC2046)")
			}
			return
		}
	}
	t.Fatal("CI Check gofmt step is missing")
}

func TestReleaseRepeatsCriticalVerificationBeforePublishing(t *testing.T) {
	workflows := loadWorkflows(t)
	release := workflows["release.yml"]
	if release == nil {
		t.Fatal("release.yml is missing")
	}

	if release.config.Env["GOFLAGS"] != "-mod=readonly" {
		t.Errorf("release GOFLAGS = %q, want -mod=readonly", release.config.Env["GOFLAGS"])
	}
	for _, command := range []string{
		"npm audit --audit-level=high",
		"go mod download",
		"go mod verify",
		"go mod tidy -diff",
		"go test -race -shuffle=on",
	} {
		if !workflowRuns(release.config, command) {
			t.Errorf("release must run %q before publishing", command)
		}
	}
	if !workflowRunsInDirectory(release.config, "web", "go test .") {
		t.Error("release must test the nested web Go module")
	}
}

func TestGoToolchainAndVulnerabilityScannerArePinned(t *testing.T) {
	goModPath := filepath.Join(projectRoot(t), "go.mod")
	contents, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	parsed, err := modfile.Parse(goModPath, contents, nil)
	if err != nil {
		t.Fatalf("parse go.mod: %v", err)
	}
	if parsed.Go == nil || goversion.Compare("go"+parsed.Go.Version, "go1.25.13") < 0 {
		var actual string
		if parsed.Go != nil {
			actual = parsed.Go.Version
		}
		t.Errorf("go.mod Go version = %q, want at least 1.25.13", actual)
	}

	for name, file := range loadWorkflows(t) {
		if !workflowRuns(file.config, "golang.org/x/vuln/cmd/govulncheck@v1.1.4") {
			t.Errorf("%s must run pinned govulncheck v1.1.4", name)
		}
	}
}

func TestReleasePinsGoReleaserBinary(t *testing.T) {
	release := loadWorkflows(t)["release.yml"]
	if release == nil {
		t.Fatal("release.yml is missing")
	}
	for _, job := range release.config.Jobs {
		for _, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "goreleaser/goreleaser-action@") {
				continue
			}
			if got := fmt.Sprint(step.With["version"]); got != "v2.18.1" {
				t.Errorf("GoReleaser version = %q, want v2.18.1", got)
			}
		}
	}
}

func TestReleaseLimitsWritePermissionToPublishingJob(t *testing.T) {
	release := loadWorkflows(t)["release.yml"]
	if release == nil {
		t.Fatal("release.yml is missing")
	}
	if got := release.config.Permissions["contents"]; got != "read" {
		t.Errorf("release workflow contents permission = %q, want read", got)
	}

	verify, ok := release.config.Jobs["verify"]
	if !ok {
		t.Error("release workflow must isolate verification in a verify job")
	}
	publish, ok := release.config.Jobs["publish"]
	if !ok {
		t.Fatal("release workflow must isolate publishing in a publish job")
	}
	if got := publish.Permissions["contents"]; got != "write" {
		t.Errorf("publish contents permission = %q, want write", got)
	}
	for name, job := range release.config.Jobs {
		if name != "publish" && job.Permissions["contents"] == "write" {
			t.Errorf("non-publishing job %q must not have contents: write", name)
		}
	}
	for _, command := range []string{"npm ci", "go test", "go vet", "govulncheck"} {
		if jobRuns(publish, command) {
			t.Errorf("publish job must not run verification command %q with write permission", command)
		}
	}
	assertReadOnlyCheckouts(t, "release verify job", workflow{Jobs: map[string]job{"verify": verify}})
}

func loadWorkflows(t *testing.T) map[string]*workflowFile {
	t.Helper()
	root := projectRoot(t)
	paths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatalf("glob workflows: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no GitHub Actions workflows found")
	}

	result := make(map[string]*workflowFile, len(paths))
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		file := &workflowFile{path: path}
		if err := yaml.Unmarshal(contents, &file.config); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if err := yaml.Unmarshal(contents, &file.document); err != nil {
			t.Fatalf("decode YAML nodes for %s: %v", path, err)
		}
		result[filepath.Base(path)] = file
	}
	return result
}

func projectRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate workflow test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
}

func actionUseNodes(node *yaml.Node) []*yaml.Node {
	var result []*yaml.Node
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Value == "uses" && value.Kind == yaml.ScalarNode {
				result = append(result, value)
			}
		}
	}
	for _, child := range node.Content {
		result = append(result, actionUseNodes(child)...)
	}
	return result
}

func workflowRuns(config workflow, command string) bool {
	for _, job := range config.Jobs {
		if jobRuns(job, command) {
			return true
		}
	}
	return false
}

func jobRuns(job job, command string) bool {
	for _, step := range job.Steps {
		if strings.Contains(step.Run, command) {
			return true
		}
	}
	return false
}

func workflowRunsInDirectory(config workflow, directory, command string) bool {
	for _, job := range config.Jobs {
		for _, step := range job.Steps {
			if step.WorkingDirectory == directory && strings.TrimSpace(step.Run) == command {
				return true
			}
		}
	}
	return false
}

func assertReadOnlyCheckouts(t *testing.T, workflowName string, config workflow) {
	t.Helper()
	var mutable []string
	for jobName, job := range config.Jobs {
		for _, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "actions/checkout@") {
				continue
			}
			value, ok := step.With["persist-credentials"]
			if !ok || fmt.Sprint(value) != "false" {
				mutable = append(mutable, jobName)
			}
		}
	}
	if len(mutable) > 0 {
		sort.Strings(mutable)
		t.Errorf("%s checkout credentials must be disabled in jobs: %s", workflowName, strings.Join(mutable, ", "))
	}
}

func jobUsesAction(job job, prefix string) bool {
	for _, step := range job.Steps {
		if strings.HasPrefix(step.Uses, prefix) {
			return true
		}
	}
	return false
}
