package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JAYY513/Workloom/internal/app"
)

// P1: resolveRoot lets commands run from a subdirectory of the project.
func TestResolveRootFromSubdir(t *testing.T) {
	repo, _ := gatedProject(t)
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	code, out, errOut := run(t, "project", "get")
	if code != CodeOK {
		t.Fatalf("project get from subdir: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "demo-project") && !strings.Contains(out, "proj") {
		t.Fatalf("stdout = %q, want project identity", out)
	}
}

// Without any .devsys/ above, the old precondition still fires.
func TestResolveRootWithoutProject(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("DEVSYS_PROJECT_ROOT", "")
	if code, _, _ := run(t, "project", "get"); code != CodePrecondition {
		t.Fatalf("code=%d, want %d", code, CodePrecondition)
	}
}

// wire --check is read-only and always exits 0.
func TestWireCheck(t *testing.T) {
	repo, _ := gatedProject(t)
	t.Chdir(repo)
	code, out, errOut := run(t, "wire", "--check")
	if code != CodeOK {
		t.Fatalf("wire --check: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"go", "git", ".devsys", "schema", "registry", "AGENTS.md", "skill", "mcp"} {
		if !strings.Contains(out, want) {
			t.Errorf("check output misses %q:\n%s", want, out)
		}
	}
}

// A managed skill file that drifted from the generator is reported as stale
// instead of passing on marker presence alone.
func TestWireCheckReportsStaleSkill(t *testing.T) {
	repo, _ := gatedProject(t)
	t.Chdir(repo)
	if code, _, errOut := run(t, "wire", "--skill"); code != CodeOK {
		t.Fatalf("wire --skill: code=%d stderr=%q", code, errOut)
	}
	p := filepath.Join(repo, ".agents", "skills", "devsys", "references", "cli.md")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append(data, []byte("\ndrift\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(t, "wire", "--check")
	if code != CodeOK || !strings.Contains(out, "stale") {
		t.Fatalf("check over a drifted skill: code=%d out=%q", code, out)
	}
	if code, _, errOut := run(t, "wire", "--skill"); code != CodeOK {
		t.Fatalf("refresh: code=%d stderr=%q", code, errOut)
	}
	if code, out, _ := run(t, "wire", "--check"); code != CodeOK || !strings.Contains(out, "[v] skill") {
		t.Fatalf("check after refresh: code=%d out=%q", code, out)
	}
}

// wire --print-mcp prints the stdio command; unknown harness is usage.
func TestWirePrintMCP(t *testing.T) {
	repo, _ := gatedProject(t)
	t.Chdir(repo)
	code, out, errOut := run(t, "wire", "--print-mcp", "codex")
	if code != CodeOK {
		t.Fatalf("print-mcp codex: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "mcp") || !strings.Contains(out, "serve") {
		t.Fatalf("snippet = %q, want the serve command", out)
	}
	if !strings.Contains(out, "--tier") || !strings.Contains(out, "core") {
		t.Fatalf("snippet = %q, want an explicit --tier core", out)
	}
	if !strings.Contains(out, "run_fail") || !strings.Contains(out, "standard") {
		t.Fatalf("snippet = %q, want the core-boundary note", out)
	}
	if code, _, _ := run(t, "wire", "--print-mcp", "nope"); code != CodeUsage {
		t.Fatalf("unknown harness: code=%d, want %d", code, CodeUsage)
	}
}

// wire --skill writes the three files and is a no-op the second time;
// hand-written files without the marker survive.
func TestWireSkill(t *testing.T) {
	repo, _ := gatedProject(t)
	t.Chdir(repo)
	code, out, errOut := run(t, "wire", "--skill")
	if code != CodeOK {
		t.Fatalf("wire --skill: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "SKILL.md") {
		t.Fatalf("stdout = %q, want the written paths", out)
	}
	for _, rel := range []string{".agents/skills/devsys/SKILL.md", ".agents/skills/devsys/references/cli.md", ".agents/skills/devsys/references/troubleshooting.md"} {
		data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if !strings.Contains(string(data), "<!-- devsys-skill -->") {
			t.Fatalf("%s lacks the marker", rel)
		}
	}
	code, out, _ = run(t, "wire", "--skill")
	if code != CodeOK || !strings.Contains(out, "already installed") {
		t.Fatalf("second skill: code=%d out=%q", code, out)
	}
	// Freshly written skill reports green before the hand-written
	// replacement below.
	code, out, _ = run(t, "wire", "--check")
	if code != CodeOK || !strings.Contains(out, "[v] skill") {
		t.Fatalf("check after skill: code=%d out=%q", code, out)
	}
	// Hand-written file without the marker is preserved, and --check says so.
	hand := "# hand skill\n"
	p := filepath.Join(repo, ".agents", "skills", "devsys", "SKILL.md")
	if err := os.WriteFile(p, []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := run(t, "wire", "--skill"); code != CodeOK {
		t.Fatalf("skill over handwritten: code=%d stderr=%q", code, errOut)
	}
	if got, _ := os.ReadFile(p); string(got) != hand {
		t.Fatalf("hand-written skill overwritten:\n%s", got)
	}
	code, out, _ = run(t, "wire", "--check")
	if code != CodeOK || !strings.Contains(out, "hand-written") {
		t.Fatalf("check over handwritten: code=%d out=%q", code, out)
	}
}

// The default wire block now references the skill.
func TestWireBlockReferencesSkill(t *testing.T) {
	repo, _ := gatedProject(t)
	if code, _, errOut := run(t, "wire"); code != CodeOK {
		t.Fatalf("wire: code=%d stderr=%q", code, errOut)
	}
	if got := readAgents(t, repo); !strings.Contains(got, ".agents/skills/devsys/SKILL.md") {
		t.Fatalf("wire block lacks the skill reference:\n%s", got)
	}
}

// Skill constants expose the marker for the repo copy to match.
func TestSkillBodyMarker(t *testing.T) {
	if !strings.Contains(app.SkillMainBody(), "<!-- devsys-skill -->") {
		t.Fatal("skill body lacks the marker")
	}
}
