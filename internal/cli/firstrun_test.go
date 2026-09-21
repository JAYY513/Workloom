package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JAYY513/Workloom/internal/next"
)

func TestInitPrintsNextSteps(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "MyProj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)
	code, out, errOut := run(t, "init")
	if code != CodeOK {
		t.Fatalf("init: code=%d stderr=%s", code, errOut)
	}
	for _, want := range []string{"next:", "wire --skill", "workitem create", "workspace view"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, want %q", out, want)
		}
	}
}

func TestEmptyProjectNextAndSession(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "Empty")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)
	if code, _, errOut := run(t, "init"); code != CodeOK {
		t.Fatalf("init: %s", errOut)
	}
	code, out, errOut := run(t, "--json", "workitem", "list")
	if code != CodeOK {
		t.Fatalf("workitem list: %s", errOut)
	}
	if !strings.Contains(out, `"items":[]`) && !strings.Contains(out, `"items": []`) {
		t.Fatalf("workitem list json = %s, want items:[]", out)
	}
	code, out, errOut = run(t, "next")
	if code != CodeOK {
		t.Fatalf("next: %s", errOut)
	}
	if strings.Contains(out, "state unavailable") {
		t.Fatalf("next claimed state unavailable:\n%s", out)
	}
	if !strings.Contains(out, "CONCERNS") || !strings.Contains(out, "empty_project") || !strings.Contains(out, "workitem create") {
		t.Fatalf("next = %s, want CONCERNS empty_project with create", out)
	}
	code, out, errOut = run(t, "session", "start")
	if code != CodeOK {
		t.Fatalf("session start: %s", errOut)
	}
	if strings.Contains(out, "state unavailable") {
		t.Fatalf("session claimed state unavailable:\n%s", out)
	}
	if !strings.Contains(out, next.CreateWorkitemCommand) && !strings.Contains(out, "workitem create") {
		t.Fatalf("session = %s, want the create command", out)
	}
}

func TestWirePrintMCPClaudeJSON(t *testing.T) {
	repo, _ := gatedProject(t)
	t.Chdir(repo)
	code, out, errOut := run(t, "wire", "--print-mcp", "claude")
	if code != CodeOK {
		t.Fatalf("print-mcp claude: code=%d stderr=%q", code, errOut)
	}
	start := strings.Index(out, "{")
	end := strings.LastIndex(out, "}")
	if start < 0 || end <= start {
		t.Fatalf("no JSON object in %q", out)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out[start:end+1]), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
}

func TestWorkitemUpdateRejectsUnknownHarness(t *testing.T) {
	dispatchProject(t, "", "one")
	code, _, errOut := run(t, "workitem", "update", "--id", "WLM-1", "--assigned-harness", "nope", "--actor", "tester", "--reason", "coverage")
	if code != CodeUsage {
		t.Fatalf("code=%d stderr=%q, want usage", code, errOut)
	}
	if !strings.Contains(errOut, "unknown harness") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestInstallPs1WritesUserPATHOnly(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if strings.Contains(body, `"$env:Path;$dest"`) || strings.Contains(body, "$env:Path;$dest") {
		t.Fatal("install.ps1 still concatenates the session PATH into the User PATH")
	}
	if !strings.Contains(body, `GetEnvironmentVariable("Path", "User")`) && !strings.Contains(body, "GetEnvironmentVariable('Path', 'User')") {
		t.Fatal("install.ps1 must read the User PATH")
	}
}
