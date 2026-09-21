package cli

// Tests for the external-review fix batch (installation/usage review):
//   - `workitem create --acceptance` sets the criteria up front, so a
//     policy's quality gate can be satisfied without a second update call.
//   - a malformed work item id is a usage error that names the id, instead of
//     the storage layer's "unsafe managed path" leak.
//   - a leased work item names the `workitem release` command that unblocks it.
//   - `approval list` reports the effective state, so a consumed or
//     invalidated approval is never shown as usable.

import (
	"context"
	"strings"
	"testing"

	"github.com/JAYY513/Workloom/internal/domain"
)

func TestCreateAcceptsAcceptanceCriteria(t *testing.T) {
	_, items := gatedProject(t)
	code, out, errOut := run(t, "workitem", "create",
		"--title", "补齐 README 构建小节",
		"--actor", "dev", "--reason", "review batch",
		"--description", "- 背景：README 缺少构建小节。- 做法：补两条命令。- 验收：config check 通过。",
		"--acceptance", "第一项,第二项")
	if code != CodeOK {
		t.Fatalf("create: code=%d stderr=%q", code, errOut)
	}
	id := strings.Fields(out)[0]
	wi, err := items.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got := wi.AcceptanceCriteria; len(got) != 2 || got[0] != "第一项" || got[1] != "第二项" {
		t.Errorf("acceptance criteria = %v, want [第一项 第二项]", got)
	}
}

func TestMalformedWorkitemIDIsUsageError(t *testing.T) {
	gatedProject(t)
	for _, id := range []string{"WLM-1\nversion:", "../../etc/passwd"} {
		code, _, errOut := run(t, "workitem", "get", id)
		if code != CodeUsage {
			t.Errorf("id %q: code=%d, want %d (stderr=%q)", id, code, CodeUsage, errOut)
		}
		if !strings.Contains(errOut, "invalid workitem id") {
			t.Errorf("id %q: stderr=%q, want it to name the malformed id", id, errOut)
		}
		if strings.Contains(errOut, "unsafe managed path") {
			t.Errorf("id %q: stderr=%q leaks the storage path", id, errOut)
		}
	}
}

func TestLeasedTransitionNamesTheReleaseCommand(t *testing.T) {
	_, items := gatedProject(t)
	code, out, errOut := run(t, "workitem", "create",
		"--title", "租约提示验证任务", "--actor", "dev", "--reason", "fixture")
	if code != CodeOK {
		t.Fatalf("create: code=%d stderr=%q", code, errOut)
	}
	id := strings.Fields(out)[0]
	transitionTo(t, items, id, domain.StatusReady)

	if code, _, errOut := run(t, "workitem", "claim", "--id", id, "--owner", "dev", "--reason", "start"); code != CodeOK {
		t.Fatalf("claim: code=%d stderr=%q", code, errOut)
	}
	code, _, errOut = run(t, "workitem", "block", "--id", id, "--actor", "dev", "--reason", "pause")
	if code != CodeInvalid {
		t.Fatalf("leased transition: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"lease token mismatch", "leased by \"dev\"", "devsys workitem release --id " + id} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr = %q, want %q", errOut, want)
		}
	}
}

func TestApprovalListShowsEffectiveState(t *testing.T) {
	repo, items := gatedProject(t)
	writeWorkflow(t, repo, "approval-flow.md", cliApprovalPolicy)
	id := createWorkitemWithPolicy(t, items, "approval-flow", "state column task", "fixture")
	must := func(args ...string) string {
		t.Helper()
		code, out, errOut := run(t, args...)
		if code != CodeOK {
			t.Fatalf("%v: code=%d stderr=%q", args, code, errOut)
		}
		return out
	}
	listHas := func(state string) {
		t.Helper()
		if out := must("approval", "list"); !strings.Contains(out, "\t"+state+"\t") {
			t.Errorf("approval list = %q, want a %s row", out, state)
		}
	}

	must("approval", "request", "--id", id, "--stage", "in_progress", "--actor", "dev", "--reason", "change")
	must("approval", "approve", "--id", "approval-1", "--by", "boss", "--comment", "ok")
	listHas("approved")

	must("workitem", "transition", "--id", id, "--to", "in_progress",
		"--actor", "dev", "--reason", "go", "--expect", expectOf(t, items, id))
	listHas("consumed")

	// Leaving the requested status invalidates an approved-but-unconsumed
	// approval (方案 §4.9); the list must show that, not "approved".
	must("approval", "request", "--id", id, "--stage", "in_progress", "--actor", "dev", "--reason", "again")
	must("approval", "approve", "--id", "approval-2", "--by", "boss", "--comment", "ok")
	must("workitem", "transition", "--id", id, "--to", "blocked",
		"--actor", "dev", "--reason", "pause", "--expect", expectOf(t, items, id))
	listHas("invalidated")
}
