package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPrimeMatchesSessionStartCompact pins prime as an alias: the payload
// matches `session start --compact` with the same intent.
func TestPrimeMatchesSessionStartCompact(t *testing.T) {
	gatedProject(t)
	wi := newWorkitem(t, "prime orientation task")
	advanceTo(t, wi, "ready")

	code, primeOut, errOut := run(t, "--json", "prime",
		"--harness", "codex", "--agent", "codex-local", "--intent", "continue development")
	if code != CodeOK {
		t.Fatalf("prime: code=%d stderr=%q", code, errOut)
	}
	code, compactOut, errOut := run(t, "--json", "session", "start",
		"--harness", "codex", "--agent", "codex-local", "--intent", "continue development", "--compact")
	if code != CodeOK {
		t.Fatalf("session start --compact: code=%d stderr=%q", code, errOut)
	}
	var prime, compact map[string]any
	if err := json.Unmarshal([]byte(primeOut), &prime); err != nil {
		t.Fatalf("prime payload: %v (%s)", err, primeOut)
	}
	if err := json.Unmarshal([]byte(compactOut), &compact); err != nil {
		t.Fatalf("compact payload: %v (%s)", err, compactOut)
	}
	for _, key := range []string{"project", "current_workitems", "recommended_next_action"} {
		pj, _ := json.Marshal(prime[key])
		cj, _ := json.Marshal(compact[key])
		if string(pj) != string(cj) {
			t.Fatalf("%s differs:\nprime: %s\ncompact: %s", key, pj, cj)
		}
	}
	if !strings.Contains(primeOut, wi) {
		t.Fatalf("prime output misses %s: %s", wi, primeOut)
	}
}

// TestLatestTransitionWalksWithoutVersion pins --latest as the explicit
// spelling of the empty-expect path: no get/copy/paste needed.
func TestLatestTransitionWalksWithoutVersion(t *testing.T) {
	gatedProject(t)
	wi := newWorkitem(t, "latest task")
	if code, _, errOut := run(t, "workitem", "transition", "--id", wi, "--to", "backlog",
		"--actor", "operator", "--reason", "go", "--latest"); code != CodeOK {
		t.Fatalf("transition --latest: code=%d stderr=%q", code, errOut)
	}
	if code, _, errOut := run(t, "workitem", "transition", "--id", wi, "--to", "ready",
		"--actor", "operator", "--reason", "go", "--latest"); code != CodeOK {
		t.Fatalf("transition --latest: code=%d stderr=%q", code, errOut)
	}
	code, out, _ := run(t, "--json", "workitem", "get", wi)
	if code != CodeOK || !strings.Contains(out, `"status":"ready"`) {
		t.Fatalf("get after latest: code=%d out=%s", code, out)
	}
}

// TestLatestAndExpectAreMutuallyExclusive pins the fail-closed pairing.
func TestLatestAndExpectAreMutuallyExclusive(t *testing.T) {
	gatedProject(t)
	wi := newWorkitem(t, "latest exclusivity task")
	version := cliRecordVersion(t, "workitem", wi)
	if code, _, _ := run(t, "workitem", "transition", "--id", wi, "--to", "backlog",
		"--actor", "operator", "--reason", "go", "--expect", version, "--latest"); code != CodeUsage {
		t.Fatalf("code=%d, want %d", code, CodeUsage)
	}
}
