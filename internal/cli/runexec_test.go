package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/app"
)

// execProject initializes a project with one work item and one run and returns
// the repository root and the run id.
func execProject(t *testing.T) (string, string) {
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
	code, out, errOut := run(t, "workitem", "create", "--title", "exec target", "--actor", "test", "--reason", "fixture")
	if code != CodeOK {
		t.Fatalf("workitem create: code=%d stderr=%q", code, errOut)
	}
	workitemID := strings.Fields(out)[0]
	code, out, errOut = run(t, "run", "create", "--workitem", workitemID, "--actor", "test", "--reason", "fixture")
	if code != CodeOK {
		t.Fatalf("run create: code=%d stderr=%q", code, errOut)
	}
	return repo, strings.Fields(out)[0]
}

// readStream parses the run's event stream (.devsys/runs/<id>.jsonl).
func readStream(t *testing.T, repo, runID string) []map[string]any {
	t.Helper()
	path := filepath.Join(repo, ".devsys", "runs", runID+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	return parseRecords(t, string(data))
}

func parseRecords(t *testing.T, body string) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("stream record %q: %v", line, err)
		}
		records = append(records, rec)
	}
	return records
}

// readEventTypes collects the (type, subject id) pairs of the project's events.
func readEventTypes(t *testing.T, repo string) map[string]string {
	t.Helper()
	dir := filepath.Join(repo, ".devsys", "events")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	out := map[string]string{}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, rec := range parseRecords(t, string(data)) {
			subject, _ := rec["subject"].(map[string]any)
			out[fmt.Sprint(rec["type"])] = fmt.Sprint(subject["id"])
		}
	}
	return out
}

// readEventContents returns the content of every event of one type.
func readEventContents(t *testing.T, repo, wantType string) []string {
	t.Helper()
	dir := filepath.Join(repo, ".devsys", "events")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	var out []string
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, rec := range parseRecords(t, string(data)) {
			if fmt.Sprint(rec["type"]) == wantType {
				out = append(out, fmt.Sprint(rec["content"]))
			}
		}
	}
	return out
}

// runView reads a run through the JSON envelope.
func runView(t *testing.T, runID string) map[string]any {
	t.Helper()
	code, out, errOut := run(t, "--json", "run", "get", runID)
	if code != CodeOK {
		t.Fatalf("run get: code=%d stderr=%q", code, errOut)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("run get json: %v (%q)", err, out)
	}
	return view
}

// sleepArgv is a command that keeps running until it is killed.
func sleepArgv(seconds int) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", fmt.Sprintf("ping -n %d 127.0.0.1 >nul", seconds)}
	}
	return []string{"sh", "-c", fmt.Sprintf("sleep %d", seconds)}
}

