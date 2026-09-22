package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupInstallsAndKeepsExistingPolicy(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "setup-proj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)

	code, out, errOut := run(t, "setup")
	if code != CodeOK {
		t.Fatalf("setup: code=%d stderr=%s stdout=%s", code, errOut, out)
	}
	for _, want := range []string{
		"[v] init:",
		"[v] workflow: installed quick-fix",
		"[v] wire:",
		"[v] wire-check:",
		"[v] config:",
		"[x] mcp:",
		"[v] prime:",
		"[x] blueprint: no blueprint declared",
		"[v] doctor:",
		"Workloom is ready.",
		"project update --blueprint-artifact",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q:\n%s", want, out)
		}
	}
	for _, rel := range []string{
		".devsys/workflows/quick-fix.md",
		"AGENTS.md",
		".agents/skills/devsys/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(rel))); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	policy := filepath.Join(repo, ".devsys", "workflows", "quick-fix.md")
	custom := []byte("# custom policy — do not reset\n")
	if err := os.WriteFile(policy, custom, 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut = run(t, "--json", "setup")
	if code != CodeOK {
		t.Fatalf("second setup: code=%d stderr=%s stdout=%s", code, errOut, out)
	}
	var doc struct {
		OK    bool `json:"ok"`
		Ready bool `json:"ready"`
		Steps []struct {
			Name    string `json:"name"`
			OK      bool   `json:"ok"`
			Skipped bool   `json:"skipped"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if !doc.OK || !doc.Ready {
		t.Fatalf("second setup not ready: %s", out)
	}
	var skipped bool
	for _, step := range doc.Steps {
		if step.Name == "workflow" && step.OK && step.Skipped {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("workflow step not skipped: %s", out)
	}
	got, err := os.ReadFile(policy)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Fatalf("policy rewritten:\n%s", got)
	}
}

func TestSetupUnknownTemplateWritesNothing(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "no-template")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)

	code, _, errOut := run(t, "setup", "--template", "no-such-template")
	if code != CodeUsage {
		t.Fatalf("code=%d, want %d; stderr=%s", code, CodeUsage, errOut)
	}
	if _, err := os.Stat(filepath.Join(repo, ".devsys")); !os.IsNotExist(err) {
		t.Fatalf(".devsys created on unknown template: %v", err)
	}
}
