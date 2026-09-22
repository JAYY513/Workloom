package app

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONSetEntryPreservesBytesAndInsertsFirst(t *testing.T) {
	doc := []byte("{\n  \"$schema\": \"x\",\n  \"mcp\": {\n    \"context7\": {\n      \"type\": \"remote\"\n    }\n  }\n}\n")
	out, inserted, err := jsonSetEntry(doc, "mcp", "devsys", []byte(`{"type":"local"}`), false)
	if err != nil {
		t.Fatalf("jsonSetEntry: %v", err)
	}
	if !inserted {
		t.Fatal("inserted = false, want true")
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v\n%s", err, out)
	}
	mcp := parsed["mcp"].(map[string]any)
	if _, ok := mcp["devsys"]; !ok {
		t.Fatalf("devsys missing from mcp: %v", mcp)
	}
	// Every pre-existing byte outside the inserted span must survive verbatim.
	wantExisting := "\"context7\": {\n      \"type\": \"remote\"\n    }"
	if !strings.Contains(string(out), wantExisting) {
		t.Fatalf("existing entry was rewritten:\n%s", out)
	}
	if !strings.HasPrefix(string(out), "{\n  \"$schema\": \"x\",") {
		t.Fatalf("prefix changed:\n%s", out)
	}
}

func TestJSONSetEntryIdempotent(t *testing.T) {
	doc := []byte("{\n  \"mcp\": {\n    \"devsys\": {\"type\": \"local\"}\n  }\n}")
	out, inserted, err := jsonSetEntry(doc, "mcp", "devsys", []byte(`{"type":"local"}`), false)
	if err != nil {
		t.Fatalf("jsonSetEntry: %v", err)
	}
	if inserted {
		t.Fatal("inserted = true on existing entry")
	}
	if string(out) != string(doc) {
		t.Fatalf("document changed on no-op:\n%s", out)
	}
}