// TestRunExecStreamsAndRecords is the milestone acceptance: one command runs,
// its output is visible and lands in the run's event stream, and the run keeps
// the evidence.
func TestRunExecStreamsAndRecords(t *testing.T) {
	repo, runID := execProject(t)
	code, out, errOut := run(t, "run", "exec", "--id", runID, "--actor", "tester", "--reason", "smoke", "--", "git", "--version")
	if code != CodeOK {
		t.Fatalf("run exec: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "git version") {
		t.Fatalf("stdout = %q, want the command output mirrored", out)
	}
	records := readStream(t, repo, runID)
	byType := func(kind string) []map[string]any {
		var out []map[string]any
		for _, rec := range records {
			if rec["type"] == kind {
				out = append(out, rec)
			}
		}
		return out
	}
	// The stream is the ordered evidence of the round: the phase it started
	// in, the round bookkeeping, then the attempt itself.
	if first := records[0]; first["type"] != "phase" || first["phase"] != "building_prompt" {
		t.Fatalf("first record = %v, want the building_prompt phase", first)
	}
	rounds := byType("round")
	if len(rounds) != 1 || rounds[0]["n"] != float64(1) || rounds[0]["mode"] != "full" {
		t.Fatalf("round records = %v, want one full first round", rounds)
	}
	if hash, _ := rounds[0]["prompt_hash"].(string); len(hash) != 64 {
		t.Fatalf("round prompt hash = %v, want a sha256 digest", rounds[0]["prompt_hash"])
	}
	starts := byType("start")
	if len(starts) != 1 || starts[0]["command"] != "git --version" {
		t.Fatalf("start records = %v, want the command", starts)
	}
	argv, _ := starts[0]["argv"].([]any)
	if len(argv) != 2 || argv[0] != "git" || argv[1] != "--version" {
		t.Fatalf("start record argv = %v, want the command as an array too", starts[0]["argv"])
	}
	outputs := byType("output")
	if len(outputs) != 1 || outputs[0]["stream"] != "stdout" ||
		!strings.Contains(fmt.Sprint(outputs[0]["line"]), "git version") {
		t.Fatalf("output records = %v, want the mirrored stdout line", outputs)
	}
	last := records[len(records)-1]
	if last["type"] != "exit" || last["code"] != float64(0) || last["status"] != app.RunSucceeded {
		t.Fatalf("last record = %v, want an exit record with code 0 and the terminal status", last)
	}
	phases := make([]string, 0, len(records))
	for _, rec := range records {
		if rec["type"] == "phase" {
			phases = append(phases, fmt.Sprint(rec["phase"]))
		}
	}
	want := []string{"building_prompt", "launching_agent", "streaming_turns", "finishing"}
	if strings.Join(phases, ",") != strings.Join(want, ",") {
		t.Fatalf("phases = %v, want %v", phases, want)
	}
	if due, _ := last["continuation_due_at"].(string); due == "" {
		t.Fatalf("exit record = %v, want a continuation due time after a clean exit", last)
	}

	view := runView(t, runID)
	item, _ := view["run"].(map[string]any)
	commands, _ := item["commands"].([]any)
	if len(commands) != 1 || commands[0] != "git --version" {
		t.Fatalf("run commands = %v, want the executed command", commands)
	}
	logs, _ := item["logs"].([]any)
	if len(logs) != 1 || logs[0] != ".devsys/runs/"+runID+".jsonl" {
		t.Fatalf("run logs = %v, want the stream reference", logs)
	}
	events := readEventTypes(t, repo)
	for _, want := range []string{"run_exec_started", "run_exec_finished"} {
		if got, ok := events[want]; !ok || got != runID {
			t.Fatalf("event %s = %q (present=%v), want subject %s", want, got, ok, runID)
		}
	}
	finished := readEventContents(t, repo, "run_exec_finished")
	if len(finished) != 1 || !strings.Contains(finished[0], "command exited 0: git --version") {
		t.Fatalf("finished event content = %v, want the outcome spelled out even on success", finished)
	}
}

// An attempt that never started is recorded symmetrically: the stream gets an
// exit-shaped record, the run carries the note and the event log has a
// finished event, so evidence never dangles.
func TestRunExecStartFailureIsRecorded(t *testing.T) {
	repo, runID := execProject(t)
	code, _, errOut := run(t, "run", "exec", "--id", runID, "--actor", "t", "--reason", "r", "--", "devsys-no-such-binary-xyz")
	if code != CodePrecondition {
		t.Fatalf("code=%d stderr=%q, want %d", code, errOut, CodePrecondition)
	}
	var exit map[string]any
	for _, rec := range readStream(t, repo, runID) {
		if rec["type"] == "exit" {
			exit = rec
		}
	}
	if exit == nil || exit["code"] != float64(-1) || !strings.Contains(fmt.Sprint(exit["text"]), "failed to start") {
		t.Fatalf("exit record = %v, want code -1 with the start failure", exit)
	}
	item, _ := runView(t, runID)["run"].(map[string]any)
	result, _ := item["result"].(map[string]any)
	errors, _ := result["errors"].([]any)
	if len(errors) == 0 || !strings.Contains(fmt.Sprint(errors[0]), "failed to start") {
		t.Fatalf("run result errors = %v, want the start failure note", errors)
	}
	if events := readEventTypes(t, repo); events["run_exec_finished"] != runID {
		t.Fatalf("events = %v, want a run_exec_finished for %s", events, runID)
	}
	// The attempt is over: a run that could not start must not stay running.
	item, _ = runView(t, runID)["run"].(map[string]any)
	if item["status"] != app.RunFailed {
		t.Fatalf("run status = %v, want %s after a failed start", item["status"], app.RunFailed)
	}
}

// A failing command is evidence, not a devsys failure: the operation succeeds,
// the command's own exit code is recorded, and the run carries the note.
func TestRunExecRecordsNonZeroExit(t *testing.T) {
	repo, runID := execProject(t)
	code, out, errOut := run(t, "--json", "run", "exec", "--id", runID, "--actor", "tester", "--reason", "smoke", "--", "git", "rev-parse", "--verify", "definitely-not-a-ref")
	if code != CodeOK {
		t.Fatalf("run exec: code=%d stderr=%q", code, errOut)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("json envelope: %v (%q)", err, out)
	}
	if view["ok"] != true || view["exit_code"] == float64(0) {
		t.Fatalf("view = %v, want ok with a non-zero exit code", view)
	}
	records := readStream(t, repo, runID)
	last := records[len(records)-1]
	if last["type"] != "exit" || last["code"] == float64(0) {
		t.Fatalf("exit record = %v, want the command's non-zero code", last)
	}
	item, _ := runView(t, runID)["run"].(map[string]any)
	result, _ := item["result"].(map[string]any)
	errors, _ := result["errors"].([]any)
	if len(errors) == 0 || !strings.Contains(fmt.Sprint(errors[0]), "command exited") {
		t.Fatalf("run result errors = %v, want a note about the failed command", errors)
	}
}

// Bad input must not create a stream or touch the run.
func TestRunExecUsageAndPrecondition(t *testing.T) {
	repo, runID := execProject(t)
	if code, _, _ := run(t, "run", "exec", "--id", runID, "--actor", "t", "--reason", "r"); code != CodeUsage {
		t.Fatalf("missing command: code=%d, want %d", code, CodeUsage)
	}
	if code, _, _ := run(t, "run", "exec", "--actor", "t", "--reason", "r", "--", "git", "--version"); code != CodeUsage {
		t.Fatalf("missing run id: code=%d, want %d", code, CodeUsage)
	}
	if code, _, _ := run(t, "run", "exec", "--id", "run-19700101-1", "--actor", "t", "--reason", "r", "--", "git", "--version"); code != CodePrecondition {
		t.Fatalf("unknown run: code=%d, want %d", code, CodePrecondition)
	}
	if _, err := os.Stat(filepath.Join(repo, ".devsys", "runs", runID+".jsonl")); !os.IsNotExist(err) {
		t.Fatalf("stream file exists after refused calls: %v", err)
	}
}

// The timeout kills the process tree instead of waiting the command out.
func TestRunExecTimeoutKillsTheCommand(t *testing.T) {
	repo, runID := execProject(t)
	argv := append([]string{"--json", "run", "exec", "--id", runID, "--actor", "tester", "--reason", "timeout", "--timeout", "700ms", "--"}, sleepArgv(30)...)
	start := time.Now()
	code, out, errOut := run(t, argv...)
	elapsed := time.Since(start)
	if code != CodeOK {
		t.Fatalf("run exec: code=%d stderr=%q", code, errOut)
	}
	if elapsed > 15*time.Second {
		t.Fatalf("exec took %v, want the command killed at the timeout", elapsed)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("json envelope: %v (%q)", err, out)
	}
	if view["timed_out"] != true {
		t.Fatalf("view = %v, want timed_out", view)
	}
	records := readStream(t, repo, runID)
	last := records[len(records)-1]
	if last["type"] != "exit" || last["timed_out"] != true {
		t.Fatalf("exit record = %v, want timed_out", last)
	}
}

// Under --json the envelope owns stdout: the command's output is mirrored to
// stderr so a parser never sees anything else.
func TestRunExecJSONKeepsStdoutPure(t *testing.T) {
	_, runID := execProject(t)
	code, out, errOut := run(t, "--json", "run", "exec", "--id", runID, "--actor", "t", "--reason", "r", "--", "git", "--version")
	if code != CodeOK {
		t.Fatalf("run exec: code=%d stderr=%q", code, errOut)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("stdout is not a single JSON document: %v (%q)", err, out)
	}
	if !strings.Contains(errOut, "git version") {
		t.Fatalf("stderr = %q, want the command output mirrored there under --json", errOut)
	}
}

// A torn tail left by an interrupted writer is repaired and recorded, so the
// stream stays appendable and the loss is not silent.
func TestRunExecRepairsTornTail(t *testing.T) {
	repo, runID := execProject(t)
	path := filepath.Join(repo, ".devsys", "runs", runID+".jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"output\",\"line\":\"partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := run(t, "run", "exec", "--id", runID, "--actor", "t", "--reason", "r", "--", "git", "--version"); code != CodeOK {
		t.Fatalf("run exec: code=%d stderr=%q", code, errOut)
	}
	records := readStream(t, repo, runID)
	if records[0]["type"] != "repair" {
		t.Fatalf("first record = %v, want a repair record", records[0])
	}
	if removed, ok := records[0]["removed_bytes"].(float64); !ok || removed <= 0 {
		t.Fatalf("repair record = %v, want the dropped byte count", records[0])
	}
	for _, rec := range records {
		if strings.Contains(fmt.Sprint(rec["line"]), "partial") {
			t.Fatalf("torn record survived: %v", rec)
		}
	}
}
