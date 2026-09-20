package cli

// 首启 P1 回归（#337，#335 批次 B）：蓝图写入、MCP 空工具集、损坏 YAML
// 退出码、内嵌工作流模板。每条对应该批次修复前失败的 repro。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workloom/internal/domain"
)

// registerArtifact creates one artifact through the CLI and returns its id.
func registerArtifact(t *testing.T, name, path string) string {
	t.Helper()
	if code, _, errOut := run(t, "artifact", "register", "--name", name, "--path", path); code != CodeOK {
		t.Fatalf("artifact register: %d %s", code, errOut)
	}
	code, out, errOut := run(t, "--json", "artifact", "list")
	if code != CodeOK {
		t.Fatalf("artifact list: %d %s", code, errOut)
	}
	var payload struct {
		Artifacts []struct {
			ID string `json:"id"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil || len(payload.Artifacts) == 0 {
		t.Fatalf("artifact list payload: %v (%s)", err, out)
	}
	return payload.Artifacts[len(payload.Artifacts)-1].ID
}

// TestProjectUpdateBlueprintArtifact: 方式 A 第 7 步只靠 CLI 写下蓝图：
// artifact register → project update --blueprint-artifact → project
// blueprint 读回；空串清除。
func TestProjectUpdateBlueprintArtifact(t *testing.T) {
	gatedProject(t)
	id := registerArtifact(t, "blueprint.md", "docs/blueprint.md")

	code, _, errOut := run(t, "project", "update", "--blueprint-artifact", id, "--latest")
	if code != CodeOK {
		t.Fatalf("project update --blueprint-artifact: code=%d stderr=%q", code, errOut)
	}
	code, out, errOut := run(t, "project", "blueprint")
	if code != CodeOK || !strings.Contains(out, id) {
		t.Fatalf("project blueprint: code=%d out=%q stderr=%q, want the declared artifact", code, out, errOut)
	}

	code, _, errOut = run(t, "project", "update", "--blueprint-artifact", "", "--latest")
	if code != CodeOK {
		t.Fatalf("clear blueprint: code=%d stderr=%q", code, errOut)
	}
	code, out, _ = run(t, "project", "blueprint")
	if code != CodeOK || !strings.Contains(out, "no blueprint declared") {
		t.Fatalf("project blueprint after clear: code=%d out=%q, want no blueprint declared", code, out)
	}

	// Untouched fields stay untouched: a name-only update must not wipe the
	// declaration either.
	if code, _, errOut = run(t, "project", "update", "--blueprint-artifact", id, "--latest"); code != CodeOK {
		t.Fatalf("re-declare: %d %s", code, errOut)
	}
	if code, _, errOut = run(t, "project", "update", "--status", "active", "--latest"); code != CodeOK {
		t.Fatalf("status-only update: %d %s", code, errOut)
	}
	code, out, _ = run(t, "--json", "project", "get")
	if !strings.Contains(out, id) {
		t.Fatalf("status-only update wiped the blueprint: %s", out)
	}
}

func TestProjectUpdateBlueprintRejectsBadArtifact(t *testing.T) {
	gatedProject(t)
	code, _, errOut := run(t, "project", "update", "--blueprint-artifact", "artifact-404", "--latest")
	if code != CodePrecondition {
		t.Fatalf("unknown artifact: code=%d, want %d", code, CodePrecondition)
	}
	if !strings.Contains(errOut, "artifact register") {
		t.Fatalf("stderr = %q, want the artifact register way out", errOut)
	}
	code, _, _ = run(t, "project", "update", "--blueprint-artifact", "not-an-id", "--latest")
	if code != CodeUsage {
		t.Fatalf("malformed id: code=%d, want %d", code, CodeUsage)
	}
}

// TestMCPServeEmptyToolsetRefuses: reviewer×core 过滤后 0 工具，不再静默
// 空转；文案点名 --tier standard。
func TestMCPServeEmptyToolsetRefuses(t *testing.T) {
	gatedProject(t)
	code, _, errOut := runMCPArgs(t, "--profile", "reviewer")
	if code != CodeUsage {
		t.Fatalf("reviewer×core: code=%d, want %d", code, CodeUsage)
	}
	want := `profile "reviewer" has no tools in tier core; use --tier standard`
	if !strings.Contains(errOut, want) {
		t.Fatalf("stderr = %q, want %q", errOut, want)
	}
	code, _, errOut = runMCPArgs(t, "--profile", "reviewer,admin")
	want = `profiles "reviewer", "admin" have no tools in tier core; use --tier standard`
	if code != CodeUsage || !strings.Contains(errOut, want) {
		t.Fatalf("reviewer+admin×core: code=%d stderr=%q", code, errOut)
	}
}

// corruptWorkitemProject prepares a project whose WLM-1.yaml is damaged and
// whose healthy WLM-2 is done with a missing referenced artifact (a downward
// repair candidate Doctor must still find).
func corruptWorkitemProject(t *testing.T) string {
	t.Helper()
	repo, _ := gatedProject(t)
	writeWorkitemState(t, repo, &domain.WorkItem{
		SchemaVersion: domain.SchemaVersion,
		ID:            "WLM-2",
		Type:          "feature",
		Title:         "healthy item",
		Status:        domain.StatusDone,
		ArtifactRefs:  []string{"artifact-999"},
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	})
	if err := os.WriteFile(filepath.Join(repo, ".devsys", "workitems", "WLM-1.yaml"),
		[]byte("not: [a work item\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

// TestDoctorReportsCorruptFileAndKeepsScanning: 文件级 INVALID 行 + exit 4，
// 且健康项的提案照常给出。
func TestDoctorReportsCorruptFileAndKeepsScanning(t *testing.T) {
	corruptWorkitemProject(t)
	code, out, errOut := run(t, "doctor")
	if code != CodeInvalid {
		t.Fatalf("doctor: code=%d stderr=%q, want %d", code, errOut, CodeInvalid)
	}
	if !strings.Contains(out, "INVALID  workitems/WLM-1.yaml") {
		t.Fatalf("doctor stdout = %q, want the file-level INVALID line", out)
	}
	if !strings.Contains(out, "WLM-2") || !strings.Contains(out, "PROPOSE") {
		t.Fatalf("doctor stdout = %q, want the WLM-2 proposal to survive", out)
	}

	code, out, _ = run(t, "--json", "doctor")
	if code != CodeInvalid {
		t.Fatalf("--json doctor: code=%d, want %d", code, CodeInvalid)
	}
	var rep struct {
		InvalidFiles []struct {
			Path string `json:"path"`
			Err  string `json:"error"`
		} `json:"invalid_files"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("doctor json: %v (%s)", err, out)
	}
	if len(rep.InvalidFiles) != 1 || rep.InvalidFiles[0].Path != "workitems/WLM-1.yaml" || rep.InvalidFiles[0].Err == "" {
		t.Fatalf("invalid_files = %+v", rep.InvalidFiles)
	}
}

// TestCorruptWorkitemUntrustedStateExitFour: next/prime/project status 对
// 损坏受管 YAML 报 exit 4 并点名文件，而不是 exit 1 的整体失败。
func TestCorruptWorkitemUntrustedStateExitFour(t *testing.T) {
	corruptWorkitemProject(t)
	for _, args := range [][]string{
		{"next"},
		{"prime"},
		{"project", "status"},
		{"session", "start"},
	} {
		code, _, errOut := run(t, args...)
		if code != CodeInvalid {
			t.Fatalf("%v: code=%d, want %d (stderr=%q)", args, code, CodeInvalid, errOut)
		}
		if !strings.Contains(errOut, "workitems/WLM-1.yaml") {
			t.Fatalf("%v: stderr=%q, want the corrupt file named", args, errOut)
		}
	}
}

// TestWorkflowInitTemplate: 纯二进制路径（go:embed）装模板、可校验、
// 拒绝覆盖、未知模板列出可用项。
func TestWorkflowInitTemplate(t *testing.T) {
	repo, _ := gatedProject(t)
	code, out, errOut := run(t, "workflow", "init", "--template", "quick-fix")
	if code != CodeOK {
		t.Fatalf("workflow init: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, ".devsys/workflows/quick-fix.md") {
		t.Fatalf("stdout = %q, want the installed path", out)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".devsys", "workflows", "quick-fix.md"))
	if err != nil {
		t.Fatalf("read installed policy: %v", err)
	}
	if !strings.Contains(string(data), "id: quick-fix") {
		t.Fatalf("installed policy does not look like quick-fix:\n%s", data)
	}
	if code, _, errOut = run(t, "workflow", "check"); code != CodeOK {
		t.Fatalf("workflow check after init: code=%d stderr=%q", code, errOut)
	}

	code, _, errOut = run(t, "workflow", "init", "--template", "quick-fix")
	if code != CodeUsage || !strings.Contains(errOut, "already exists") {
		t.Fatalf("second init: code=%d stderr=%q, want a refusal", code, errOut)
	}
	code, _, errOut = run(t, "workflow", "init", "--template", "nope")
	if code != CodeUsage || !strings.Contains(errOut, "available") {
		t.Fatalf("unknown template: code=%d stderr=%q", code, errOut)
	}
	code, _, _ = run(t, "workflow", "init")
	if code != CodeUsage {
		t.Fatalf("init without --template: code=%d, want %d", code, CodeUsage)
	}
}

// TestInitHintNamesWorkflowInit: init 的下一步提示指向模板命令，不再
// 要求仓库内 docs/ 目录（纯二进制用户）。
func TestInitHintNamesWorkflowInit(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "HintProj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)
	code, out, errOut := run(t, "init")
	if code != CodeOK {
		t.Fatalf("init: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "workflow init --template") {
		t.Fatalf("init next steps = %q, want the workflow init hint", out)
	}
}
