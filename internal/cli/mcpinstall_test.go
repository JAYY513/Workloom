package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMCPInstallProjectScope covers the productized registration path:
// `workloom mcp install --apply --scope project` writes .mcp.json /
// opencode.json / .codex/config.toml in the project, is idempotent, and
// refuses unknown clients with a usage error.
func TestMCPInstallProjectScope(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "mcp-proj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)

	if code, _, errOut := run(t, "init"); code != CodeOK {
		t.Fatalf("init: code=%d stderr=%s", code, errOut)
	}

	code, out, errOut := run(t, "mcp", "install", "--apply", "--scope", "project", "--client", "claude", "--client", "opencode", "--client", "codex")
	if code != CodeOK {
		t.Fatalf("mcp install: code=%d stderr=%s stdout=%s", code, errOut, out)
	}
	for _, want := range []string{"target claude:", "target opencode:", "target codex:", "[v] claude:", "[v] opencode:", "[v] codex:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "target claude:") > strings.Index(out, "[v] claude:") {
		t.Fatalf("target path must be printed before the write report:\n%s", out)
	}

	mcpJSON, err := os.ReadFile(filepath.Join(repo, ".mcp.json"))
	if err != nil {
		t.Fatalf(".mcp.json: %v", err)
	}
	if !strings.Contains(string(mcpJSON), `"mcpServers"`) || !strings.Contains(string(mcpJSON), `"devsys"`) {
		t.Fatalf(".mcp.json lacks the devsys registration:\n%s", mcpJSON)
	}
	ocJSON, err := os.ReadFile(filepath.Join(repo, "opencode.json"))
	if err != nil {
		t.Fatalf("opencode.json: %v", err)
	}
	if !strings.Contains(string(ocJSON), `"devsys"`) {
		t.Fatalf("opencode.json lacks the devsys registration:\n%s", ocJSON)
	}
	codexTOML, err := os.ReadFile(filepath.Join(repo, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("codex config: %v", err)
	}
	if !strings.Contains(string(codexTOML), "[mcp_servers.devsys]") {
		t.Fatalf("codex config lacks the devsys table:\n%s", codexTOML)
	}

	// Idempotent rerun: everything skipped, files byte-identical.
	code, out, errOut = run(t, "mcp", "install", "--apply", "--scope", "project")
	if code != CodeOK {
		t.Fatalf("rerun: code=%d stderr=%s", code, errOut)
	}
	if strings.Contains(out, "[v]") || !strings.Contains(out, "[=] claude:") {
		t.Fatalf("rerun did not skip everything:\n%s", out)
	}
	mcpJSON2, _ := os.ReadFile(filepath.Join(repo, ".mcp.json"))
	if string(mcpJSON2) != string(mcpJSON) {
		t.Fatal(".mcp.json changed on idempotent rerun")
	}
}

func TestMCPInstallUnknownClientIsUsageError(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "mcp-bad")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)

	code, _, _ := run(t, "mcp", "install", "--client", "emacs")
	if code != CodeUsage {
		t.Fatalf("code=%d, want usage error %d", code, CodeUsage)
	}
}

func TestMCPInstallJSONShape(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "mcp-json")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)

	code, out, errOut := run(t, "--json", "mcp", "install", "--scope", "project", "--dry-run", "--client", "claude")
	if code != CodeOK {
		t.Fatalf("dry-run: code=%d stderr=%s", code, errOut)
	}
	var doc struct {
		OK      bool `json:"ok"`
		Scope   string
		Results []struct {
			Client string
			Status string
			Path   string
		}
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if !doc.OK || doc.Scope != "project" || len(doc.Results) != 1 {
		t.Fatalf("unexpected envelope: %s", out)
	}
	if doc.Results[0].Status != "planned" {
		t.Fatalf("dry-run status = %s, want planned", doc.Results[0].Status)
	}
	if _, err := os.Stat(filepath.Join(repo, ".mcp.json")); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote .mcp.json")
	}
}

func TestMCPInstallDefaultIsDryRun(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "mcp-dry")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)
	if code, _, errOut := run(t, "init"); code != CodeOK {
		t.Fatalf("init: code=%d stderr=%s", code, errOut)
	}
	code, out, errOut := run(t, "mcp", "install", "--scope", "project", "--client", "claude")
	if code != CodeOK {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, errOut, out)
	}
	if !strings.Contains(out, "target claude:") || !strings.Contains(out, "dry-run:") {
		t.Fatalf("stdout = %s, want target path and dry-run", out)
	}
	if _, err := os.Stat(filepath.Join(repo, ".mcp.json")); !os.IsNotExist(err) {
		t.Fatal("default install wrote .mcp.json")
	}
	code, _, errOut = run(t, "mcp", "install", "--apply", "--dry-run", "--client", "claude")
	if code != CodeUsage {
		t.Fatalf("apply+dry-run code=%d stderr=%s, want usage", code, errOut)
	}
}

func TestMCPInstallForceReplacesExisting(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "mcp-force")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)
	if code, _, errOut := run(t, "init"); code != CodeOK {
		t.Fatalf("init: code=%d stderr=%s", code, errOut)
	}
	original := "{\n  \"keep\": true,\n  \"mcpServers\": {\n    \"devsys\": {\"command\": \"old\"}\n  }\n}\n"
	if err := os.WriteFile(filepath.Join(repo, ".mcp.json"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "mcp", "install", "--apply", "--scope", "project", "--client", "claude")
	if code != CodeOK {
		t.Fatalf("apply: code=%d stderr=%s stdout=%s", code, errOut, out)
	}
	if !strings.Contains(out, "[=] claude:") {
		t.Fatalf("existing entry was not skipped:\n%s", out)
	}
	got, _ := os.ReadFile(filepath.Join(repo, ".mcp.json"))
	if string(got) != original {
		t.Fatalf("file changed without --force:\n%s", got)
	}
	code, out, errOut = run(t, "mcp", "install", "--apply", "--force", "--scope", "project", "--client", "claude")
	if code != CodeOK {
		t.Fatalf("force: code=%d stderr=%s stdout=%s", code, errOut, out)
	}
	if strings.Index(out, "target claude:") > strings.Index(out, "[v] claude:") {
		t.Fatalf("target path must precede the write report:\n%s", out)
	}
	got, _ = os.ReadFile(filepath.Join(repo, ".mcp.json"))
	if !strings.Contains(string(got), `"keep": true`) || strings.Contains(string(got), `"command": "old"`) || strings.Contains(string(got), `"command":"old"`) {
		t.Fatalf("force rewrite wrong:\n%s", got)
	}
}
