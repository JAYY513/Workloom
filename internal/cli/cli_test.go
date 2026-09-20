package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workloom/internal/domain"
	"workloom/internal/events"
	"workloom/internal/record"
	"workloom/internal/storage"
	"workloom/internal/workitem"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}

func insideRepo(t *testing.T, dir string) bool {
	t.Helper()
	_, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	return err == nil
}

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = Run(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestVersionAndHelp(t *testing.T) {
	code, out, _ := run(t, "--version")
	if code != CodeOK || !strings.HasPrefix(out, "devsys ") {
		t.Fatalf("--version: code=%d out=%q", code, out)
	}
	code, out, _ = run(t, "--help")
	if code != CodeOK || !strings.Contains(out, "usage:") {
		t.Fatalf("--help: code=%d out=%q", code, out)
	}
}

func TestUsageErrors(t *testing.T) {
	cases := [][]string{
		{},
		{"bogus"},
		{"--nope"},
		{"init", "extra"},
	}
	for _, args := range cases {
		code, _, errOut := run(t, args...)
		if code != CodeUsage {
			t.Errorf("%v: code=%d, want %d (stderr=%q)", args, code, CodeUsage, errOut)
		}
		if !strings.Contains(errOut, "devsys:") {
			t.Errorf("%v: stderr=%q", args, errOut)
		}
	}
}

func TestInitJSON(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "MyProj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	cfgDir := t.TempDir()
	t.Setenv("DEVSYS_CONFIG_DIR", cfgDir)
	t.Chdir(repo)

	code, out, errOut := run(t, "--json", "init")
	if code != CodeOK {
		t.Fatalf("code=%d stderr=%s", code, errOut)
	}
	var payload struct {
		OK              bool     `json:"ok"`
		Root            string   `json:"root"`
		ID              string   `json:"id"`
		Name            string   `json:"name"`
		Created         []string `json:"created"`
		RegistryPath    string   `json:"registry_path"`
		RegistryUpdated bool     `json:"registry_updated"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json: %v out=%s", err, out)
	}
	if !payload.OK || payload.ID != "myproj" || payload.Name != "MyProj" {
		t.Errorf("payload = %+v", payload)
	}
	if len(payload.Created) == 0 {
		t.Error("created must list new paths on first run")
	}
	wantReg := filepath.Join(cfgDir, "registry.yaml")
	if payload.RegistryPath != wantReg || !payload.RegistryUpdated {
		t.Errorf("registry fields = %q %v", payload.RegistryPath, payload.RegistryUpdated)
	}
	data, err := os.ReadFile(wantReg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "myproj") {
		t.Errorf("registry content = %s", data)
	}

	code, out, errOut = run(t, "--json", "init")
	if code != CodeOK {
		t.Fatalf("second init: code=%d stderr=%s", code, errOut)
	}
	var second struct {
		Created []string `json:"created"`
	}
	if err := json.Unmarshal([]byte(out), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Created) != 0 {
		t.Errorf("second init created %v", second.Created)
	}
}

func TestInitQuiet(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "quietproj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)

	code, out, errOut := run(t, "--quiet", "init")
	if code != CodeOK {
		t.Fatalf("code=%d stderr=%s", code, errOut)
	}
	if out != "" {
		t.Errorf("--quiet must print nothing, got %q", out)
	}
}

func TestInitNotGitRepo(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	if insideRepo(t, dir) {
		t.Skipf("temp dir %s is inside a repository", dir)
	}
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(dir)

	code, _, errOut := run(t, "init")
	if code != CodePrecondition {
		t.Fatalf("code=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "git init") {
		t.Errorf("stderr = %q", errOut)
	}

	code, _, errOut = run(t, "--json", "init")
	if code != CodePrecondition {
		t.Fatalf("json code=%d stderr=%s", code, errOut)
	}
	var payload struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    int    `json:"code"`
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(errOut), &payload); err != nil {
		t.Fatalf("json error: %v stderr=%s", err, errOut)
	}
	if payload.OK || payload.Error.Code != CodePrecondition || payload.Error.Kind != "precondition" {
		t.Errorf("payload = %+v", payload)
	}
}

func TestConfigCheck(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "checkproj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)

	if code, _, errOut := run(t, "init"); code != CodeOK {
		t.Fatalf("init: code=%d stderr=%s", code, errOut)
	}

	code, out, errOut := run(t, "config", "check")
	if code != CodeOK {
		t.Fatalf("check: code=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(out, "config ok: 4 files checked") || !strings.Contains(out, "checkproj") {
		t.Errorf("stdout = %q", out)
	}

	// Three independently located defects, returned together.
	broken := "# c\nschema_version: 1\nid: 42\nunknown_field: true\n"
	if err := os.WriteFile(filepath.Join(repo, ".devsys", "project.yaml"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut = run(t, "config", "check")
	if code != CodeInvalid {
		t.Fatalf("code=%d, want %d (stderr=%s)", code, CodeInvalid, errOut)
	}
	if out != "" {
		t.Errorf("stdout = %q, want nothing on problems", out)
	}

	code, _, errOut = run(t, "--json", "config", "check")
	if code != CodeInvalid {
		t.Fatalf("json code=%d stderr=%s", code, errOut)
	}
	var payload struct {
		OK    bool `json:"ok"`
		Error struct {
			Code     int `json:"code"`
			Kind     string
			Problems []struct {
				File   string `json:"file"`
				Line   int    `json:"line"`
				Field  string `json:"field"`
				Reason string `json:"reason"`
			} `json:"problems"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(errOut), &payload); err != nil {
		t.Fatalf("json: %v stderr=%s", err, errOut)
	}
	if payload.OK || payload.Error.Code != CodeInvalid || payload.Error.Kind != "invalid" {
		t.Errorf("payload = %+v", payload)
	}
	if len(payload.Error.Problems) != 3 {
		t.Fatalf("problems = %+v", payload.Error.Problems)
	}
	first := payload.Error.Problems[0]
	if first.File != "project.yaml" || first.Line != 3 || first.Field != "id" ||
		first.Reason != "expected string, got !!int" {
		t.Errorf("first problem = %+v", first)
	}
}

func TestConfigCheckNotInitialized(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "emptyproj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)

	code, _, errOut := run(t, "config", "check")
	if code != CodePrecondition {
		t.Fatalf("code=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "devsys init") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestConfigUsage(t *testing.T) {
	for _, args := range [][]string{{"config"}, {"config", "bogus"}, {"config", "check", "extra"}} {
		code, _, errOut := run(t, args...)
		if code != CodeUsage {
			t.Errorf("%v: code=%d stderr=%s", args, code, errOut)
		}
	}
}

func TestInitRefusesUnknownSchemaVersion(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "oldproj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)

	if code, _, errOut := run(t, "init"); code != CodeOK {
		t.Fatalf("init: code=%d stderr=%s", code, errOut)
	}
	projectPath := filepath.Join(repo, ".devsys", "project.yaml")
	data, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(data), "schema_version: 1", "schema_version: 99", 1)
	if patched == string(data) {
		t.Fatalf("could not patch project.yaml:\n%s", data)
	}
	if err := os.WriteFile(projectPath, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, errOut := run(t, "init")
	if code != CodeInvalid {
		t.Fatalf("init: code=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "unsupported version 99") || !strings.Contains(errOut, "M9.2") {
		t.Errorf("stderr = %q", errOut)
	}
	after, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != patched {
		t.Errorf("refused init must not rewrite project.yaml:\n%s", after)
	}

	// Read-only diagnostics still work on state this build refuses to write.
	code, _, errOut = run(t, "config", "check")
	if code != CodeInvalid {
		t.Fatalf("check: code=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "project.yaml:2: schema_version: unsupported version 99") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestSearchReportsMatchesAndExcludes(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "searchproj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)
	if code, _, errOut := run(t, "init"); code != CodeOK {
		t.Fatalf("init: code=%d stderr=%s", code, errOut)
	}
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(repo, ".devsys", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("workitems/WLM-1.yaml", "schema_version: 1\nid: WLM-1\ntitle: 密码协议适配\n")
	write("workitems/WLM-2.yaml", "schema_version: 1\nid: WLM-2\ntitle: 无关任务\n")
	write("local/lock", "密码 in locked material\n")

	code, out, errOut := run(t, "search", "密码")
	if code != CodeOK {
		t.Fatalf("search: code=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(out, "workitems/WLM-1.yaml:3: title: 密码协议适配") {
		t.Errorf("stdout missing match:\n%s", out)
	}
	if strings.Contains(out, "local/") {
		t.Errorf("searched excluded local/: %s", out)
	}
	if !strings.Contains(out, "1 match(es)") {
		t.Errorf("stdout = %q", out)
	}

	// --json shape: structured matches plus a total count.
	code, out, errOut = run(t, "--json", "search", "WLM-2")
	if code != CodeOK {
		t.Fatalf("json: code=%d stderr=%s", code, errOut)
	}
	var payload struct {
		OK      bool `json:"ok"`
		Matches []struct {
			Path string `json:"path"`
			Line int    `json:"line"`
			Text string `json:"text"`
		} `json:"matches"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json: %v out=%s", err, out)
	}
	if !payload.OK || payload.Total != 1 || len(payload.Matches) != 1 || payload.Matches[0].Path != "workitems/WLM-2.yaml" {
		t.Errorf("payload = %+v", payload)
	}

	// No hits is still success, with an empty match list in JSON mode.
	code, out, _ = run(t, "--json", "search", "no-such-keyword")
	if code != CodeOK {
		t.Fatalf("no-hit: code=%d", code)
	}
	if !strings.Contains(out, `"total":0`) || !strings.Contains(out, `"matches":[]`) {
		t.Errorf("no-hit payload = %q", out)
	}

	// Usage errors: no keyword, or more than one.
	for _, args := range [][]string{{"search"}, {"search", "a", "b"}} {
		if code, _, _ := run(t, args...); code != CodeUsage {
			t.Errorf("%v: code=%d, want %d", args, code, CodeUsage)
		}
	}
}

// writeWorkflow writes one policy file below root/.devsys/workflows/.
func writeWorkflow(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, ".devsys", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const cliValidWorkflow = `---
id: alpha
name: 示例
version: 1
steps:
  - id: inspect
    type: inspect
---
`

func TestWorkflowCheckNotInitialized(t *testing.T) {
	t.Chdir(t.TempDir())
	code, _, errOut := run(t, "workflow", "check")
	if code != CodePrecondition {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestWorkflowCheckValidWithWarning(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "alpha.md", cliValidWorkflow)
	writeWorkflow(t, dir, "notes.txt", "not a policy")
	t.Chdir(dir)

	code, out, errOut := run(t, "workflow", "check")
	if code != CodeOK {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"workflow ok: 1 policies checked", "warning: workflows/notes.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, want %q", out, want)
		}
	}

	// --quiet drops the success banner but keeps warnings visible.
	code, out, errOut = run(t, "--quiet", "workflow", "check")
	if code != CodeOK {
		t.Fatalf("quiet: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "warning: workflows/notes.txt") || strings.Contains(out, "workflow ok:") {
		t.Errorf("quiet stdout = %q", out)
	}
}

func TestWorkflowCheckInvalidPolicy(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "beta.md", "---\nname: 缺 id\nversion: 1\nsteps:\n  - id: inspect\n    type: inspect\n---\n")
	t.Chdir(dir)

	code, _, errOut := run(t, "workflow", "check")
	if code != CodeInvalid {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(errOut, "workflows/beta.md") || !strings.Contains(errOut, "id: required") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestWorkflowCheckBadTemplate(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "alpha.md", "---\nid: alpha\nname: 示例\nversion: 1\nsteps:\n  - id: inspect\n    type: inspect\n---\nhello {{broken\n")
	t.Chdir(dir)

	code, _, errOut := run(t, "workflow", "check")
	if code != CodeInvalid {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(errOut, "workflows/alpha.md:9: body") || !strings.Contains(errOut, "unclosed {{") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestWorkflowCheckJSON(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "alpha.md", cliValidWorkflow)
	writeWorkflow(t, dir, "notes.txt", "not a policy")
	t.Chdir(dir)

	code, out, errOut := run(t, "--json", "workflow", "check")
	if code != CodeOK {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	var payload struct {
		OK       bool `json:"ok"`
		Policies []struct {
			File    string `json:"file"`
			ID      string `json:"id"`
			Name    string `json:"name"`
			Version int    `json:"version"`
		} `json:"policies"`
		Warnings []struct {
			File     string `json:"file"`
			Severity string `json:"severity"`
		} `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json: %v out=%s", err, out)
	}
	if !payload.OK || len(payload.Policies) != 1 || payload.Policies[0].ID != "alpha" || payload.Policies[0].Version != 1 {
		t.Errorf("policies = %+v", payload.Policies)
	}
	if len(payload.Warnings) != 1 || payload.Warnings[0].File != "workflows/notes.txt" || payload.Warnings[0].Severity != "warning" {
		t.Errorf("warnings = %+v", payload.Warnings)
	}

	// Invalid state renders the located problems as a JSON array.
	writeWorkflow(t, dir, "beta.md", "---\nname: 缺 id\nversion: 1\nsteps:\n  - id: inspect\n    type: inspect\n---\n")
	code, _, errOut = run(t, "--json", "workflow", "check")
	if code != CodeInvalid {
		t.Fatalf("invalid: code=%d stderr=%q", code, errOut)
	}
	var errPayload struct {
		OK    bool `json:"ok"`
		Error struct {
			Code     int `json:"code"`
			Problems []struct {
				File  string `json:"file"`
				Field string `json:"field"`
			} `json:"problems"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(errOut), &errPayload); err != nil {
		t.Fatalf("json error: %v out=%s", err, errOut)
	}
	if errPayload.Error.Code != CodeInvalid || len(errPayload.Error.Problems) == 0 || errPayload.Error.Problems[0].File != "workflows/beta.md" {
		t.Errorf("error payload = %+v", errPayload)
	}
}

const cliGatePolicy = `---
id: gated
name: 门禁示例
version: 1
steps:
  - id: implement
    type: execute
  - id: verify
    type: verify
gates:
  exempt_stages:
    - draft
  stages:
    verification:
      require_artifacts:
        - test-results
      require_comment: true
    done:
      require_artifacts:
        - test-results
quality_gate:
  min_score: 55
---
prompt body
`

// gatedProject prepares a fresh initialized project declaring the gated
// policy and returns the repo root with a work item store.
func gatedProject(t *testing.T) (string, *workitem.Store) {
	t.Helper()
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)
	if code, _, errOut := run(t, "init"); code != CodeOK {
		t.Fatalf("init: code=%d stderr=%q", code, errOut)
	}
	writeWorkflow(t, repo, "gated.md", cliGatePolicy)
	return repo, workitem.New(repo)
}

// createGatedWorkitem creates a ready work item bound to the gated policy.
func createGatedWorkitem(t *testing.T, items *workitem.Store, title, description string) string {
	t.Helper()
	return createReadyWorkitem(t, items, readyItem(title, description, "gated"))
}

// readyItem is the base work item fixtures start from: draft, in the demo
// project, optionally bound to a policy ("" leaves it under no instance, which
// is how a project with policies still ends up with ungated work).
func readyItem(title, description, policyID string) *domain.WorkItem {
	now := time.Now().UTC()
	wi := &domain.WorkItem{
		ProjectID: "demo", Type: "task", Title: title, Description: description,
		Status:    domain.StatusDraft,
		CreatedAt: now, UpdatedAt: now,
	}
	if policyID != "" {
		wi.Workflow = &domain.WorkflowInstance{ID: policyID, Step: "implement", StepEnteredAt: now}
	}
	return wi
}

// createReadyWorkitem creates wi and drives it to status ready, where the
// readiness evaluation and the claim path see it.
func createReadyWorkitem(t *testing.T, items *workitem.Store, wi *domain.WorkItem) string {
	t.Helper()
	id, err := items.Create(context.Background(), wi, "WLM")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	transitionTo(t, items, id, domain.StatusBacklog)
	transitionTo(t, items, id, domain.StatusReady)
	return id
}

func expectOf(t *testing.T, items *workitem.Store, id string) string {
	t.Helper()
	_, raw, err := items.ReadSnapshot(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", storage.HashBytes(raw))
}

func TestTransitionGateBlocksUntilEvidence(t *testing.T) {
	repo, items := gatedProject(t)
	id := createGatedWorkitem(t, items, "gate fixture task", "fixture")

	code, _, errOut := run(t, "workitem", "transition", "--id", id, "--to", "verification",
		"--actor", "test", "--reason", "try", "--expect", expectOf(t, items, id))
	if code != CodeInvalid {
		t.Fatalf("blocked transition: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"gates.stages.verification", "test-results", "comment"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr = %q, want %q", errOut, want)
		}
	}

	ctx := context.Background()
	if _, err := record.New(repo).CreateArtifact(ctx, &domain.Artifact{
		ProjectID: "demo", Type: "test", Name: "test-results", Status: "ready",
		RelatedWorkItems: []string{id},
	}); err != nil {
		t.Fatal(err)
	}
	if err := events.New(repo).Append(ctx, &domain.Event{
		ProjectID: "demo", Type: "comment",
		Subject: domain.Reference{Type: "workitem", ID: id}, Actor: "test", Content: "evidence ready",
	}); err != nil {
		t.Fatal(err)
	}

	code, _, errOut = run(t, "workitem", "transition", "--id", id, "--to", "verification",
		"--actor", "test", "--reason", "go", "--expect", expectOf(t, items, id))
	if code != CodeOK {
		t.Fatalf("allowed transition: code=%d stderr=%q", code, errOut)
	}
	got, err := items.Get(ctx, id)
	if err != nil || got.Status != domain.StatusVerification {
		t.Fatalf("status = %+v err=%v", got, err)
	}
}

func TestClaimQualityGateBlocksAndImproves(t *testing.T) {
	_, items := gatedProject(t)
	id := createGatedWorkitem(t, items, "fix", "x")

	code, _, errOut := run(t, "workitem", "claim", "--id", id, "--owner", "tester",
		"--reason", "try", "--expect", expectOf(t, items, id))
	if code != CodeInvalid {
		t.Fatalf("blocked claim: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"quality_gate.min_score", "标题过短", "缺少验收标准"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr = %q, want %q", errOut, want)
		}
	}

	ctx := context.Background()
	cur, raw, err := items.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	cur.Title = "实现质量门与门禁的 CLI 接线（M3.3）"
	cur.Description = "背景：领取前需要确定性质量门。\n- 见 internal/cli/cli.go 的接线\n- 验收：go test ./... 通过"
	cur.AcceptanceCriteria = []string{"低质量任务被拒且返回改进项"}
	if err := items.Update(ctx, cur, raw); err != nil {
		t.Fatalf("update: %v", err)
	}

	code, out, errOut := run(t, "workitem", "claim", "--id", id, "--owner", "tester",
		"--reason", "go", "--expect", expectOf(t, items, id))
	if code != CodeOK {
		t.Fatalf("allowed claim: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "claimed "+id) {
		t.Errorf("stdout = %q", out)
	}
}

const cliLKGPolicy = `---
id: lkg
name: LKG 示例
version: 1
steps:
  - id: implement
    type: execute
gates:
  stages:
    verification:
      require_comment: true
---
prompt body
`

// createWorkitemWithPolicy creates a ready work item declaring policyID.
func createWorkitemWithPolicy(t *testing.T, items *workitem.Store, policyID, title, description string) string {
	t.Helper()
	return createReadyWorkitem(t, items, readyItem(title, description, policyID))
}

func TestLastKnownGoodBlocksDispatchKeepsReadsWorking(t *testing.T) {
	repo, items := gatedProject(t)
	writeWorkflow(t, repo, "lkg.md", cliLKGPolicy)
	ctx := context.Background()
	target := createWorkitemWithPolicy(t, items, "lkg", "lkg target task", "fixture")

	// Prime the snapshot: a gate-checked transition on the valid policy runs
	// the resolver (and gets blocked by the comment gate).
	code, out, errOut := run(t, "workitem", "transition", "--id", target, "--to", "verification",
		"--actor", "test", "--reason", "prime", "--expect", expectOf(t, items, target))
	if code != CodeInvalid {
		t.Fatalf("priming transition: code=%d stderr=%q", code, errOut)
	}

	writeWorkflow(t, repo, "lkg.md", "---\nname: 缺 id\nversion: \"x\"\n---\n")

	// A blocked gate still surfaces the last-known-good notice on stdout
	// while the gate error names the missing evidence.
	code, out, errOut = run(t, "workitem", "transition", "--id", target, "--to", "verification",
		"--actor", "test", "--reason", "try", "--expect", expectOf(t, items, target))
	if code != CodeInvalid {
		t.Fatalf("blocked lkg transition: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "last-known-good") {
		t.Errorf("stdout = %q, want the last-known-good notice", out)
	}
	if !strings.Contains(errOut, "comment") {
		t.Errorf("stderr = %q, want the missing comment evidence", errOut)
	}

	// Read path stays usable and reports the risk, including work items that
	// reference a missing policy.
	ghost := &domain.WorkItem{
		ProjectID: "demo", Type: "task", Title: "ghost policy task", Status: domain.StatusDraft,
		Workflow:  &domain.WorkflowInstance{ID: "ghost-policy", Step: "implement", StepEnteredAt: time.Now().UTC()},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if _, err := items.Create(ctx, ghost, "WLM"); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = run(t, "--json", "next")
	if code != CodeOK {
		t.Fatalf("next: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "invalid_policy") {
		t.Errorf("next json = %s", out)
	}
	if !strings.Contains(out, `references missing policy \"ghost-policy\"`) {
		t.Errorf("next json = %s, want the ghost policy reference", out)
	}

	// Dispatch (claim) is refused with the reason.
	code, _, errOut = run(t, "workitem", "claim", "--id", target, "--owner", "tester",
		"--reason", "try", "--expect", expectOf(t, items, target))
	if code != CodeInvalid {
		t.Fatalf("claim: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"invalid", "last-known-good"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr = %q, want %q", errOut, want)
		}
	}

	// The gate keeps working from the snapshot and says so.
	if err := events.New(repo).Append(ctx, &domain.Event{
		ProjectID: "demo", Type: "comment",
		Subject: domain.Reference{Type: "workitem", ID: target},
		Actor:   "test", Content: "verification evidence",
	}); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = run(t, "workitem", "transition", "--id", target, "--to", "verification",
		"--actor", "test", "--reason", "go", "--expect", expectOf(t, items, target))
	if code != CodeOK {
		t.Fatalf("lkg transition: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "last-known-good") {
		t.Errorf("stdout = %q", out)
	}

	// Restoring the file unblocks dispatch.
	writeWorkflow(t, repo, "lkg.md", cliLKGPolicy)
	restored := createWorkitemWithPolicy(t, items, "lkg", "lkg restored task", "fixture")
	code, _, errOut = run(t, "workitem", "claim", "--id", restored, "--owner", "tester",
		"--reason", "go", "--expect", expectOf(t, items, restored))
	if code != CodeOK {
		t.Fatalf("restored claim: code=%d stderr=%q", code, errOut)
	}
}

const cliFlowPolicy = `---
id: flow
name: CLI 流程
version: 1
steps:
  - id: inspect
    type: inspect
  - id: implement
    type: execute
  - id: verify
    type: verify
transitions:
  - from: inspect
    to: implement
    when: workitem.clarification_needed == false
  - from: inspect
    to: verify
    when: workitem.clarification_needed == true
  - from: implement
    to: verify
  - from: verify
    to: done
---
prompt body
`

func TestWorkflowLifecycleCLI(t *testing.T) {
	repo, items := gatedProject(t)
	writeWorkflow(t, repo, "flow.md", cliFlowPolicy)
	id := createPlainWorkitem(t, items, "workflow lifecycle task")

	code, out, errOut := run(t, "workflow", "start", "--id", id, "--policy", "flow",
		"--actor", "smoke", "--reason", "begin", "--expect", expectOf(t, items, id))
	if code != CodeOK {
		t.Fatalf("start: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "step inspect") {
		t.Errorf("stdout = %q", out)
	}

	code, out, errOut = run(t, "workflow", "next", "--id", id)
	if code != CodeOK {
		t.Fatalf("next: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"candidate: to=implement", "candidate: to=verify", "next: implement"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, want %q", out, want)
		}
	}

	code, _, errOut = run(t, "workflow", "step-complete", "--id", id, "--to", "nope",
		"--actor", "smoke", "--reason", "try", "--expect", expectOf(t, items, id))
	if code != CodeInvalid {
		t.Fatalf("jump: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"not a declared transition", "to=implement", "to=verify"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr = %q, want %q", errOut, want)
		}
	}

	code, out, errOut = run(t, "workflow", "step-complete", "--id", id, "--to", "implement",
		"--actor", "smoke", "--reason", "go", "--expect", expectOf(t, items, id))
	if code != CodeOK {
		t.Fatalf("complete: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "step implement") {
		t.Errorf("stdout = %q", out)
	}

	code, _, errOut = run(t, "workflow", "pause", "--id", id, "--actor", "smoke", "--reason", "hold", "--expect", expectOf(t, items, id))
	if code != CodeOK {
		t.Fatalf("pause: code=%d stderr=%q", code, errOut)
	}
	code, out, errOut = run(t, "workflow", "next", "--id", id)
	if code != CodeOK {
		t.Fatalf("paused next: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "paused=true") || strings.Contains(out, "next: ") {
		t.Errorf("paused next = %q, want paused state without a next hint", out)
	}
	code, _, errOut = run(t, "workflow", "pause", "--id", id, "--actor", "smoke", "--reason", "again", "--expect", expectOf(t, items, id))
	if code != CodeInvalid || !strings.Contains(errOut, "already paused") {
		t.Fatalf("repeat pause: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "workflow", "step-complete", "--id", id, "--actor", "smoke", "--reason", "try", "--expect", expectOf(t, items, id))
	if code != CodeInvalid || !strings.Contains(errOut, "paused") {
		t.Fatalf("paused complete: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "workflow", "resume", "--id", id, "--actor", "smoke", "--reason", "go", "--expect", expectOf(t, items, id))
	if code != CodeOK {
		t.Fatalf("resume: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "workflow", "resume", "--id", id, "--actor", "smoke", "--reason", "again", "--expect", expectOf(t, items, id))
	if code != CodeInvalid || !strings.Contains(errOut, "not paused") {
		t.Fatalf("repeat resume: code=%d stderr=%q", code, errOut)
	}
	code, out, errOut = run(t, "workflow", "step-complete", "--id", id, "--to", "verify",
		"--actor", "smoke", "--reason", "go", "--expect", expectOf(t, items, id))
	if code != CodeOK || !strings.Contains(out, "step verify") {
		t.Fatalf("complete to verify: code=%d out=%q stderr=%q", code, out, errOut)
	}
	code, out, errOut = run(t, "workflow", "step-complete", "--id", id, "--to", "done",
		"--actor", "smoke", "--reason", "finish", "--expect", expectOf(t, items, id))
	if code != CodeOK || !strings.Contains(out, "step done") {
		t.Fatalf("complete to done: code=%d out=%q stderr=%q", code, out, errOut)
	}
	code, _, errOut = run(t, "workflow", "pause", "--id", id, "--actor", "smoke", "--reason", "hold", "--expect", expectOf(t, items, id))
	if code != CodeInvalid || !strings.Contains(errOut, "already done") {
		t.Fatalf("pause after done: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "workflow", "cancel", "--id", id, "--actor", "smoke", "--reason", "stop", "--expect", expectOf(t, items, id))
	if code != CodeInvalid || !strings.Contains(errOut, "already done") {
		t.Fatalf("cancel after done: code=%d stderr=%q", code, errOut)
	}

	// Cancel stays available on a live instance and is terminal.
	cancelID := createPlainWorkitem(t, items, "cancel task")
	code, _, errOut = run(t, "workflow", "start", "--id", cancelID, "--policy", "flow",
		"--actor", "smoke", "--reason", "begin", "--expect", expectOf(t, items, cancelID))
	if code != CodeOK {
		t.Fatalf("cancel start: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "workflow", "cancel", "--id", cancelID, "--actor", "smoke", "--reason", "stop", "--expect", expectOf(t, items, cancelID))
	if code != CodeOK {
		t.Fatalf("cancel: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "workflow", "step-complete", "--id", cancelID, "--actor", "smoke", "--reason", "try", "--expect", expectOf(t, items, cancelID))
	if code != CodeInvalid || !strings.Contains(errOut, "already cancelled") {
		t.Fatalf("terminal complete: code=%d stderr=%q", code, errOut)
	}

	// Instance operations never create scheduling material.
	if entries, err := os.ReadDir(filepath.Join(repo, ".devsys", "scheduling")); err == nil && len(entries) > 0 {
		t.Errorf("scheduling files appeared: %v", entries)
	}
}

func TestWorkflowStartBlockedByInvalidPolicyAdvanceUsesLKG(t *testing.T) {
	repo, items := gatedProject(t)
	writeWorkflow(t, repo, "flow.md", cliFlowPolicy)
	id := createPlainWorkitem(t, items, "lkg flow task")

	// Prime the last-known-good snapshot with a valid start.
	code, _, errOut := run(t, "workflow", "start", "--id", id, "--policy", "flow",
		"--actor", "smoke", "--reason", "begin", "--expect", expectOf(t, items, id))
	if code != CodeOK {
		t.Fatalf("start: code=%d stderr=%q", code, errOut)
	}

	writeWorkflow(t, repo, "flow.md", "---\nname: 缺 id\nversion: \"x\"\n---\n")

	// Advancing accepts the snapshot and says so.
	code, out, errOut := run(t, "workflow", "step-complete", "--id", id,
		"--actor", "smoke", "--reason", "go", "--expect", expectOf(t, items, id))
	if code != CodeOK {
		t.Fatalf("lkg complete: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "last-known-good") {
		t.Errorf("stdout = %q", out)
	}

	// The read path keeps working with the notice as well.
	code, out, errOut = run(t, "workflow", "next", "--id", id)
	if code != CodeOK || !strings.Contains(out, "last-known-good") {
		t.Fatalf("lkg next: code=%d out=%q stderr=%q", code, out, errOut)
	}

	// Starting a new instance is dispatch-like and stays blocked.
	other := createPlainWorkitem(t, items, "lkg blocked start")
	code, _, errOut = run(t, "workflow", "start", "--id", other, "--policy", "flow",
		"--actor", "smoke", "--reason", "try", "--expect", expectOf(t, items, other))
	if code != CodeInvalid || !strings.Contains(errOut, "blocked until the file is fixed") {
		t.Fatalf("blocked start: code=%d stderr=%q", code, errOut)
	}
}

const cliApprovalPolicy = `---
id: approval-flow
name: 审批门禁
version: 1
steps:
  - id: implement
    type: execute
gates:
  exempt_stages:
    - draft
    - backlog
  stages:
    in_progress:
      require_approval: true
---
prompt body
`

const cliApprovalRegressPolicy = `---
id: approval-flow
name: 审批门禁（回退）
version: 1
steps:
  - id: implement
    type: execute
gates:
  exempt_stages:
    - draft
    - backlog
  stages:
    in_progress:
      require_approval: true
on_reject: regress:ready
---
prompt body
`

func TestApprovalGateLifecycle(t *testing.T) {
	repo, items := gatedProject(t)
	writeWorkflow(t, repo, "approval-flow.md", cliApprovalPolicy)
	ctx := context.Background()
	id := createWorkitemWithPolicy(t, items, "approval-flow", "approval lifecycle task", "fixture")

	// Without an approval the gate refuses the advance.
	code, _, errOut := run(t, "workitem", "transition", "--id", id, "--to", "in_progress",
		"--actor", "dev", "--reason", "start", "--expect", expectOf(t, items, id))
	if code != CodeInvalid || !strings.Contains(errOut, "approval") {
		t.Fatalf("gated transition: code=%d stderr=%q", code, errOut)
	}

	code, out, errOut := run(t, "approval", "request", "--id", id, "--stage", "in_progress",
		"--actor", "dev", "--reason", "architecture change")
	if code != CodeOK {
		t.Fatalf("request: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "approval-1") || !strings.Contains(out, "pending") {
		t.Errorf("stdout = %q", out)
	}

	// A pending approval is a priority-2 signal for next.
	code, out, errOut = run(t, "--json", "next")
	if code != CodeOK || !strings.Contains(out, "pending_approval") || !strings.Contains(out, "waiting for approval-1") {
		t.Fatalf("next json = %s (stderr=%q)", out, errOut)
	}

	// Still refused while pending.
	code, _, errOut = run(t, "workitem", "transition", "--id", id, "--to", "in_progress",
		"--actor", "dev", "--reason", "start", "--expect", expectOf(t, items, id))
	if code != CodeInvalid {
		t.Fatalf("pending transition: code=%d stderr=%q", code, errOut)
	}

	code, _, errOut = run(t, "approval", "approve", "--id", "approval-1", "--by", "boss", "--comment", "ok")
	if code != CodeOK {
		t.Fatalf("approve: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "workitem", "transition", "--id", id, "--to", "in_progress",
		"--actor", "dev", "--reason", "go", "--expect", expectOf(t, items, id))
	if code != CodeOK {
		t.Fatalf("approved transition: code=%d stderr=%q", code, errOut)
	}

	// The approval is consumed exactly once, with an event.
	code, out, _ = run(t, "--json", "approval", "list", "--workitem", id)
	if code != CodeOK || !strings.Contains(out, "consumed_at") || strings.Contains(out, `"consumed_at":null`) {
		t.Errorf("list = %s", out)
	}
	evs, err := events.New(repo).Read(ctx, events.Filter{
		Subject: &domain.Reference{Type: "approval", ID: "approval-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	consumed := 0
	for _, ev := range evs {
		if ev.Type == "approval_consumed" {
			consumed++
		}
	}
	if consumed != 1 {
		t.Errorf("approval_consumed events = %d, want 1", consumed)
	}

	// Leaving the stage invalidates an unconsumed approval: approval-2 is
	// requested from in_progress but the work item moves to review first.
	code, _, errOut = run(t, "approval", "request", "--id", id, "--stage", "in_progress",
		"--actor", "dev", "--reason", "second change")
	if code != CodeOK {
		t.Fatalf("second request: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "approval", "approve", "--id", "approval-2", "--by", "boss")
	if code != CodeOK {
		t.Fatalf("second approve: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "workitem", "transition", "--id", id, "--to", "review",
		"--actor", "dev", "--reason", "review", "--expect", expectOf(t, items, id))
	if code != CodeOK {
		t.Fatalf("to review: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "workitem", "transition", "--id", id, "--to", "in_progress",
		"--actor", "dev", "--reason", "back", "--expect", expectOf(t, items, id))
	if code != CodeInvalid || !strings.Contains(errOut, "approval") {
		t.Fatalf("stale approval: code=%d stderr=%q", code, errOut)
	}

	// A fresh request from the current stage works.
	code, _, errOut = run(t, "approval", "request", "--id", id, "--stage", "in_progress",
		"--actor", "dev", "--reason", "third attempt")
	if code != CodeOK {
		t.Fatalf("third request: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "approval", "approve", "--id", "approval-3", "--by", "boss")
	if code != CodeOK {
		t.Fatalf("third approve: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "workitem", "transition", "--id", id, "--to", "in_progress",
		"--actor", "dev", "--reason", "go again", "--expect", expectOf(t, items, id))
	if code != CodeOK {
		t.Fatalf("fresh approval transition: code=%d stderr=%q", code, errOut)
	}
}

func TestApprovalRejectBlocksWorkitem(t *testing.T) {
	repo, items := gatedProject(t)
	writeWorkflow(t, repo, "approval-flow.md", cliApprovalPolicy)
	ctx := context.Background()
	id := createWorkitemWithPolicy(t, items, "approval-flow", "reject task", "fixture")

	code, _, errOut := run(t, "approval", "request", "--id", id, "--stage", "in_progress",
		"--actor", "dev", "--reason", "architecture change")
	if code != CodeOK {
		t.Fatalf("request: code=%d stderr=%q", code, errOut)
	}
	code, out, errOut := run(t, "approval", "reject", "--id", "approval-1", "--by", "boss", "--reason", "risk too high")
	if code != CodeOK {
		t.Fatalf("reject: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "blocked") {
		t.Errorf("stdout = %q", out)
	}

	wi, err := items.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if wi.Status != domain.StatusBlocked || wi.PreviousStatus != "ready" {
		t.Errorf("work item = %s (previous %s)", wi.Status, wi.PreviousStatus)
	}

	// Reason and approval reference ride the status_changed event, and the
	// rejection itself is recorded.
	evs, err := events.New(repo).Read(ctx, events.Filter{Subject: &domain.Reference{Type: "workitem", ID: id}})
	if err != nil {
		t.Fatal(err)
	}
	foundReason := false
	for _, ev := range evs {
		if ev.Type == "status_changed" && strings.Contains(ev.Content, "approval-1 rejected: risk too high") {
			foundReason = true
		}
	}
	if !foundReason {
		t.Errorf("status_changed events = %+v", evs)
	}
	rejections, err := events.New(repo).Read(ctx, events.Filter{Subject: &domain.Reference{Type: "approval", ID: "approval-1"}})
	if err != nil {
		t.Fatal(err)
	}
	foundRejected := false
	for _, ev := range rejections {
		if ev.Type == "approval_rejected" {
			foundRejected = true
		}
	}
	if !foundRejected {
		t.Errorf("approval events = %+v", rejections)
	}
}

func TestApprovalRejectRegressionPolicy(t *testing.T) {
	repo, items := gatedProject(t)
	writeWorkflow(t, repo, "approval-flow.md", cliApprovalRegressPolicy)
	id := createWorkitemWithPolicy(t, items, "approval-flow", "regress task", "fixture")
	transitionTo(t, items, id, domain.StatusReview)

	code, _, errOut := run(t, "approval", "request", "--id", id, "--stage", "in_progress",
		"--actor", "dev", "--reason", "change")
	if code != CodeOK {
		t.Fatalf("request: code=%d stderr=%q", code, errOut)
	}
	code, out, errOut := run(t, "approval", "reject", "--id", "approval-1", "--by", "boss", "--reason", "no")
	if code != CodeOK {
		t.Fatalf("reject: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "ready") {
		t.Errorf("stdout = %q", out)
	}
	wi, err := items.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if wi.Status != domain.StatusReady {
		t.Errorf("status = %s, want ready (on_reject regress)", wi.Status)
	}
}

func TestApprovalStaleAfterReturningToStatus(t *testing.T) {
	repo, items := gatedProject(t)
	writeWorkflow(t, repo, "approval-flow.md", cliApprovalPolicy)
	id := createWorkitemWithPolicy(t, items, "approval-flow", "stale approval task", "fixture")

	code, _, errOut := run(t, "approval", "request", "--id", id, "--stage", "in_progress",
		"--actor", "dev", "--reason", "change")
	if code != CodeOK {
		t.Fatalf("request: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "approval", "approve", "--id", "approval-1", "--by", "boss")
	if code != CodeOK {
		t.Fatalf("approve: code=%d stderr=%q", code, errOut)
	}

	// Leaving ready and returning invalidates the approval (方案 §4.9).
	transitionTo(t, items, id, domain.StatusBacklog)
	transitionTo(t, items, id, domain.StatusReady)
	code, _, errOut = run(t, "workitem", "transition", "--id", id, "--to", "in_progress",
		"--actor", "dev", "--reason", "try", "--expect", expectOf(t, items, id))
	if code != CodeInvalid || !strings.Contains(errOut, "approval") {
		t.Fatalf("stale approval allowed: code=%d stderr=%q", code, errOut)
	}

	code, out, _ := run(t, "--json", "approval", "list", "--workitem", id)
	if code != CodeOK || !strings.Contains(out, `"invalidated_at":"2`) {
		t.Errorf("list = %s", out)
	}
	code, _, errOut = run(t, "approval", "approve", "--id", "approval-1", "--by", "boss")
	if code != CodeInvalid || !strings.Contains(errOut, "invalidated") {
		t.Fatalf("approve invalidated: code=%d stderr=%q", code, errOut)
	}

	// A fresh request from the current status works.
	code, _, errOut = run(t, "approval", "request", "--id", id, "--stage", "in_progress",
		"--actor", "dev", "--reason", "again")
	if code != CodeOK {
		t.Fatalf("fresh request: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "approval", "approve", "--id", "approval-2", "--by", "boss")
	if code != CodeOK {
		t.Fatalf("fresh approve: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "workitem", "transition", "--id", id, "--to", "in_progress",
		"--actor", "dev", "--reason", "go", "--expect", expectOf(t, items, id))
	if code != CodeOK {
		t.Fatalf("fresh approval transition: code=%d stderr=%q", code, errOut)
	}
}

func TestApprovalRejectAtomicWhenDispositionBlocked(t *testing.T) {
	_, items := gatedProject(t)
	ctx := context.Background()
	id := createPlainWorkitem(t, items, "claimed reject task")

	// An active lease fences the disposition transition; the rejection must
	// then be refused as a whole.
	if _, err := items.Claim(ctx, id, workitem.ClaimOptions{
		Owner: "holder", Actor: "holder", Reason: "work", Expected: rawOf(t, items, id),
	}); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "approval", "request", "--id", id, "--stage", "in_progress",
		"--actor", "dev", "--reason", "change")
	if code != CodeOK {
		t.Fatalf("request: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "approval", "reject", "--id", "approval-1", "--by", "boss", "--reason", "no")
	if code != CodeInvalid || !strings.Contains(errOut, "not recorded") {
		t.Fatalf("reject: code=%d stderr=%q", code, errOut)
	}

	// Nothing changed: the approval is still pending and the work item keeps
	// its status.
	code, out, _ := run(t, "--json", "approval", "list", "--workitem", id)
	if code != CodeOK || !strings.Contains(out, `"pending"`) {
		t.Errorf("list = %s", out)
	}
	wi, err := items.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if wi.Status != domain.StatusInProgress {
		t.Errorf("status = %s, want in_progress", wi.Status)
	}
}

func TestApprovalListRejectsUnknownStatus(t *testing.T) {
	gatedProject(t)
	code, _, errOut := run(t, "approval", "list", "--status", "foo")
	if code != CodeUsage || !strings.Contains(errOut, "--status") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

// rawOf returns the raw snapshot bytes a mutation needs as its guard.
func rawOf(t *testing.T, items *workitem.Store, id string) []byte {
	t.Helper()
	_, raw, err := items.ReadSnapshot(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// transitionTo walks one work item one legal step.
func transitionTo(t *testing.T, items *workitem.Store, id, target string) {
	t.Helper()
	if _, err := items.Transition(context.Background(), id, workitem.TransitionRequest{
		TargetStatus: target, Actor: "test", Reason: "fixture",
	}, rawOf(t, items, id)); err != nil {
		t.Fatalf("transition %s -> %s: %v", id, target, err)
	}
}

// createPlainWorkitem creates a work item without a workflow instance (so the
// quality gate does not apply) and walks it to ready.
func createPlainWorkitem(t *testing.T, items *workitem.Store, title string) string {
	t.Helper()
	now := time.Now().UTC()
	wi := &domain.WorkItem{
		ProjectID: "demo", Type: "task", Title: title, Description: "fixture",
		Status: domain.StatusDraft, CreatedAt: now, UpdatedAt: now,
	}
	id, err := items.Create(context.Background(), wi, "WLM")
	if err != nil {
		t.Fatal(err)
	}
	transitionTo(t, items, id, domain.StatusBacklog)
	transitionTo(t, items, id, domain.StatusReady)
	return id
}

func TestNextReportsAllFourRisksAndRecoversFirst(t *testing.T) {
	repo, items := gatedProject(t)
	ctx := context.Background()

	// WLM-1: an expired claim (1ns lease).
	expired := createPlainWorkitem(t, items, "expired claim task")
	if _, err := items.Claim(ctx, expired, workitem.ClaimOptions{
		Owner: "probe", Actor: "probe", Reason: "fixture",
		Expected: rawOf(t, items, expired), LeaseDuration: time.Nanosecond,
	}); err != nil {
		t.Fatal(err)
	}

	// WLM-2: an orphan claim (its run file is gone).
	orphan := createPlainWorkitem(t, items, "orphan claim task")
	claim, err := items.Claim(ctx, orphan, workitem.ClaimOptions{
		Owner: "probe", Actor: "probe", Reason: "fixture", Expected: rawOf(t, items, orphan),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, ".devsys", "runs", claim.RunID+".yaml")); err != nil {
		t.Fatal(err)
	}

	// WLM-3: blocked.
	blocked := createPlainWorkitem(t, items, "blocked task")
	transitionTo(t, items, blocked, domain.StatusBlocked)

	// WLM-4: a review that has been waiting beyond the stale threshold.
	stale := createPlainWorkitem(t, items, "stale review task")
	transitionTo(t, items, stale, domain.StatusReview)
	cur, raw, err := items.ReadSnapshot(ctx, stale)
	if err != nil {
		t.Fatal(err)
	}
	cur.UpdatedAt = time.Now().UTC().Add(-48 * time.Hour)
	if err := items.Update(ctx, cur, raw); err != nil {
		t.Fatal(err)
	}

	// WLM-5: a runnable ready candidate.
	createPlainWorkitem(t, items, "ready candidate")

	code, out, errOut := run(t, "--json", "next")
	if code != CodeOK {
		t.Fatalf("next: code=%d stderr=%q", code, errOut)
	}
	var payload struct {
		OK      bool   `json:"ok"`
		Verdict string `json:"verdict"`
		Risks   []struct {
			Kind       string `json:"kind"`
			WorkitemID string `json:"workitem_id"`
		} `json:"risks"`
		Next struct {
			Action     string `json:"action"`
			WorkitemID string `json:"workitem_id"`
		} `json:"next"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json: %v out=%s", err, out)
	}
	if payload.Verdict != "CONCERNS" {
		t.Errorf("verdict = %s (%s)", payload.Verdict, out)
	}
	kinds := map[string]bool{}
	for _, r := range payload.Risks {
		kinds[r.Kind] = true
	}
	for _, want := range []string{"expired_lease", "orphan_claim", "blocked", "stale_review"} {
		if !kinds[want] {
			t.Errorf("missing risk %q in %+v", want, payload.Risks)
		}
	}
	if payload.Next.Action != "recover_claim" || payload.Next.WorkitemID == "" {
		t.Errorf("next = %+v", payload.Next)
	}

	// The human output carries no time estimates.
	code, out, errOut = run(t, "next")
	if code != CodeOK {
		t.Fatalf("next human: code=%d stderr=%q", code, errOut)
	}
	for _, word := range []string{"estimate", "ETA", "估算", "预计", "耗时"} {
		if strings.Contains(out, word) {
			t.Errorf("stdout contains %q:\n%s", word, out)
		}
	}
	if !strings.Contains(out, "readiness: CONCERNS") || !strings.Contains(out, "next: recover_claim") {
		t.Errorf("stdout = %q", out)
	}
}
