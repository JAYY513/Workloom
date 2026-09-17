package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

	// Three located defects: a type error, an unknown key and a required field
	// that is gone.
	broken := "# c\nschema_version: 1\nid: 42\nupdated_at: 2026-01-01\n"
	if err := os.WriteFile(filepath.Join(repo, ".devsys", "project.yaml"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut = run(t, "config", "check")
	if code != CodeInvalid {
		t.Fatalf("code=%d, want %d (stderr=%s)", code, CodeInvalid, errOut)
	}
	for _, want := range []string{
		"project.yaml:3: id: expected string, got !!int",
		"project.yaml:4: updated_at: unknown key",
		"project.yaml:2: name: required",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q:\n%s", want, errOut)
		}
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
