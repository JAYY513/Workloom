package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/JAYY513/Workloom/internal/config"
)

// newWorkitem creates a work item through the CLI and returns its id.
func newWorkitem(t *testing.T, title string) string {
	t.Helper()
	code, out, errOut := run(t, "--json", "workitem", "create", "--title", title, "--actor", "operator", "--reason", "fixture")
	if code != CodeOK {
		t.Fatalf("workitem create: code=%d stderr=%q", code, errOut)
	}
	var payload struct {
		Item struct {
			ID string `json:"id"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("create output: %v (%s)", err, out)
	}
	return payload.Item.ID
}

// recordVersion reads a decision through the CLI and returns its version hash.
func cliRecordVersion(t *testing.T, kind, id string) string {
	t.Helper()
	code, out, errOut := run(t, "--json", kind, "get", id)
	if code != CodeOK {
		t.Fatalf("%s get %s: code=%d stderr=%q", kind, id, code, errOut)
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("cli output: %v (%s)", err, out)
	}
	return payload.Version
}

// TestRecordCLIAndMCPAgree pins the M4.3 acceptance: one record, two entry
// points, identical identity and version hash.
func TestRecordCLIAndMCPAgree(t *testing.T) {
	gatedProject(t)
	cs, _ := serveSession(t, "--tier", "standard")

	// CLI creates, MCP reads.
	wi := newWorkitem(t, "sdk adoption task")
	code, out, errOut := run(t, "--json", "decision", "create",
		"--title", "adopt the sdk", "--decision", "use the official MCP Go SDK",
		"--by", "operator", "--related", wi)
	if code != CodeOK {
		t.Fatalf("decision create: code=%d stderr=%q", code, errOut)
	}
	var created struct {
		Decision struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"decision"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("create output: %v (%s)", err, out)
	}
	if created.Decision.ID == "" || created.Version == "" {
		t.Fatalf("create output lacks identity: %s", out)
	}

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "decision_get", Arguments: map[string]any{"id": created.Decision.ID},
	})
	if err != nil {
		t.Fatalf("decision_get: %v", err)
	}
	if res.IsError {
		t.Fatalf("decision_get failed: %+v", res)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var overMCP struct {
		Decision struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"decision"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &overMCP); err != nil {
		t.Fatalf("mcp payload: %v (%s)", err, raw)
	}
	if overMCP.Version != created.Version || overMCP.Decision.Title != created.Decision.Title {
		t.Fatalf("MCP view %+v differs from CLI view %+v", overMCP, created)
	}

	// MCP creates, CLI reads — same record identity space.
	res, err = cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "finding_create",
		Arguments: map[string]any{
			"title": "gate evidence window", "description": "evidence is collected outside the transition transaction",
			"severity": "low", "related_workitems": []string{wi},
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("finding_create: err=%v res=%+v", err, res)
	}
	raw, _ = json.Marshal(res.StructuredContent)
	var finding struct {
		Finding struct {
			ID string `json:"id"`
		} `json:"finding"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &finding); err != nil {
		t.Fatalf("finding payload: %v (%s)", err, raw)
	}
	if got := cliRecordVersion(t, "finding", finding.Finding.ID); got != finding.Version {
		t.Fatalf("CLI version %s != MCP version %s", got, finding.Version)
	}
}

// TestContextLocatesRecords proves an agent can find tasks, progress and
// decisions without reading source.
func TestContextLocatesRecords(t *testing.T) {
	gatedProject(t)
	cs, _ := serveSession(t, "--tier", "standard")
	wi := newWorkitem(t, "context probe task")

	if code, _, errOut := run(t, "decision", "create", "--title", "context probe",
		"--decision", "recorded for context", "--by", "operator", "--related", wi); code != CodeOK {
		t.Fatalf("decision create: %d %s", code, errOut)
	}

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "context_get", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("context_get: err=%v res=%+v", err, res)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var view struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
		Verdict         string `json:"verdict"`
		RecentDecisions []struct {
			Ref string `json:"ref"`
		} `json:"recent_decisions"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("context payload: %v (%s)", err, raw)
	}
	if view.Project.ID != "proj" || view.Verdict == "" {
		t.Fatalf("context = %+v", view)
	}
	found := false
	for _, ref := range view.RecentDecisions {
		if strings.HasPrefix(ref.Ref, "decision://") {
			found = true
		}
	}
	if !found {
		t.Fatalf("context does not reference the decision: %s", raw)
	}

	// context_for_workitem surfaces records that reference the work item.
	res, err = cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "context_for_workitem", Arguments: map[string]any{"id": wi},
	})
	if err != nil {
		t.Fatalf("context_for_workitem: %v", err)
	}
	if res.IsError {
		t.Fatalf("context_for_workitem failed: %+v", res)
	}
	raw, _ = json.Marshal(res.StructuredContent)
	if !strings.Contains(string(raw), "decision://") {
		t.Fatalf("work item context lacks the referencing decision: %s", raw)
	}
}

// TestRunLifecycleEvidence walks the run tools the milestone owns (create,
// read, guarded update, log, heartbeat credentials).
func TestRunLifecycleEvidence(t *testing.T) {
	gatedProject(t)
	wi := newWorkitem(t, "run evidence task")
	if code, _, errOut := run(t, "run", "create", "--workitem", wi, "--actor", "operator", "--reason", "evidence"); code != CodeOK {
		t.Fatalf("run create: %d %s", code, errOut)
	}
	code, out, errOut := run(t, "--json", "run", "list", "--workitem", wi)
	if code != CodeOK {
		t.Fatalf("run list: %d %s", code, errOut)
	}
	var listed struct {
		Runs []struct {
			ID string `json:"id"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("run list output: %v (%s)", err, out)
	}
	if len(listed.Runs) != 1 {
		t.Fatalf("runs = %+v", listed.Runs)
	}
	id := listed.Runs[0].ID
	version := cliRecordVersion(t, "run", id)

	if code, _, errOut := run(t, "run", "update", "--id", id, "--status", "completed",
		"--log", "did the thing", "--expect", version); code != CodeOK {
		t.Fatalf("run update: %d %s", code, errOut)
	}
	code, out, _ = run(t, "--json", "run", "log", "--id", id)
	if code != CodeOK {
		t.Fatalf("run log: %d", code)
	}
	if !strings.Contains(out, "did the thing") {
		t.Fatalf("run log lacks the appended line: %s", out)
	}

	// Heartbeat needs a real lease: an unclaimed work item refuses it.
	if code, _, _ := run(t, "run", "heartbeat", "--id", id, "--owner", "nobody",
		"--token", "nope", "--actor", "operator", "--reason", "try"); code == CodeOK {
		t.Fatal("heartbeat without a lease succeeded")
	}
}

