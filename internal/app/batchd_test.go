package app

// #342（安装报告批次 D）服务层回归：run complete 成功即释租约且不改写
// run 终态（ForCompleted）；拒绝路径的 ForRefused 不能杀活动 claim。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workloom/internal/domain"
	"workloom/internal/run"
	"workloom/internal/workitem"
)

// TestRunFinishReleasesClaimOnSuccess: run complete 成功后，该 run 绑定的
// 租约被释放（报告 #4），run 记录保持 succeeded（不被 release 的
// run-closing 覆盖），scheduling 文件被删除。
func TestRunFinishReleasesClaimOnSuccess(t *testing.T) {
	root, svc := dispatchFixture(t, "", &domain.WorkItem{Title: "one", Priority: 5})
	ctx := context.Background()
	res, err := svc.WorkitemClaim(ctx, "WLM-1", "dispatch", "go", "")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	// Bind the run to a workspace that has advanced: use the project root
	// itself and make a commit so verifyCompletion sees a new head.
	r, _, err := readRun(ctx, svc, res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	head := commitFile(t, root, "a.txt", "one")
	r.Workspace.Path = root
	r.Workspace.Branch = "master"
	r.Claim.HeadSHA = head
	if err := svc.runUpdateRaw(r, versionOf(t, svc, res.RunID)); err != nil {
		t.Fatalf("bind workspace: %v", err)
	}
	commitFile(t, root, "b.txt", "two")
	if _, err := svc.RunFinish(ctx, RunFinishRequest{
		RunID: res.RunID, Outcome: RunSucceeded, Actor: "agent", Reason: "done",
	}); err != nil {
		t.Fatalf("run finish: %v", err)
	}
	// The run keeps its terminal status…
	done, err := svc.RunGet(ctx, res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Run.Status != RunSucceeded {
		t.Fatalf("run status = %q, want succeeded (release must not clobber it)", done.Run.Status)
	}
	// …and the claim is gone.
	if _, err := os.Stat(filepath.Join(root, ".devsys", "scheduling", "WLM-1.yaml")); !os.IsNotExist(err) {
		t.Fatalf("lease file still present (err=%v)", err)
	}
	wi, err := svc.WorkitemGet(ctx, "WLM-1")
	if err != nil {
		t.Fatal(err)
	}
	if wi.Item.LeaseOwner != "" || wi.Item.LeaseUntil != nil {
		t.Fatalf("lease fields still set: %+v", wi.Item)
	}
}

// TestRefusedCompletionDoesNotKillForeignClaim: ForRefused requires the lease
// to belong to the refusing run — another run's active claim is untouched.
func TestRefusedCompletionDoesNotKillForeignClaim(t *testing.T) {
	root, svc := dispatchFixture(t, "", &domain.WorkItem{Title: "one", Priority: 5})
	ctx := context.Background()
	res, err := svc.WorkitemClaim(ctx, "WLM-1", "agent-a", "go", "")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	// A run that never bound this claim tries the refused release.
	foreign := &domain.Run{
		ProjectID: "demo", WorkItemID: "WLM-1", Status: "created", Attempt: 9,
		Phase: "building_prompt", StartedAt: time.Now().UTC(),
	}
	foreignID, err := newRunFor(t, svc, foreign)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.items().Release(ctx, "WLM-1", workitem.ReleaseOptions{
		ForRefused: true, BoundRunID: foreignID,
		Actor: "test", Reason: "abuse attempt", Now: time.Now().UTC(),
	}); err == nil {
		t.Fatal("ForRefused released a claim bound to a different run")
	}
	if _, err := os.Stat(filepath.Join(root, ".devsys", "scheduling", "WLM-1.yaml")); err != nil {
		t.Fatalf("legitimate lease removed: %v", err)
	}
	if wi, _ := svc.WorkitemGet(ctx, "WLM-1"); wi.Item.LeaseOwner != "agent-a" {
		t.Fatalf("lease owner = %q, want agent-a", wi.Item.LeaseOwner)
	}
	_ = res
}

// helpers ----------------------------------------------------------------

func versionOf(t *testing.T, svc *Service, runID string) string {
	t.Helper()
	_, raw, err := readRun(context.Background(), svc, runID)
	if err != nil {
		t.Fatal(err)
	}
	return versionHash(raw)
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func cmdOutput(root, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = root
	out, err := cmd.Output()
	return string(out), err
}

func newRunFor(t *testing.T, svc *Service, r *domain.Run) (string, error) {
	t.Helper()
	return run.New(svc.Root).Create(context.Background(), r)
}

func commitFile(t *testing.T, root, name, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", name)
	runGit(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "test: "+name)
	out, err := cmdOutput(root, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}
