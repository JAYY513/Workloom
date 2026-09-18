package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// `run verify` reports the completion check read-only, and `run complete`
// refuses a completion the check cannot support.
func TestRunVerifyAndCompletionRefusal(t *testing.T) {
	_, runID, workitemID := roundProject(t, roundPolicy)
	// Claim the item first: the attempt is what a completion speaks for, and
	// only a claimed (in_progress) item can be routed to review.
	for _, target := range []string{"backlog", "ready"} {
		if code, _, errOut := run(t, "workitem", "transition", "--id", workitemID, "--to", target, "--actor", "t", "--reason", "fixture"); code != CodeOK {
			t.Fatalf("transition %s: %q", target, errOut)
		}
	}
	if code, _, errOut := run(t, "workitem", "claim", "--id", workitemID, "--owner", "agent", "--reason", "attempt"); code != CodeOK {
		t.Fatalf("workitem claim: %q", errOut)
	}
	if code, _, errOut := run(t, "run", "exec", "--id", runID, "--actor", "t", "--reason", "r", "--", "git", "--version"); code != CodeOK {
		t.Fatalf("run exec: %q", errOut)
	}
	code, out, errOut := run(t, "--json", "run", "verify", "--id", runID)
	if code != CodeOK {
		t.Fatalf("run verify: code=%d stderr=%q", code, errOut)
	}
	var check map[string]any
	if err := json.Unmarshal([]byte(out), &check); err != nil {
		t.Fatalf("json: %v (%q)", err, out)
	}
	if check["advanced"] != false || check["reason"] == "" {
		t.Fatalf("check = %v, want a refusal with a reason (the attempt committed nothing)", check)
	}
	code, _, errOut = run(t, "run", "complete", "--id", runID, "--actor", "agent", "--reason", "done")
	if code != CodePrecondition {
		t.Fatalf("run complete: code=%d stderr=%q, want %d", code, errOut, CodePrecondition)
	}
	if !strings.Contains(errOut, "cannot be marked succeeded") {
		t.Fatalf("stderr = %q, want the refusal explained", errOut)
	}
	code, out, _ = run(t, "--json", "workitem", "get", workitemID)
	if code != CodeOK {
		t.Fatal("workitem get failed")
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	item, _ := view["item"].(map[string]any)
	if item["status"] != "review" {
		t.Fatalf("work item status = %v, want review", item["status"])
	}
	// Forcing without a reviewer is a usage error; with one it completes.
	if code, _, _ := run(t, "run", "complete", "--id", runID, "--actor", "agent", "--reason", "done", "--force"); code != CodeUsage {
		t.Fatalf("force without --by: code=%d, want %d", code, CodeUsage)
	}
	code, out, errOut = run(t, "run", "complete", "--id", runID, "--actor", "alice", "--reason", "reviewed", "--force", "--by", "alice")
	if code != CodeOK {
		t.Fatalf("forced completion: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "succeeded") {
		t.Fatalf("stdout = %q, want the terminal status", out)
	}
}