// TestArtifactVersionChain walks register → update → history.
func TestArtifactVersionChain(t *testing.T) {
	gatedProject(t)
	wi := newWorkitem(t, "artifact chain task")
	if code, _, errOut := run(t, "artifact", "register", "--name", "design.md",
		"--path", "docs/design.md", "--related", wi, "--actor", "tester", "--reason", "coverage"); code != CodeOK {
		t.Fatalf("artifact register: %d %s", code, errOut)
	}
	code, out, _ := run(t, "--json", "artifact", "list")
	if code != CodeOK {
		t.Fatal("artifact list failed")
	}
	var listed struct {
		Artifacts []struct {
			ID string `json:"id"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil || len(listed.Artifacts) != 1 {
		t.Fatalf("artifact list: %v (%s)", err, out)
	}
	id := listed.Artifacts[0].ID
	version := cliRecordVersion(t, "artifact", id)

	code, out, errOut := run(t, "--json", "artifact", "update", "--id", id, "--status", "approved", "--expect", version)
	if code != CodeOK {
		t.Fatalf("artifact update: %d %s", code, errOut)
	}
	var updated struct {
		Artifact struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Version int    `json:"version"`
		} `json:"artifact"`
	}
	if err := json.Unmarshal([]byte(out), &updated); err != nil {
		t.Fatalf("update output: %v (%s)", err, out)
	}
	if updated.Artifact.Version != 2 || updated.Artifact.Status != "approved" || updated.Artifact.ID == id {
		t.Fatalf("update did not append a new version: %s", out)
	}
	code, out, _ = run(t, "--json", "artifact", "history", updated.Artifact.ID)
	if code != CodeOK {
		t.Fatal("artifact history failed")
	}
	var history struct {
		Artifacts []struct {
			Version    int     `json:"version"`
			PreviousID *string `json:"previous_id"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal([]byte(out), &history); err != nil {
		t.Fatalf("history output: %v (%s)", err, out)
	}
	if len(history.Artifacts) != 2 || history.Artifacts[0].Version != 2 || history.Artifacts[0].PreviousID == nil {
		t.Fatalf("history = %s", out)
	}
}

// TestKnowledgeStatusDegrades checks the documented degradation (方案 §12.6):
// a project without a page layer answers `missing` with the reserved exit code
// and no error, so the rest of the system keeps working.
func TestKnowledgeStatusDegrades(t *testing.T) {
	gatedProject(t)
	code, out, errOut := run(t, "--json", "knowledge", "status")
	if code != CodeMissing {
		t.Fatalf("knowledge status: code=%d stderr=%q", code, errOut)
	}
	var view struct {
		OK     bool   `json:"ok"`
		Status string `json:"status"`
		Pages  int    `json:"pages"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if !view.OK || view.Status != "missing" || view.Pages != 0 || !strings.Contains(view.Reason, "生成器") {
		t.Fatalf("view = %s", out)
	}
	if strings.Contains(errOut, "devsys:") {
		t.Fatalf("degradation was reported as an error: %q", errOut)
	}
}

// writePage writes a knowledge page under the project.
func writePage(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const cliGoodPage = `---
status: stable
type: module
triggers:
  - 存储层
description: 可靠文本存储的定位
source_commit: 997c5f8
sources:
  - internal/storage/**
---

# 存储层
`

// TestKnowledgeValidateNoPageLayer pins the degradation: a project without a
// generator answers `missing` and exits 0 — the page layer's absence belongs to
// `knowledge status`, not to the format gate (方案 §12.6).
func TestKnowledgeValidateNoPageLayer(t *testing.T) {
	gatedProject(t)
	code, out, errOut := run(t, "knowledge", "validate")
	if code != CodeOK {
		t.Fatalf("validate: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "page layer not generated") {
		t.Fatalf("stdout = %q", out)
	}
	code, out, _ = run(t, "--json", "knowledge", "validate")
	if code != CodeOK {
		t.Fatalf("json validate: code=%d", code)
	}
	var view struct {
		OK      bool `json:"ok"`
		Missing bool `json:"missing"`
		Errors  int  `json:"errors"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if !view.OK || !view.Missing || view.Errors != 0 {
		t.Fatalf("view = %s", out)
	}
}

// TestKnowledgeValidateRejectsPageWithoutTriggers is the M5.1 acceptance: a
// page missing `triggers` is refused, located, and exits non-zero for CI.
func TestKnowledgeValidateRejectsPageWithoutTriggers(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "docs/repowiki/knowledge/坏页.md", `---
status: stable
type: module
description: 没有触发词的页面
source_commit: 997c5f8
---

# 坏页
`)
	code, _, errOut := run(t, "knowledge", "validate")
	if code != CodeInvalid {
		t.Fatalf("code = %d, want %d (stderr=%q)", code, CodeInvalid, errOut)
	}
	if !strings.Contains(errOut, "坏页.md:1: triggers: 缺少必填字段") {
		t.Fatalf("stderr = %q", errOut)
	}
	code, _, errOut = run(t, "--json", "knowledge", "validate")
	if code != CodeInvalid {
		t.Fatalf("json code = %d", code)
	}
	var payload struct {
		OK    bool `json:"ok"`
		Error struct {
			Code     int              `json:"code"`
			Kind     string           `json:"kind"`
			Problems []config.Problem `json:"problems"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(errOut), &payload); err != nil {
		t.Fatalf("json error payload: %v (%s)", err, errOut)
	}
	if payload.OK || payload.Error.Code != CodeInvalid || payload.Error.Kind != "knowledge" {
		t.Fatalf("payload = %s", errOut)
	}
	// The payload carries every located problem, warnings included, so CI sees
	// the whole picture; the error-severity one is the gate.
	if len(payload.Error.Problems) != 2 || payload.Error.Problems[0].Field != "triggers" || payload.Error.Problems[0].Severity != "" {
		t.Fatalf("problems = %+v", payload.Error.Problems)
	}
	if payload.Error.Problems[1].Severity != config.SeverityWarning {
		t.Fatalf("warning not marked: %+v", payload.Error.Problems[1])
	}

	// Fixing the page clears the gate: the same command exits 0.
	writePage(t, root, "docs/repowiki/knowledge/坏页.md", cliGoodPage)
	code, out, errOut := run(t, "knowledge", "validate")
	if code != CodeOK {
		t.Fatalf("after fix: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "1 pages, 0 errors, 0 warnings") {
		t.Fatalf("stdout = %q", out)
	}
}

// Advisories do not fail the gate, and they stay visible under --quiet.
func TestKnowledgeValidateWarnings(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "docs/repowiki/knowledge/无来源.md", strings.Replace(cliGoodPage, "sources:\n  - internal/storage/**\n", "", 1))
	code, out, errOut := run(t, "--quiet", "knowledge", "validate")
	if code != CodeOK {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "warning:") || !strings.Contains(out, "sources") {
		t.Fatalf("stdout = %q", out)
	}
}

// An explicit root must exist and stay inside the project: a report about a
// directory the layer cannot attribute to the project would be a lie.
func TestKnowledgeValidateExplicitRoots(t *testing.T) {
	root, _ := gatedProject(t)
	for _, tc := range []struct {
		path string
		want int
	}{
		{"docs/missing", CodePrecondition},
		{"../outside", CodePrecondition},
		{"docs/repowiki/page.txt", CodePrecondition},
	} {
		if code, _, errOut := run(t, "knowledge", "validate", tc.path); code != tc.want {
			t.Errorf("validate %s: code=%d want=%d (stderr=%q)", tc.path, code, tc.want, errOut)
		}
	}

	writePage(t, root, "docs/kb/页.md", cliGoodPage)
	code, out, errOut := run(t, "--json", "knowledge", "validate", "docs/kb")
	if code != CodeOK {
		t.Fatalf("validate docs/kb: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, `"roots":["docs/kb"]`) || !strings.Contains(out, "docs/kb/页.md") {
		t.Fatalf("stdout = %s", out)
	}
}

// knowledge_pages in config.yaml replaces the built-in roots, so a project
// that keeps its pages elsewhere is validated there.
func TestKnowledgeValidateConfigRoots(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "docs/kb/旧页.md", strings.Replace(cliGoodPage, "triggers:\n  - 存储层\n", "", 1))
	configPath := filepath.Join(root, ".devsys", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), "schema_version: 1", "schema_version: 1\nknowledge_pages:\n  - docs/kb\n", 1)
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "knowledge", "validate")
	if code != CodeInvalid {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(errOut, "docs/kb/旧页.md:1: triggers:") {
		t.Fatalf("stderr = %q", errOut)
	}
}

// TestKnowledgeScanExcludesSecretsAndLargeFiles is the M5.2 acceptance: the
// scan's file count matches the tree, and secrets and oversized files show up
// in the exclusion list with their reason.
func TestKnowledgeScanExcludesSecretsAndLargeFiles(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "a.go", "package a\n")
	writePage(t, root, ".env", "TOKEN=1\n")
	writePage(t, root, "big.bin", strings.Repeat("x", (1<<20)+1))

	code, out, errOut := run(t, "--json", "knowledge", "scan")
	if code != CodeOK {
		t.Fatalf("scan: code=%d stderr=%q", code, errOut)
	}
	var view struct {
		OK       bool   `json:"ok"`
		File     string `json:"file"`
		Files    int    `json:"files"`
		Excluded []struct {
			Path   string `json:"path"`
			Reason string `json:"reason"`
		} `json:"excluded"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	// The scanned tree contains a.go plus the repository-root
	// .gitattributes that `devsys init` now writes (#345 N2).
	if !view.OK || view.File != ".devsys/knowledge/snapshot.json" || view.Files != 2 {
		t.Fatalf("view = %s", out)
	}
	reasons := map[string]string{}
	for _, exclusion := range view.Excluded {
		reasons[exclusion.Path] = exclusion.Reason
	}
	if !strings.Contains(reasons[".env"], "密钥名模式") {
		t.Errorf(".env reason = %q", reasons[".env"])
	}
	if !strings.Contains(reasons["big.bin"], "字节上限") {
		t.Errorf("big.bin reason = %q", reasons["big.bin"])
	}
	if _, err := os.Stat(filepath.Join(root, ".devsys", "knowledge", "snapshot.json")); err != nil {
		t.Fatalf("snapshot not written: %v", err)
	}

	// The snapshot is derived state and can be rebuilt identically.
	first, err := os.ReadFile(filepath.Join(root, ".devsys", "knowledge", "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := run(t, "knowledge", "scan"); code != CodeOK {
		t.Fatalf("rescan: code=%d stderr=%q", code, errOut)
	}
	second, err := os.ReadFile(filepath.Join(root, ".devsys", "knowledge", "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	stripTime := func(data []byte) string {
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatal(err)
		}
		delete(payload, "generated_at")
		normalized, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		return string(normalized)
	}
	if stripTime(first) != stripTime(second) {
		t.Fatal("rescan produced a different snapshot")
	}
}
