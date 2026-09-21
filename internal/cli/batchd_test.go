package cli

// #342（安装报告批次 D）CLI 回归：token 侧车脱敏 + 围栏仍生效。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLeaseTokenNotCommitted: scheduling/ 与 workitems/ 里的租约不带
// token（§16.3），token 只在本机 local/leases/ 侧车（§14.4 元数据仍随
// git 走，跨设备分叉检测不受影响）。
func TestLeaseTokenNotCommitted(t *testing.T) {
	repo, items := gatedProject(t)
	createReadyWorkitem(t, items, strongItem("gated")) // clears the gated policy's quality gate
	if code, _, errOut := run(t, "workitem", "claim", "--id", "WLM-1", "--owner", "me", "--reason", "go"); code != CodeOK {
		t.Fatalf("claim: code=%d stderr=%q", code, errOut)
	}
	assertNoToken := func(path, key string) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, key+":") {
				val := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, key+":")), `"'`)
				if val != "" {
					t.Fatalf("%s leaks %s: %q", filepath.Base(path), key, line)
				}
			}
		}
	}
	assertNoToken(filepath.Join(repo, ".devsys", "scheduling", "WLM-1.yaml"), "token")
	assertNoToken(filepath.Join(repo, ".devsys", "workitems", "WLM-1.yaml"), "lease_token")

	// The sidecar holds the token locally.
	matches, err := filepath.Glob(filepath.Join(repo, ".devsys", "local", "leases", "WLM-1.token"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("sidecar = %v (%v), want one local/leases/WLM-1.token", matches, err)
	}
	// Fencing still works: a wrong token is refused, the sidecar token
	// releases cleanly.
	code, _, errOut := run(t, "workitem", "release", "--id", "WLM-1", "--owner", "me", "--token", "wrong", "--actor", "me", "--reason", "x")
	if code == CodeOK || !strings.Contains(errOut, "mismatch") {
		t.Fatalf("wrong-token release: code=%d stderr=%q, want a mismatch refusal", code, errOut)
	}
	tok, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if code, _, errOut = run(t, "workitem", "release", "--id", "WLM-1", "--owner", "me", "--token", strings.TrimSpace(string(tok)), "--actor", "me", "--reason", "pause", "--latest"); code != CodeOK {
		t.Fatalf("sidecar-token release: code=%d stderr=%q", code, errOut)
	}
	if rest, _ := os.ReadFile(filepath.Join(repo, ".devsys", "scheduling", "WLM-1.yaml")); len(rest) != 0 {
		t.Fatalf("lease file not removed after release: %q", rest)
	}
}

// TestRunCompleteRefusalNamesGateWhenReviewBlocked (#345 N1): 无证据且
// review 门未满足时，拒绝文案必须说出真实状态（未进 review + 先补
// comment），而不是谎称 "in review"。
func TestRunCompleteRefusalNamesGateWhenReviewBlocked(t *testing.T) {
	repo, items := gatedProject(t)
	writeWorkflow(t, repo, "review-gated.md", `---
id: review-gated
name: review 门策略
version: 1
steps:
  - id: implement
    type: execute
    required: true
gates:
  stages:
    review:
      require_comment: true
quality_gate:
  min_score: 10
limits:
  max_attempts: 3
---
prompt body
`)
	createReadyWorkitem(t, items, readyItem("完成校验回归任务", "描述足够长以满足质量门的最低要求。", "review-gated"))
	code, out, errOut := run(t, "--json", "workitem", "claim", "--id", "WLM-1", "--owner", "agent", "--reason", "attempt")
	if code != CodeOK {
		t.Fatalf("claim: code=%d stderr=%q", code, errOut)
	}
	var claim struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal([]byte(out), &claim); err != nil || claim.RunID == "" {
		t.Fatalf("claim payload = %q (%v)", out, err)
	}
	// No evidence on the branch: the completion must be refused.
	code, _, errOut = run(t, "run", "complete", "--id", claim.RunID, "--actor", "agent", "--reason", "done")
	if code != CodePrecondition {
		t.Fatalf("run complete: code=%d stderr=%q, want %d", code, errOut, CodePrecondition)
	}
	if strings.Contains(errOut, "is in review") {
		t.Fatalf("stderr lies about the state: %q", errOut)
	}
	for _, want := range []string{"did not enter review", "workitem comment"} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("stderr = %q, want %q", errOut, want)
		}
	}
	code, out, _ = run(t, "--json", "workitem", "get", "WLM-1")
	if code != CodeOK {
		t.Fatal("workitem get failed")
	}
	var view struct {
		Item struct {
			Status string `json:"status"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if view.Item.Status != "in_progress" {
		t.Fatalf("status = %q, want in_progress (gate blocked the review route)", view.Item.Status)
	}
}
