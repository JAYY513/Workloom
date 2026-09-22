package cli

// #338（P2 批）回归：族级 --help exit 0、wire 默认含 skill 与 --check
// --strict、update/register 强制审计、config check 范围声明。每条对应该
// 批次修复前失败的 repro。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFamilyHelpExitsZero: 族无参/--help 打 usage 且 exit 0，不再被当
// 未知子命令（`workloom config --help` 曾是 exit 2）。
func TestFamilyHelpExitsZero(t *testing.T) {
	gatedProject(t)
	cases := [][]string{
		{"config", "--help"},
		{"config"},
		{"workitem", "--help"},
		{"workflow"},
		{"project", "-h"},
		{"mcp", "--help"},
		{"doctor", "--help"}, // leaf: top-level usage
		{"next", "-h"},
	}
	for _, args := range cases {
		code, out, errOut := run(t, args...)
		if code != CodeOK {
			t.Fatalf("%v: code=%d stderr=%q, want 0 with usage", args, code, errOut)
		}
		if !strings.Contains(out, "usage") && !strings.Contains(out, "subcommand") {
			t.Fatalf("%v: stdout=%q, want a usage text", args, out)
		}
	}
}

// TestWireCheckStrictFails: --check --strict 在检查项失败时 exit 3；
// 无 --strict 时保持 exit 0。
func TestWireCheckStrictFails(t *testing.T) {
	// 空目录：.devsys/ 缺失，strict 必须非 0。
	t.Chdir(t.TempDir())
	code, _, _ := run(t, "wire", "--check", "--strict")
	if code != CodePrecondition {
		t.Fatalf("strict on empty dir: code=%d, want %d", code, CodePrecondition)
	}
	code, _, _ = run(t, "wire", "--check")
	if code != CodeOK {
		t.Fatalf("plain --check: code=%d, want 0", code)
	}
}

// TestWireDefaultWritesSkill: `workloom wire` 默认写 skill 文件——AGENTS.md
// 块指向的 SKILL.md 必须存在（方式 B 只跑 wire 不再是断链安装）。
func TestWireDefaultWritesSkill(t *testing.T) {
	repo, _ := gatedProject(t)
	if err := os.RemoveAll(filepath.Join(repo, ".agents")); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "wire")
	if code != CodeOK {
		t.Fatalf("wire: code=%d stderr=%q", code, errOut)
	}
	skill := filepath.Join(repo, ".agents", "skills", "devsys", "SKILL.md")
	data, err := os.ReadFile(skill)
	if err != nil {
		t.Fatalf("default wire did not write %s: %v", skill, err)
	}
	if len(data) == 0 {
		t.Fatalf("%s is empty", skill)
	}
	if !strings.Contains(out, "skill: wrote") {
		t.Fatalf("wire stdout = %q, want the skill line", out)
	}
}

// TestWorkitemUpdateRequiresAudit: 缺 --actor/--reason → exit 2；补齐后
// 成功且写 workitem_updated 审计事件。
func TestWorkitemUpdateRequiresAudit(t *testing.T) {
	repo, _ := gatedProject(t)
	if code, _, errOut := run(t, "workitem", "create", "--title", "审计回归任务", "--actor", "me", "--reason", "fixture"); code != CodeOK {
		t.Fatalf("workitem create: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut := run(t, "workitem", "update", "--id", "WLM-1", "--title", "x")
	if code != CodeUsage || !strings.Contains(errOut, "--actor") {
		t.Fatalf("missing audit: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "workitem", "update", "--id", "WLM-1", "--title", "改名后标题", "--actor", "me", "--reason", "澄清范围", "--latest")
	if code != CodeOK {
		t.Fatalf("with audit: code=%d stderr=%q", code, errOut)
	}
	eventsPath := filepath.Join(repo, ".devsys", "events")
	entries, err := os.ReadDir(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		data, rerr := os.ReadFile(filepath.Join(eventsPath, e.Name()))
		if rerr == nil && strings.Contains(string(data), "workitem_updated") && strings.Contains(string(data), "澄清范围") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no workitem_updated audit event with the reason under %s", eventsPath)
	}
}

// TestArtifactRegisterRequiresAudit: 同上，覆盖 artifact register。
func TestArtifactRegisterRequiresAudit(t *testing.T) {
	gatedProject(t)
	code, _, errOut := run(t, "artifact", "register", "--name", "a.md")
	if code != CodeUsage || !strings.Contains(errOut, "--actor") {
		t.Fatalf("missing audit: code=%d stderr=%q", code, errOut)
	}
	if code, _, errOut = run(t, "artifact", "register", "--name", "a.md", "--actor", "me", "--reason", "coverage"); code != CodeOK {
		t.Fatalf("with audit: code=%d stderr=%q", code, errOut)
	}
}

// TestConfigCheckDeclaresScope: 人类输出点名受检的 4 个元数据档（m18：
// 范围误读 → 输出声明范围）。
func TestConfigCheckDeclaresScope(t *testing.T) {
	gatedProject(t)
	code, out, errOut := run(t, "config", "check")
	if code != CodeOK {
		t.Fatalf("config check: %d %s", code, errOut)
	}
	for _, want := range []string{"project.yaml", "config.yaml", "state/current.yaml", "state/milestones.yaml"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout = %q, want scope with %s", out, want)
		}
	}
}