func TestJSONSetEntryMissingObject(t *testing.T) {
	doc := []byte("{\n  \"agent\": {}\n}\n")
	out, inserted, err := jsonSetEntry(doc, "mcp", "devsys", []byte(`{"type":"local"}`), false)
	if err != nil || !inserted {
		t.Fatalf("insert = %v, %v", inserted, err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if _, ok := parsed["mcp"].(map[string]any)["devsys"]; !ok {
		t.Fatalf("mcp.devsys missing:\n%s", out)
	}
}

func TestJSONSetEntryEmptyTopObject(t *testing.T) {
	out, inserted, err := jsonSetEntry([]byte("{}"), "mcpServers", "devsys", []byte(`{"type":"stdio"}`), false)
	if err != nil || !inserted {
		t.Fatalf("insert = %v, %v", inserted, err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if _, ok := parsed["mcpServers"].(map[string]any)["devsys"]; !ok {
		t.Fatalf("mcpServers.devsys missing:\n%s", out)
	}
}

func TestJSONSetEntryEmptyTargetObject(t *testing.T) {
	out, inserted, err := jsonSetEntry([]byte("{\"mcp\": {}}"), "mcp", "devsys", []byte(`{"type":"local"}`), false)
	if err != nil || !inserted {
		t.Fatalf("insert = %v, %v", inserted, err)
	}
	if string(out) != `{"mcp": {"devsys": {"type":"local"}}}` {
		t.Fatalf("unexpected result: %s", out)
	}
}

func TestJSONSetEntryIgnoresEntryKeyInValues(t *testing.T) {
	doc := []byte("{\n  \"mcp\": {\n    \"note\": \"mentions devsys \\\"devsys\\\" in a string\"\n  }\n}")
	out, inserted, err := jsonSetEntry(doc, "mcp", "devsys", []byte(`{"type":"local"}`), false)
	if err != nil || !inserted {
		t.Fatalf("insert = %v, %v", inserted, err)
	}
	if !strings.Contains(string(out), "mentions devsys") {
		t.Fatalf("string value mangled:\n%s", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
}

func TestJSONSetEntryRefusesNonObjectTarget(t *testing.T) {
	doc := []byte("{\n  \"mcp\": null\n}\n")
	if _, _, err := jsonSetEntry(doc, "mcp", "devsys", []byte(`{}`), false); err == nil {
		t.Fatal("expected refusal for non-object mcp")
	}
}

func TestInstallCodexTOMLAppendAndIdempotence(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	existing := "# operator config\nmodel = \"gpt-5\"\n\n[mcp_servers.semble]\ncommand = \"uvx\"\n"
	if err := os.WriteFile(p, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, detail, err := installCodexTOML(p, MCPCommand{Command: "C:/bin/devsys.exe"}, false, false)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v detail=%s", changed, err, detail)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), existing) {
		t.Fatalf("existing content not preserved:\n%s", got)
	}
	if !strings.Contains(string(got), "[mcp_servers.devsys]\ncommand = \"C:/bin/devsys.exe\"\nargs = [\"mcp\",\"serve\",\"--profile\",\"session,executor\",\"--tier\",\"core\"]\n") {
		t.Fatalf("block missing or malformed:\n%s", got)
	}
	changed, _, err = installCodexTOML(p, MCPCommand{Command: "C:/bin/devsys.exe"}, false, false)
	if err != nil || changed {
		t.Fatalf("rerun changed=%v err=%v, want idempotent skip", changed, err)
	}
}

func TestInstallCodexTOMLRefusesInlineTable(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte("mcp_servers = { other = 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := installCodexTOML(p, MCPCommand{Command: "C:/bin/devsys.exe"}, false, true); err == nil {
		t.Fatal("expected refusal for inline mcp_servers table")
	}
}

func TestInstallCodexTOMLForceReplacesTable(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	existing := "model = \"gpt-5\"\n\n[mcp_servers.devsys]\ncommand = \"old\"\n\n[mcp_servers.devsys.env]\nFOO = \"bar\"\n\n[other]\nx = 1\n"
	if err := os.WriteFile(p, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, _, err := installCodexTOML(p, MCPCommand{Command: "C:/bin/new.exe"}, false, false)
	if err != nil || changed {
		t.Fatalf("without force changed=%v err=%v", changed, err)
	}
	if got, _ := os.ReadFile(p); string(got) != existing {
		t.Fatalf("file changed without force:\n%s", got)
	}
	changed, _, err = installCodexTOML(p, MCPCommand{Command: "C:/bin/new.exe"}, false, true)
	if err != nil || !changed {
		t.Fatalf("force changed=%v err=%v", changed, err)
	}
	got, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(got), "model = \"gpt-5\"\n\n") || !strings.Contains(string(got), "[other]\nx = 1\n") {
		t.Fatalf("sibling tables rewritten:\n%s", got)
	}
	if strings.Contains(string(got), "command = \"old\"") || strings.Contains(string(got), "FOO = \"bar\"") {
		t.Fatalf("old devsys table survived:\n%s", got)
	}
	if !strings.Contains(string(got), "command = \"C:/bin/new.exe\"") {
		t.Fatalf("new command missing:\n%s", got)
	}
}

func TestInstallClientJSONRefusesJSONC(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(p, []byte("{\n  // comment\n  \"mcp\": {}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := installClientJSON(p, "mcp", []byte(`{"type":"local"}`), false, true); err == nil {
		t.Fatal("expected refusal for JSONC document")
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "// comment") {
		t.Fatalf("refusal must not rewrite the file:\n%s", got)
	}
}

func TestMCPInstallUserScopeEndToEnd(t *testing.T) {
	svc, _ := newTestService(t)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"gpt-5\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte("{\n  \"mcpServers\": {\n    \"semble\": {\n      \"command\": \"uvx\"\n    }\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opencodeDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(opencodeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opencodeDir, "opencode.json"), []byte("{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"mcp\": {}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	view, err := svc.MCPInstall(t.Context(), MCPInstallRequest{
		Scope:   "user",
		Clients: []string{"codex", "claude", "opencode"},
		HomeDir: home,
		Bin:     "C:/bin/devsys.exe",
		Apply:   true,
		probe:   stubProbe(MCPProbeResult{OK: true, Tools: 19}),
	})
	if err != nil {
		t.Fatalf("MCPInstall: %v", err)
	}
	if view.Probe == nil || !view.Probe.OK {
		t.Fatalf("probe = %+v, want the reported success", view.Probe)
	}
	if len(view.Results) != 3 {
		t.Fatalf("results = %d, want 3", len(view.Results))
	}
	for _, r := range view.Results {
		if r.Status != "installed" {
			t.Errorf("%s: status=%s detail=%s", r.Client, r.Status, r.Detail)
		}
	}
	// Byte-preservation spot checks.
	codex, _ := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if !strings.HasPrefix(string(codex), "model = \"gpt-5\"\n") || !strings.Contains(string(codex), "[mcp_servers.devsys]") {
		t.Fatalf("codex config wrong:\n%s", codex)
	}
	claude, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	if !strings.Contains(string(claude), `"semble": {`) || !strings.Contains(string(claude), `"devsys": {`) {
		t.Fatalf("claude config wrong:\n%s", claude)
	}
	opencode, _ := os.ReadFile(filepath.Join(opencodeDir, "opencode.json"))
	if !strings.Contains(string(opencode), `"$schema": "https://opencode.ai/config.json"`) || !strings.Contains(string(opencode), `"devsys": {`) {
		t.Fatalf("opencode config wrong:\n%s", opencode)
	}
	// Rerun: everything already registered, byte-identical.
	view2, err := svc.MCPInstall(t.Context(), MCPInstallRequest{Scope: "user", Clients: []string{"codex", "claude", "opencode"}, HomeDir: home, Bin: "C:/bin/devsys.exe", Apply: true, probe: stubProbe(MCPProbeResult{OK: true, Tools: 19})})
	if err != nil {
		t.Fatalf("rerun MCPInstall: %v", err)
	}
	for _, r := range view2.Results {
		if r.Status != "skipped" {
			t.Errorf("rerun %s: status=%s, want skipped", r.Client, r.Status)
		}
	}
	if string(mustReadFile(t, filepath.Join(home, ".claude.json"))) != string(claude) {
		t.Fatal("claude.json changed on idempotent rerun")
	}
}

func TestMCPInstallDryRunWritesNothing(t *testing.T) {
	svc, _ := newTestService(t)
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	view, err := svc.MCPInstall(t.Context(), MCPInstallRequest{Scope: "user", Clients: []string{"claude"}, HomeDir: home, Bin: "C:/bin/devsys.exe", DryRun: true})
	if err != nil {
		t.Fatalf("MCPInstall: %v", err)
	}
	if view.Results[0].Status != "planned" {
		t.Fatalf("status = %s, want planned", view.Results[0].Status)
	}
	after, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	if string(after) != string(before) {
		t.Fatal("dry-run wrote to the file")
	}
}

func TestMCPInstallRejectsUnknownClientAndScope(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.MCPInstall(t.Context(), MCPInstallRequest{Clients: []string{"emacs"}}); err == nil {
		t.Fatal("expected usage error for unknown client")
	}
	if _, err := svc.MCPInstall(t.Context(), MCPInstallRequest{Scope: "team"}); err == nil {
		t.Fatal("expected usage error for unknown scope")
	}
}

func TestMCPInstallDefaultDoesNotWrite(t *testing.T) {
	svc, _ := newTestService(t)
	home := t.TempDir()
	path := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	announced := false
	view, err := svc.MCPInstall(t.Context(), MCPInstallRequest{
		Scope:   "user",
		Clients: []string{"claude"},
		HomeDir: home,
		Bin:     "C:/bin/devsys.exe",
		Announce: func(client, p string) {
			announced = client == "claude" && p != ""
		},
	})
	if err != nil {
		t.Fatalf("MCPInstall: %v", err)
	}
	if view.Results[0].Status != "planned" {
		t.Fatalf("status = %s, want planned", view.Results[0].Status)
	}
	if !announced {
		t.Fatal("expected target announcement before a dry-run")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("default install wrote to the file")
	}
}

// A registration that cannot start is reported as such: the config the
// operator asked for is written, but `--apply` does not claim success. Dry
// runs report the same probe and stay read-only.
func TestMCPInstallApplyReportsAProbeFailure(t *testing.T) {
	svc, _ := newTestService(t)
	home := t.TempDir()
	view, err := svc.MCPInstall(t.Context(), MCPInstallRequest{
		Scope: "user", Clients: []string{"claude"}, HomeDir: home, Bin: "C:/bin/devsys.exe", Apply: true,
		probe: stubProbe(MCPProbeResult{Detail: "fork/exec: no such file"}),
	})
	if err == nil {
		t.Fatal("want a failure when the registered command cannot start")
	}
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Class() != KindPrecondition {
		t.Fatalf("error = %v, want a precondition failure", err)
	}
	if view == nil || view.Probe == nil || view.Probe.OK {
		t.Fatalf("view = %+v, want the failed probe reported", view)
	}
	written, readErr := os.ReadFile(filepath.Join(home, ".claude.json"))
	if readErr != nil || !strings.Contains(string(written), `"devsys"`) {
		t.Fatalf("config = %s (err %v), want the registration written", written, readErr)
	}

	dry, err := svc.MCPInstall(t.Context(), MCPInstallRequest{
		Clients: []string{"claude"}, HomeDir: home, Bin: "C:/bin/devsys.exe", DryRun: true,
		probe: stubProbe(MCPProbeResult{Detail: "fork/exec: no such file"}),
	})
	if err != nil {
		t.Fatalf("dry run must stay read-only: %v", err)
	}
	if dry.Probe == nil || dry.Probe.OK {
		t.Fatalf("dry-run probe = %+v", dry.Probe)
	}
}

func TestMCPInstallForceReplacesEntry(t *testing.T) {
	svc, _ := newTestService(t)
	home := t.TempDir()
	path := filepath.Join(home, ".claude.json")
	original := "{\n  \"keep\": 1,\n  \"mcpServers\": {\n    \"other\": {\"command\": \"uvx\"},\n    \"devsys\": {\"command\": \"old\"}\n  }\n}\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	view, err := svc.MCPInstall(t.Context(), MCPInstallRequest{
		Scope: "user", Clients: []string{"claude"}, HomeDir: home, Bin: "C:/bin/new.exe", Apply: true,
		probe: stubProbe(MCPProbeResult{OK: true, Tools: 19}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Results[0].Status != "skipped" {
		t.Fatalf("without force status = %s, want skipped", view.Results[0].Status)
	}
	if got := string(mustReadFile(t, path)); got != original {
		t.Fatalf("file changed without force:\n%s", got)
	}
	view, err = svc.MCPInstall(t.Context(), MCPInstallRequest{
		Scope: "user", Clients: []string{"claude"}, HomeDir: home, Bin: "C:/bin/new.exe", Apply: true, Force: true,
		probe: stubProbe(MCPProbeResult{OK: true, Tools: 19}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Results[0].Status != "installed" {
		t.Fatalf("force status = %s detail=%s", view.Results[0].Status, view.Results[0].Detail)
	}
	got := string(mustReadFile(t, path))
	if !strings.Contains(got, `"keep": 1`) || !strings.Contains(got, `"other": {"command": "uvx"}`) {
		t.Fatalf("siblings rewritten:\n%s", got)
	}
	if !strings.Contains(got, `"command":"C:/bin/new.exe"`) && !strings.Contains(got, `"command": "C:/bin/new.exe"`) {
		t.Fatalf("command not replaced:\n%s", got)
	}
	if strings.Contains(got, `"command": "old"`) || strings.Contains(got, `"command":"old"`) {
		t.Fatalf("old command survived:\n%s", got)
	}
}

// newTestService returns a service rooted in a fresh Git repository, so
// project-scope operations have a realistic root.
func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, out)
	}
	return New(root), root
}

func mustReadFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
