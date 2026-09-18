package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// roundProject is a project with a policy (hooks and limits included), one
// work item, one run and a first commit — worktree creation needs a HEAD.
func roundProject(t *testing.T, policy string) (string, string, string) {
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
	if policy != "" {
		if err := os.WriteFile(filepath.Join(repo, ".devsys", "workflows", "gated.md"), []byte(policy), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitCmd(t, repo, "add", "-A")
	gitCmd(t, repo, "commit", "-q", "-m", "fixture")

	code, out, errOut := run(t, "workitem", "create", "--title", "round target", "--actor", "test", "--reason", "fixture")
	if code != CodeOK {
		t.Fatalf("workitem create: code=%d stderr=%q", code, errOut)
	}
	workitemID := strings.Fields(out)[0]
	if policy != "" {
		if code, _, errOut := run(t, "workflow", "start", "--id", workitemID, "--policy", "gated", "--actor", "test", "--reason", "fixture"); code != CodeOK {
			t.Fatalf("attach workflow: code=%d stderr=%q", code, errOut)
		}
	}
	code, out, errOut = run(t, "run", "create", "--workitem", workitemID, "--actor", "test", "--reason", "fixture")
	if code != CodeOK {
		t.Fatalf("run create: code=%d stderr=%q", code, errOut)
	}
	return repo, strings.Fields(out)[0], workitemID
}

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// echoArgv prints one environment variable, so the test can see what the
// attempt was handed.
func echoArgv(name string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", "echo %" + name + "%"}
	}
	return []string{"sh", "-c", "echo $" + name}
}

const roundPolicy = `---
id: gated
name: 多轮策略
version: 1
steps:
  - id: implement
    type: execute
    required: true
  - id: verify
    type: verify
    required: true
limits:
  max_attempts: 3
---
按 {{step}} 步骤实现：{{workitem.title}}
`

// The first round is full: the prompt file carries the rendered policy body and
// the child sees where it is.
func TestRunExecFirstRoundIsFull(t *testing.T) {
	repo, runID, _ := roundProject(t, roundPolicy)
	code, out, errOut := run(t, "run", "exec", "--id", runID, "--actor", "t", "--reason", "r", "--", "git", "--version")
	if code != CodeOK {
		t.Fatalf("run exec: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "round=1(full)") || !strings.Contains(out, "status=succeeded") {
		t.Fatalf("stdout = %q, want the round and status summary", out)
	}
	// A clean round does not end the attempt: the session may continue (§4.8).
	if code, out, errOut := run(t, "--json", "run", "get", runID); code != CodeOK || !strings.Contains(out, `"status":"running"`) {
		t.Fatalf("run status after a clean round: code=%d out=%q stderr=%q", code, out, errOut)
	}
	promptPath := filepath.Join(repo, ".devsys", "local", "runs", runID, "round-1.md")
	body, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("read prompt file: %v", err)
	}
	for _, want := range []string{"## 工作流策略正文", "按 implement 步骤实现：round target", "## 汇报协议"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("round 1 prompt misses %q:\n%s", want, body)
		}
	}
	// The child was handed the prompt file of the round it is running.
	argv := append([]string{"run", "exec", "--id", runID, "--actor", "t", "--reason", "r", "--"}, echoArgv("DEVSYS_PROMPT_FILE")...)
	code, out, errOut = run(t, argv...)
	if code != CodeOK {
		t.Fatalf("run exec (env): code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "round-2.md") {
		t.Fatalf("stdout = %q, want the prompt file the attempt was handed", out)
	}
}

// The second round is a continuation: it does not resend the brief, and the
// stream records both rounds. It is possible because a clean round leaves the
// run running (the attempt ends with run complete).
func TestRunExecSecondRoundIsContinuation(t *testing.T) {
	repo, runID, workitemID := roundProject(t, roundPolicy)
	if code, _, errOut := run(t, "run", "exec", "--id", runID, "--actor", "t", "--reason", "r", "--", "git", "--version"); code != CodeOK {
		t.Fatalf("first round: code=%d stderr=%q", code, errOut)
	}
	// Something changed since round 1: the continuation must carry it.
	if code, _, errOut := run(t, "workitem", "comment", "--id", workitemID, "--actor", "owner", "--text", "记得加指标"); code != CodeOK {
		t.Fatalf("comment: code=%d stderr=%q", code, errOut)
	}
	code, out, errOut := run(t, "run", "exec", "--id", runID, "--actor", "t", "--reason", "r", "--", "git", "--version")
	if code != CodeOK {
		t.Fatalf("second round: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "round=2(continuation)") {
		t.Fatalf("stdout = %q, want round 2 as a continuation", out)
	}
	body, err := os.ReadFile(filepath.Join(repo, ".devsys", "local", "runs", runID, "round-2.md"))
	if err != nil {
		t.Fatalf("read continuation prompt: %v", err)
	}
	text := string(body)
	if strings.Contains(text, "## 工作流策略正文") || strings.Contains(text, "按 implement 步骤实现") {
		t.Fatalf("continuation resent the brief:\n%s", text)
	}
	for _, want := range []string{"## 上一轮以来的变化", "记得加指标", "## 未完成项", "待完成步骤：verify"} {
		if !strings.Contains(text, want) {
			t.Fatalf("continuation misses %q:\n%s", want, text)
		}
	}
	records := readStream(t, repo, runID)
	var rounds []map[string]any
	for _, rec := range records {
		if rec["type"] == "round" {
			rounds = append(rounds, rec)
		}
	}
	if len(rounds) != 2 || rounds[0]["mode"] != "full" || rounds[1]["mode"] != "continuation" {
		t.Fatalf("round records = %v, want full then continuation", rounds)
	}
	if rounds[1]["prompt_hash"] == rounds[0]["prompt_hash"] {
		t.Fatal("both rounds recorded the same prompt hash")
	}
}

// A replay of a recorded round reproduces its hash: the stored hash is the
// only proof of what the agent received.
func TestRunPromptReplaysRecordedRounds(t *testing.T) {
	repo, runID, _ := roundProject(t, roundPolicy)
	if code, _, errOut := run(t, "run", "exec", "--id", runID, "--actor", "t", "--reason", "r", "--", "git", "--version"); code != CodeOK {
		t.Fatalf("round 1: %q", errOut)
	}
	code, out, errOut := run(t, "--json", "run", "prompt", "--id", runID, "--round", "1")
	if code != CodeOK {
		t.Fatalf("run prompt: code=%d stderr=%q", code, errOut)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("json: %v (%q)", err, out)
	}
	if view["mode"] != "full" || view["round"] != float64(1) {
		t.Fatalf("view = %v, want the full first round", view)
	}
	replayed, _ := view["hash"].(string)
	var recorded string
	for _, rec := range readStream(t, repo, runID) {
		if rec["type"] == "round" {
			recorded, _ = rec["prompt_hash"].(string)
		}
	}
	if replayed == "" || replayed != recorded {
		t.Fatalf("replayed hash %q != recorded hash %q", replayed, recorded)
	}
}

// Exceeding the policy's round bound is refused before anything runs.
func TestRunExecRefusesBeyondRoundCap(t *testing.T) {
	policy := strings.Replace(roundPolicy, "max_attempts: 3", "max_attempts: 1", 1)
	repo, runID, _ := roundProject(t, policy)
	if code, _, errOut := run(t, "run", "exec", "--id", runID, "--actor", "t", "--reason", "r", "--", "git", "--version"); code != CodeOK {
		t.Fatalf("round 1: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut := run(t, "run", "exec", "--id", runID, "--actor", "t", "--reason", "r", "--", "git", "--version")
	if code != CodePrecondition {
		t.Fatalf("round 2 beyond the cap: code=%d stderr=%q, want %d", code, errOut, CodePrecondition)
	}
	if !strings.Contains(errOut, "max_attempts") {
		t.Fatalf("stderr = %q, want the round bound named", errOut)
	}
	records := readStream(t, repo, runID)
	starts := 0
	for _, rec := range records {
		if rec["type"] == "start" {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("stream has %d start records, want only the first round's", starts)
	}
	// The attempt is still open, so completion is what ends it.
	if code, _, errOut := run(t, "run", "complete", "--id", runID, "--actor", "t", "--reason", "done"); code != CodeOK {
		t.Fatalf("run complete: code=%d stderr=%q", code, errOut)
	}
	if code, _, errOut := run(t, "run", "exec", "--id", runID, "--actor", "t", "--reason", "r", "--", "git", "--version"); code != CodePrecondition {
		t.Fatalf("exec after completion: code=%d stderr=%q", code, errOut)
	}
}

// run complete|fail|cancel record the terminal status under the version guard.
func TestRunFinishCommands(t *testing.T) {
	_, runID, _ := roundProject(t, roundPolicy)
	code, out, errOut := run(t, "--json", "run", "get", runID)
	if code != CodeOK {
		t.Fatalf("run get: %q", errOut)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	version, _ := view["version"].(string)

	code, _, errOut = run(t, "run", "complete", "--id", runID, "--expect", strings.Repeat("0", 64), "--actor", "t", "--reason", "r")
	if code == CodeOK {
		t.Fatalf("a stale expect was accepted (stderr=%q)", errOut)
	}
	code, out, errOut = run(t, "run", "complete", "--id", runID, "--expect", version, "--actor", "t", "--reason", "acceptance met")
	if code != CodeOK {
		t.Fatalf("run complete: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "succeeded") {
		t.Fatalf("stdout = %q, want the terminal status", out)
	}
	code, _, errOut = run(t, "run", "cancel", "--id", runID, "--actor", "t", "--reason", "too late")
	if code != CodePrecondition {
		t.Fatalf("ending a finished run: code=%d stderr=%q, want %d", code, errOut, CodePrecondition)
	}
	if !strings.Contains(errOut, "already ended") {
		t.Fatalf("stderr = %q, want it to say the run already ended", errOut)
	}
}

// An explicit --round is only accepted when it is the round the stream says is
// next: replaying a round would refire a full prompt and slip past the bound.
func TestRunExecRejectsAReplayedRound(t *testing.T) {
	_, runID, _ := roundProject(t, roundPolicy)
	if code, _, errOut := run(t, "run", "exec", "--id", runID, "--actor", "t", "--reason", "r", "--", "git", "--version"); code != CodeOK {
		t.Fatalf("round 1: %q", errOut)
	}
	code, _, errOut := run(t, "run", "exec", "--id", runID, "--round", "1", "--actor", "t", "--reason", "r", "--", "git", "--version")
	if code != CodePrecondition {
		t.Fatalf("replaying round 1: code=%d stderr=%q, want %d", code, errOut, CodePrecondition)
	}
	if !strings.Contains(errOut, "round 2") {
		t.Fatalf("stderr = %q, want it to name the round the session is at", errOut)
	}
	// The round the stream asks for is accepted.
	if code, out, errOut := run(t, "run", "exec", "--id", runID, "--round", "2", "--actor", "t", "--reason", "r", "--", "git", "--version"); code != CodeOK {
		t.Fatalf("round 2: code=%d stderr=%q out=%q", code, errOut, out)
	}
}
