package cli

// Tests for the second external-review fix batch (usage review, 待决策 items):
//   - `next` judges a ready work item with the same quality gate `workitem
//     claim` applies, instead of recommending a claim that must fail.
//   - `next` reports queued retries instead of reporting the project idle.
//   - a stage gate names the approval 方案 §4.9 invalidated, so "I approved
//     this" has an answer.
//   - `config.yaml default_policy` gates work items that declare no instance,
//     and a claim under no policy at all says the gates did not run.
//   - `project blueprint` reports "no blueprint declared" as a result (exit 0)
//     like the other read-only queries.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/config"
	"github.com/JAYY513/Workloom/internal/domain"
)

// setDefaultPolicy rewrites .devsys/config.yaml with a project-level policy.
func setDefaultPolicy(t *testing.T, repo, id string) {
	t.Helper()
	body := fmt.Sprintf("schema_version: %d\ndefault_policy: %s\n", domain.SchemaVersion, id)
	if err := os.WriteFile(filepath.Join(repo, ".devsys", "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeWorkitemState writes a raw work item document. A retry_queued item is
// only reachable through a failed attempt plus a dispatch tick, which is far
// more fixture than a read-only query test needs.
func writeWorkitemState(t *testing.T, repo string, wi *domain.WorkItem) {
	t.Helper()
	md, problems := config.Load(repo)
	if md == nil || md.Project == nil || problems != nil {
		t.Fatalf("fixture project: %v", problems)
	}
	wi.SchemaVersion = domain.SchemaVersion
	wi.ProjectID = md.Project.ID
	if wi.CreatedAt.IsZero() {
		wi.CreatedAt, wi.UpdatedAt = time.Now().UTC(), time.Now().UTC()
	}
	data, err := domain.EncodeYAML(wi)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo, ".devsys", "workitems", wi.ID+".yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProjectBlueprintWithoutDeclarationExitsZero(t *testing.T) {
	gatedProject(t)
	code, out, errOut := run(t, "project", "blueprint")
	if code != CodeOK {
		t.Fatalf("blueprint: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "no blueprint declared") {
		t.Errorf("stdout = %q, want a no-blueprint result", out)
	}
	code, out, errOut = run(t, "--json", "project", "blueprint")
	if code != CodeOK {
		t.Fatalf("blueprint --json: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, `"artifact":null`) {
		t.Errorf("stdout = %q, want a null artifact in the ok envelope", out)
	}
}

func TestNextReportsQualityGateBlock(t *testing.T) {
	_, items := gatedProject(t)
	id := createReadyWorkitem(t, items, readyItem("弱标题", "短", "gated"))

	code, out, errOut := run(t, "next")
	if code != CodeOK {
		t.Fatalf("next: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{
		"readiness: CONCERNS",
		"risk: quality_blocked " + id,
		"quality gate",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, want %q", out, want)
		}
	}
	// The ladder still names the task to work on, together with the command
	// that makes its claim possible.
	if !strings.Contains(out, "next: start "+id) {
		t.Errorf("stdout = %q, want the ladder to point at %s", out, id)
	}
	if !strings.Contains(out, "workloom workitem update --id "+id) {
		t.Errorf("stdout = %q, want the remediation command", out)
	}
}

func TestNextReportsQueuedRetry(t *testing.T) {
	repo, _ := gatedProject(t)
	due := time.Now().UTC().Add(30 * time.Second).Truncate(time.Second)
	writeWorkitemState(t, repo, &domain.WorkItem{
		ID: "WLM-9", Type: "task", Title: "重试中的任务", Status: domain.StatusRetryQueued,
		NextAttemptAt: &due, SchedulingState: domain.SchedulingRetryQueued,
	})

	code, out, errOut := run(t, "next")
	if code != CodeOK {
		t.Fatalf("next: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "risk: retry_pending WLM-9: retry queued; next attempt at "+due.Format(time.RFC3339)) {
		t.Errorf("stdout = %q, want the queued retry with its recorded due time", out)
	}
	for _, want := range []string{"next: report_done", "wait for a dispatch retry", "workloom dispatch --once"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, want %q (the project is not idle)", out, want)
		}
	}
}

func TestClaimWarnsWhenNoPolicyGoverns(t *testing.T) {
	repo, items := gatedProject(t)
	id := createReadyWorkitem(t, items, readyItem("无策略工作项", "fixture description", ""))

	code, out, errOut := run(t, "workitem", "claim", "--id", id, "--owner", "dev", "--reason", "start")
	if code != CodeOK {
		t.Fatalf("claim: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "warning: ") || !strings.Contains(out, "gates are not enforced") {
		t.Errorf("stdout = %q, want the gates-not-enforced warning", out)
	}
	if !strings.Contains(out, "workloom workflow start --id "+id) {
		t.Errorf("stdout = %q, want the binding command", out)
	}

	// With a project default in place the same shape of item is gated, so the
	// claim runs under a policy and there is nothing to warn about.
	setDefaultPolicy(t, repo, "gated")
	second := createReadyWorkitem(t, items, strongItem(""))
	code, out, errOut = run(t, "workitem", "claim", "--id", second, "--owner", "dev", "--reason", "start")
	if code != CodeOK {
		t.Fatalf("claim under the default policy: code=%d stderr=%q", code, errOut)
	}
	if strings.Contains(out, "warning:") {
		t.Errorf("stdout = %q, want no warning once the default policy gates the item", out)
	}
}

// strongItem is an unbound ready work item that also clears the gated policy's
// quality gate: every scored component is present.
func strongItem(policyID string) *domain.WorkItem {
	wi := readyItem(
		"补齐 README 构建小节并复核验收路径",
		"- 背景：README 缺少构建小节，安装者找不到入口。\n- 做法：在 docs/使用手册.md 补两条命令与一段验收说明，并复核 config check 输出。\n- 验收：按文档执行 go build ./cmd/devsys 成功，config check 通过。",
		policyID)
	wi.AcceptanceCriteria = []string{"按文档构建成功", "config check 通过"}
	return wi
}

func TestDefaultPolicyGatesUnboundWorkitem(t *testing.T) {
	repo, items := gatedProject(t)
	setDefaultPolicy(t, repo, "gated")
	id := createReadyWorkitem(t, items, readyItem("弱标题", "短", ""))

	code, _, errOut := run(t, "workitem", "claim", "--id", id, "--owner", "dev", "--reason", "start")
	if code != CodeInvalid {
		t.Fatalf("claim: code=%d stderr=%q, want the quality gate to refuse it", code, errOut)
	}
	if !strings.Contains(errOut, "quality gate not satisfied") {
		t.Errorf("stderr = %q, want the quality gate rejection", errOut)
	}

	// An item that declares its own instance keeps using it, whatever the
	// project default says.
	code, _, errOut = run(t, "next")
	if code != CodeOK {
		t.Fatalf("next: code=%d stderr=%q", code, errOut)
	}
	if strings.Contains(errOut, "declares no default_policy") {
		t.Errorf("stderr = %q, want no unbound-policy notice", errOut)
	}
}

func TestUnusableConfigCannotSilentlyDropGates(t *testing.T) {
	repo, items := gatedProject(t)
	// An unknown key makes config.yaml invalid; without the fail-closed branch
	// the default policy would vanish and the claim would run ungated.
	if err := os.WriteFile(filepath.Join(repo, ".devsys", "config.yaml"),
		[]byte(fmt.Sprintf("schema_version: %d\ndefault_policy: gated\nno_such_key: 1\n", domain.SchemaVersion)), 0o644); err != nil {
		t.Fatal(err)
	}
	id := createReadyWorkitem(t, items, readyItem("弱标题", "短", ""))
	code, _, errOut := run(t, "workitem", "claim", "--id", id, "--owner", "dev", "--reason", "start")
	if code != CodeInvalid {
		t.Fatalf("claim under a broken config.yaml: code=%d stderr=%q, want a refusal", code, errOut)
	}
	if !strings.Contains(errOut, "config.yaml is invalid") || !strings.Contains(errOut, "config check") {
		t.Errorf("stderr = %q, want the config failure and its check command", errOut)
	}

	// A default_policy that names no policy file is refused the same way.
	setDefaultPolicy(t, repo, "missing-policy")
	second := createReadyWorkitem(t, items, readyItem("弱标题", "短", ""))
	code, _, errOut = run(t, "workitem", "claim", "--id", second, "--owner", "dev", "--reason", "start")
	if code != CodeInvalid || !strings.Contains(errOut, "missing-policy") {
		t.Fatalf("claim under a missing default policy: code=%d stderr=%q", code, errOut)
	}
}

func TestTransitionGateNamesInvalidatedApproval(t *testing.T) {
	repo, items := gatedProject(t)
	writeWorkflow(t, repo, "approval-flow.md", cliApprovalPolicy)
	id := createReadyWorkitem(t, items, readyItem("审批门禁任务", "fixture description", "approval-flow"))

	code, out, errOut := run(t, "approval", "request", "--id", id, "--stage", "in_progress",
		"--actor", "dev", "--reason", "gate")
	if code != CodeOK {
		t.Fatalf("approval request: code=%d stderr=%q", code, errOut)
	}
	approvalID := strings.Fields(out)[0]
	if code, _, errOut = run(t, "approval", "approve", "--id", approvalID, "--by", "lead"); code != CodeOK {
		t.Fatalf("approval approve: code=%d stderr=%q", code, errOut)
	}

	// Leaving the status the approval was requested from voids it (方案 §4.9).
	if code, _, errOut = run(t, "workitem", "block", "--id", id, "--actor", "dev", "--reason", "pause"); code != CodeOK {
		t.Fatalf("block: code=%d stderr=%q", code, errOut)
	}
	if code, _, errOut = run(t, "workitem", "transition", "--id", id, "--to", "ready",
		"--actor", "dev", "--reason", "resume"); code != CodeOK {
		t.Fatalf("resume: code=%d stderr=%q", code, errOut)
	}

	code, _, errOut = run(t, "workitem", "transition", "--id", id, "--to", "in_progress",
		"--actor", "dev", "--reason", "go")
	if code != CodeInvalid {
		t.Fatalf("gated transition: code=%d stderr=%q, want the approval gate to refuse", code, errOut)
	}
	for _, want := range []string{approvalID, "invalidated", "§4.9", "request a new approval"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr = %q, want %q", errOut, want)
		}
	}
}
