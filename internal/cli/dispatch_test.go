package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dispatchProject is a project with a policy, a dispatch command and one ready
// work item per title.
func dispatchProject(t *testing.T, concurrency string, titles ...string) string {
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
	policy := "---\nid: conc\nname: 并发策略\nversion: 1\nsteps:\n  - id: implement\n    type: execute\n    required: true\n"
	if concurrency != "" {
		policy += concurrency
	}
	policy += "limits:\n  max_attempts: 3\n---\n实现：{{workitem.title}}\n"
	if err := os.WriteFile(filepath.Join(repo, ".devsys", "workflows", "conc.md"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(repo, ".devsys", "config.yaml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	config = append(config, []byte("dispatch_command: \"echo dispatched\"\n")...)
	if err := os.WriteFile(configPath, config, 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "add", "-A")
	gitCmd(t, repo, "commit", "-q", "-m", "fixture")
	for i, title := range titles {
		code, out, errOut := run(t, "workitem", "create", "--title", title, "--actor", "test", "--reason", "fixture", "--priority", "5")
		if code != CodeOK {
			t.Fatalf("workitem create: code=%d stderr=%q", code, errOut)
		}
		id := strings.Fields(out)[0]
		if code, _, errOut := run(t, "workflow", "start", "--id", id, "--policy", "conc", "--actor", "test", "--reason", "fixture"); code != CodeOK {
			t.Fatalf("workflow start: code=%d stderr=%q", code, errOut)
		}
		for _, target := range []string{"backlog", "ready"} {
			if code, _, errOut := run(t, "workitem", "transition", "--id", id, "--to", target, "--actor", "test", "--reason", "fixture"); code != CodeOK {
				t.Fatalf("transition %s: code=%d stderr=%q", target, code, errOut)
			}
		}
		_ = i
	}
	return repo
}

func runIDs(t *testing.T, repo string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repo, ".devsys", "runs"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".yaml") {
			out = append(out, strings.TrimSuffix(entry.Name(), ".yaml"))
		}
	}
	return out
}

// dispatch --dry-run reports the plan without claiming, preparing or starting
// anything: the read-only preview of a tick.
func TestDispatchDryRunPlansWithoutChanges(t *testing.T) {
	repo := dispatchProject(t, "concurrency:\n  global: 2\n", "one", "two")
	code, out, errOut := run(t, "dispatch", "--dry-run", "--actor", "ops", "--reason", "preview")
	if code != CodeOK {
		t.Fatalf("dispatch --dry-run: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "dry-run") || !strings.Contains(out, "in flight: 0") {
		t.Fatalf("stdout = %q, want the plan summary", out)
	}
	if ids := runIDs(t, repo); len(ids) != 0 {
		t.Fatalf("dry run created runs: %v", ids)
	}
	code, out, _ = run(t, "--json", "dispatch", "--dry-run", "--actor", "ops", "--reason", "preview")
	if code != CodeOK {
		t.Fatalf("dispatch --dry-run --json: code=%d", code)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("json: %v (%q)", err, out)
	}
	if report["dry_run"] != true {
		t.Fatalf("report = %v, want dry_run true", report)
	}
	plan, _ := report["plan"].(map[string]any)
	start, _ := plan["start"].([]any)
	if len(start) != 2 {
		t.Fatalf("planned starts = %v, want both candidates", start)
	}
}

// Without a dispatch command the tick refuses and names the key; nothing is
// claimed or started.
func TestDispatchWithoutCommandRefuses(t *testing.T) {
	repo := dispatchProject(t, "", "one")
	configPath := filepath.Join(repo, ".devsys", "config.yaml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(strings.Replace(string(config), "dispatch_command: \"echo dispatched\"\n", "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "dispatch", "--actor", "ops", "--reason", "tick")
	if code != CodePrecondition {
		t.Fatalf("dispatch without a command: code=%d stderr=%q, want %d", code, errOut, CodePrecondition)
	}
	if !strings.Contains(errOut, "dispatch_command") {
		t.Fatalf("stderr = %q, want it to name the missing key", errOut)
	}
	if ids := runIDs(t, repo); len(ids) != 0 {
		t.Fatalf("runs were created: %v", ids)
	}
}

// The tick needs an operator and a reason, like every other write command.
func TestDispatchUsage(t *testing.T) {
	dispatchProject(t, "", "one")
	if code, _, _ := run(t, "dispatch", "--reason", "tick"); code != CodeUsage {
		t.Fatalf("missing actor: code=%d, want %d", code, CodeUsage)
	}
	if code, _, _ := run(t, "dispatch", "--actor", "ops"); code != CodeUsage {
		t.Fatalf("missing reason: code=%d, want %d", code, CodeUsage)
	}
	if code, _, _ := run(t, "dispatch", "--once", "--watch", "--actor", "ops", "--reason", "tick"); code != CodeUsage {
		t.Fatalf("--once with --watch: code=%d, want %d", code, CodeUsage)
	}
}

// Read-only commands never dispatch: the scheduling state and the run list are
// untouched by next, doctor and status.
func TestReadOnlyCommandsDoNotDispatch(t *testing.T) {
	repo := dispatchProject(t, "", "one")
	before := len(runIDs(t, repo))
	for _, args := range [][]string{
		{"next"}, {"doctor"}, {"project", "status"},
	} {
		if code, _, _ := run(t, args...); code != CodeOK && code != CodePrecondition {
			t.Fatalf("%v: code=%d", args, code)
		}
	}
	if after := len(runIDs(t, repo)); after != before {
		t.Fatalf("read-only commands created runs: %d -> %d", before, after)
	}
	code, out, _ := run(t, "--json", "workitem", "get", "WLM-1")
	if code != CodeOK {
		t.Fatalf("workitem get: code=%d", code)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	item, _ := view["item"].(map[string]any)
	if item["status"] != "ready" {
		t.Fatalf("work item status = %v, want it untouched (ready)", item["status"])
	}
}

// A work item can name the harness dispatch should drive: without this the
// assignment would only be settable by hand-editing managed state.
func TestWorkitemAssignedHarnessRoundTrip(t *testing.T) {
	dispatchProject(t, "", "one")
	if code, _, errOut := run(t, "workitem", "update", "--id", "WLM-1", "--assigned-harness", "codex", "--assigned-agent", "agent-7"); code != CodeOK {
		t.Fatalf("workitem update: code=%d stderr=%q", code, errOut)
	}
	code, out, errOut := run(t, "--json", "workitem", "get", "WLM-1")
	if code != CodeOK {
		t.Fatalf("workitem get: %q", errOut)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	item, _ := view["item"].(map[string]any)
	if item["assigned_harness"] != "codex" || item["assigned_agent"] != "agent-7" {
		t.Fatalf("item = %v, want the assignment recorded", item)
	}
	// An empty value clears it, so "no harness" has one representation.
	if code, _, errOut := run(t, "workitem", "update", "--id", "WLM-1", "--assigned-harness", ""); code != CodeOK {
		t.Fatalf("clearing: code=%d stderr=%q", code, errOut)
	}
	code, out, _ = run(t, "--json", "workitem", "get", "WLM-1")
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	item, _ = view["item"].(map[string]any)
	if item["assigned_harness"] != nil {
		t.Fatalf("assigned_harness = %v, want nil after clearing", item["assigned_harness"])
	}
}
