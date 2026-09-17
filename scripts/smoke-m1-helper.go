// The smoke-m1 helper walks the M1 lifecycle against one initialized project
// directory (实施计划 M1.7): 创建项目 → 建任务 → 记录决策 → 完成. It drives
// the real domain layers — workitem, events, run, record, config — so the
// produced files are exactly what a normal session would leave behind. The
// project ID is read from the initialized .devsys/project.yaml (not hard
// coded); every artifact is created via the public APIs; references between
// records use the IDs the storage layer assigned.
//
//	go run scripts/smoke-m1-helper.go <project-root>          # seed the scenario
//	go run scripts/smoke-m1-helper.go --verify <project-root> # read it back
//
// Any error is propagated to stderr with a non-zero exit code. The seed run
// prints the IDs it assigned so the surrounding script can assert on them.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"workloom/internal/config"
	"workloom/internal/domain"
	"workloom/internal/events"
	"workloom/internal/record"
	"workloom/internal/run"
	"workloom/internal/workitem"
)

const workitemPrefix = "WLM"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: smoke-m1-helper [--verify] <project-root>")
		os.Exit(2)
	}
	mode, root := parseArgs(os.Args[1:])
	switch mode {
	case modeSeed:
		seed(root)
	case modeVerify:
		verify(root)
	default:
		fmt.Fprintln(os.Stderr, "smoke-m1-helper: unknown mode")
		os.Exit(2)
	}
}

type runMode int

const (
	modeSeed runMode = iota
	modeVerify
)

// parseArgs splits positional arguments into a mode + project-root pair.
// --verify selects the read-back mode; everything else is a seed run.
func parseArgs(args []string) (runMode, string) {
	for _, a := range args {
		if a == "--verify" {
			return modeVerify, lastPath(args)
		}
	}
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: smoke-m1-helper [--verify] <project-root>")
		os.Exit(2)
	}
	return modeSeed, args[0]
}

func lastPath(args []string) string {
	for i := len(args) - 1; i >= 0; i-- {
		if args[i] == "--verify" {
			continue
		}
		return args[i]
	}
	fmt.Fprintln(os.Stderr, "usage: smoke-m1-helper [--verify] <project-root>")
	os.Exit(2)
	return ""
}

// seed writes a complete scenario and prints the IDs the surrounding script
// asserts on. It reads the project ID from the initialized project.yaml so
// the run works against any clean `devsys init` (not just a fixed "smokem1"
// placeholder).
func seed(root string) {
	ctx := context.Background()
	now := time.Date(2026, 9, 17, 15, 0, 0, 0, time.UTC)
	projectID := loadProjectID(root)

	// 1. 建任务：WorkItem 先以 draft 落盘，循环末尾再通过 ReadSnapshot + Update
	// 推进到 done。M1 只提供原始持久化，没有 M2 状态机，所以这一步不是合法
	// 状态转换的语义校验，只是文件层替换（参考 实施计划 §M2.1）。
	items := workitem.New(root)
	wi := &domain.WorkItem{
		SchemaVersion: domain.SchemaVersion,
		ProjectID:     projectID,
		Type:          "feature",
		Title:         "协议适配器骨架",
		Description:   "打通 M1 里程碑剧本（M1.7 smoke helper）",
		Status:        "draft",
		Priority:      2,
		AcceptanceCriteria: []string{
			"剧本在干净目录一次跑通",
			"git status 状态文件全部是文本",
		},
		Constraints: []string{"不引入数据库"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	wiID, err := items.Create(ctx, wi, workitemPrefix)
	exit(err)
	fmt.Printf("workitem %s\n", wiID)

	// 2. 评论事件：一条根评论 + 一条同主题的回复，回复事件需带 reply_to。
	stream := events.New(root)
	subject := domain.Reference{Type: "workitem", ID: wiID}
	rootComment := &domain.Event{
		SchemaVersion: domain.SchemaVersion,
		ProjectID:     projectID,
		Type:          "comment",
		Subject:       subject,
		Time:          now.Add(1 * time.Second),
		Actor:         "smoke-script",
		Content:       "任务拆解已对齐，准备进入实施。",
	}
	exit(stream.Append(ctx, rootComment))

	replyComment := &domain.Event{
		SchemaVersion: domain.SchemaVersion,
		ProjectID:     projectID,
		Type:          "comment",
		Subject:       subject,
		Time:          now.Add(2 * time.Second),
		Actor:         "smoke-script",
		ReplyTo:       &rootComment.ID,
		Content:       "已读完规格，批准开工。",
	}
	exit(stream.Append(ctx, replyComment))

	// 3. 决策与发现：M1.5 一记录一文件，ID 由存储层分配。
	records := record.New(root)
	decID, err := records.CreateDecision(ctx, &domain.Decision{
		SchemaVersion:    domain.SchemaVersion,
		ProjectID:        projectID,
		Title:            "M1 采用事件溯源 + 文本快照",
		Context:          "单机协调与 Git 接力，无需数据库",
		Options:          []string{"SQLite", "纯文本 + 事务日志"},
		Decision:         "纯文本 + 事务日志",
		Reasoning:        "方案 §14.1：项目内文本 + Git 同步",
		Consequences:     []string{"检索走文本扫描"},
		Status:           "approved",
		CreatedBy:        "smoke-script",
		RelatedWorkItems: []string{wiID},
		CreatedAt:        now,
	})
	exit(err)
	fmt.Printf("decision %s\n", decID)

	finID, err := records.CreateFinding(ctx, &domain.Finding{
		SchemaVersion:    domain.SchemaVersion,
		ProjectID:        projectID,
		Type:             "risk",
		Title:            "示例风险：实施前需验证边界条件",
		Description:      "M1 剧本中的示例风险，不代表已观测到的生产故障",
		Severity:         "medium",
		Status:           "open",
		RelatedWorkItems: []string{wiID},
	})
	exit(err)
	fmt.Printf("finding %s\n", finID)

	// 4. 三个版本的 artifact：每次 NewArtifactVersion 都基于"上一个文件"
	// 拷字段后再追加。本次刻意覆盖 Name，验证 NewArtifactVersion 的拷贝
	// 语义保留 Name 之外的所有字段（Path、Type、Source、RelatedWorkItems）。
	artifact := &domain.Artifact{
		SchemaVersion:    domain.SchemaVersion,
		ProjectID:        projectID,
		Type:             "architecture",
		Name:             "协议适配器蓝图",
		Source:           "agent",
		Status:           "current",
		RelatedWorkItems: []string{wiID},
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	v1, err := records.CreateArtifact(ctx, artifact)
	exit(err)
	v2, err := records.NewArtifactVersion(ctx, v1, &domain.Artifact{
		Name: "协议适配器蓝图 v2（边界用例）",
	})
	exit(err)
	v3, err := records.NewArtifactVersion(ctx, v2, &domain.Artifact{
		Name: "协议适配器蓝图 v3 终稿",
	})
	exit(err)
	fmt.Printf("artifact %s -> %s -> %s\n", v1, v2, v3)

	// 5. Run 记录 + §11.3 上下文快照：快照把 Agent 看到的版本号原样存下，
	// 未接入工作流或知识库；对应引用与 WorkspaceHead 留空，避免编造来源。
	runs := run.New(root)
	r := &domain.Run{
		SchemaVersion:    domain.SchemaVersion,
		ProjectID:        projectID,
		WorkItemID:       wiID,
		Agent:            domain.RunAgent{ID: "smoke-agent", Harness: "shell", Model: "smoke"},
		Status:           "finished",
		Attempt:          1,
		Phase:            "streaming_turns",
		StartedAt:        now.Add(3 * time.Second),
		InputContextRefs: []string{"artifact://" + v3, "decision://" + decID},
		ContextSnapshot: &domain.ContextSnapshot{
			ProjectStateVersion: 1,
			WorkItemVersion:     1,
			ArtifactVersions:    []string{v3 + ":v3"},
			DecisionIDs:         []string{decID},
		},
	}
	runID, err := runs.Create(ctx, r)
	exit(err)
	fmt.Printf("run %s\n", runID)

	// 6. ReadSnapshot → Update 把 WorkItem 推到终态。说明：本步只走存储层
	// 乐观并发（方案 §15.2），不代表 M2 工作项状态机的合法转换。M1 阶段
	// 不做状态机校验，所以这里只是把"done"字样写进文件，不校验转移合法性。
	snap, raw, err := items.ReadSnapshot(ctx, wiID)
	exit(err)
	if snap.Status == "done" {
		fmt.Fprintln(os.Stderr, "smoke-m1-helper: work item already marked done before update")
		os.Exit(1)
	}
	snap.Status = "done"
	snap.UpdatedAt = now.Add(4 * time.Second)
	exit(items.Update(ctx, snap, raw))
}

// loadProjectID reads the project ID from .devsys/project.yaml via the public
// config loader. The smoke helper must work against any initialized project,
// not a hard-coded literal.
func loadProjectID(root string) string {
	md, problems := config.Load(root)
	if len(problems) > 0 {
		fmt.Fprintln(os.Stderr, "smoke-m1-helper: project metadata invalid:", problems)
		os.Exit(1)
	}
	if md.Project == nil || md.Project.ID == "" {
		fmt.Fprintln(os.Stderr, "smoke-m1-helper: project.yaml missing project id")
		os.Exit(1)
	}
	return md.Project.ID
}

// verify reads the seeded scenario back from disk and asserts the lifecycle
// is intact. Discovery walks the records through the public APIs in a
// dependency order: work item + decision + finding first, then run (which
// carries the v3 artifact ID in its context snapshot), then artifact history
// from v3 back to v1, then the comment thread, then git-tracked UTF-8.
func verify(root string) {
	ctx := context.Background()
	projectID := loadProjectID(root)

	items := workitem.New(root)
	stream := events.New(root)
	records := record.New(root)
	runs := run.New(root)

	// Work item：精确一条；status=done 由 ReadSnapshot + Update 推上来。
	allItems, err := items.List(ctx)
	exit(err)
	if len(allItems) != 1 {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: expected 1 work item, found %d\n", len(allItems))
		os.Exit(1)
	}
	wi := allItems[0]
	if wi.ProjectID != projectID {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: work item project id = %q, want %q\n", wi.ProjectID, projectID)
		os.Exit(1)
	}
	if wi.Status != "done" {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: work item status = %q, want \"done\"\n", wi.Status)
		os.Exit(1)
	}
	if wi.ID == "" || !workitem.ValidID(wi.ID) {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: work item id %q is not well-formed\n", wi.ID)
		os.Exit(1)
	}
	fmt.Printf("workitem %s\n", wi.ID)

	// 决策 / 发现：精确数量 1，ID 与 ProjectID 都对得上。
	decisions, err := records.ListDecisions(ctx)
	exit(err)
	if len(decisions) != 1 {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: expected 1 decision, found %d\n", len(decisions))
		os.Exit(1)
	}
	dec := decisions[0]
	if dec.ProjectID != projectID || dec.Status != "approved" {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: decision %s unexpected (project=%q status=%q)\n", dec.ID, dec.ProjectID, dec.Status)
		os.Exit(1)
	}
	fmt.Printf("decision %s\n", dec.ID)

	findings, err := records.ListFindings(ctx)
	exit(err)
	if len(findings) != 1 {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: expected 1 finding, found %d\n", len(findings))
		os.Exit(1)
	}
	fin := findings[0]
	if fin.ProjectID != projectID || fin.Status != "open" {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: finding %s unexpected (project=%q status=%q)\n", fin.ID, fin.ProjectID, fin.Status)
		os.Exit(1)
	}
	fmt.Printf("finding %s\n", fin.ID)

	// Run：精确一条；ContextSnapshot 把 v3 ID 与决策 ID 钉进去。
	runList, err := runs.List(ctx)
	exit(err)
	if len(runList) != 1 {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: expected 1 run, found %d\n", len(runList))
		os.Exit(1)
	}
	got := runList[0]
	if got.Status != "finished" {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: run %s status = %q, want \"finished\"\n", got.ID, got.Status)
		os.Exit(1)
	}
	if got.WorkItemID != wi.ID {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: run work_item = %q, want %s\n", got.WorkItemID, wi.ID)
		os.Exit(1)
	}
	if got.ContextSnapshot == nil {
		fmt.Fprintln(os.Stderr, "smoke-m1-helper: run missing context snapshot")
		os.Exit(1)
	}
	snap := got.ContextSnapshot
	if len(snap.ArtifactVersions) != 1 {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: run snapshot artifact_versions = %v\n", snap.ArtifactVersions)
		os.Exit(1)
	}
	v3Ref := snap.ArtifactVersions[0]
	v3ID, v3Tag, hasTag := strings.Cut(v3Ref, ":")
	if !hasTag || v3Tag != "v3" || !record.KindArtifact.ValidID(v3ID) {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: run snapshot artifact ref = %q\n", v3Ref)
		os.Exit(1)
	}
	if len(snap.DecisionIDs) != 1 || snap.DecisionIDs[0] != dec.ID {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: run snapshot decision_ids = %v\n", snap.DecisionIDs)
		os.Exit(1)
	}
	wantInput := []string{"artifact://" + v3ID, "decision://" + dec.ID}
	if !equalStrings(got.InputContextRefs, wantInput) {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: run input_context_refs = %v, want %v\n", got.InputContextRefs, wantInput)
		os.Exit(1)
	}
	fmt.Printf("run %s\n", got.ID)

	// Artifact 三版本：从 v3 头取链，每一步都把保留字段带回来。
	history, err := records.ArtifactHistory(ctx, v3ID)
	exit(err)
	if len(history) != 3 {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: expected 3 artifact versions, found %d\n", len(history))
		os.Exit(1)
	}
	for i, ver := range history {
		if ver.Version != 3-i {
			fmt.Fprintf(os.Stderr, "smoke-m1-helper: artifact[%d] version = %d, want %d\n", i, ver.Version, 3-i)
			os.Exit(1)
		}
		if ver.ProjectID != projectID {
			fmt.Fprintf(os.Stderr, "smoke-m1-helper: artifact[%d] project id = %q\n", i, ver.ProjectID)
			os.Exit(1)
		}
		if ver.Type != "architecture" {
			fmt.Fprintf(os.Stderr, "smoke-m1-helper: artifact[%d] type = %q, want \"architecture\"\n", i, ver.Type)
			os.Exit(1)
		}
		// 历史按 newest-first 返回：history[0]=v3 链向 history[1]=v2，链尾
		// 的 v1 不带 PreviousID。
		if i == len(history)-1 {
			if ver.PreviousID != nil {
				fmt.Fprintf(os.Stderr, "smoke-m1-helper: artifact v1 has unexpected previous link %v\n", *ver.PreviousID)
				os.Exit(1)
			}
		} else {
			want := history[i+1].ID
			if ver.PreviousID == nil || *ver.PreviousID != want {
				fmt.Fprintf(os.Stderr, "smoke-m1-helper: artifact[%d] previous = %v, want %s\n", i, ver.PreviousID, want)
				os.Exit(1)
			}
		}
		// 链上每一节都要带上原 WorkItem 关联，证明 NewArtifactVersion
		// 的字段拷贝语义（不是只抄 Name）。
		if len(ver.RelatedWorkItems) != 1 || ver.RelatedWorkItems[0] != wi.ID {
			fmt.Fprintf(os.Stderr, "smoke-m1-helper: artifact[%d] related_workitems = %v\n", i, ver.RelatedWorkItems)
			os.Exit(1)
		}
	}
	fmt.Printf("artifact %s -> %s -> %s\n", history[2].ID, history[1].ID, history[0].ID)

	// 评论线程：根评论 + 同主题的回复各一条，reply_to 必须指向根评论。
	subject := domain.Reference{Type: "workitem", ID: wi.ID}
	threads, err := stream.Comments(ctx, subject)
	exit(err)
	if len(threads) != 1 {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: expected 1 comment thread, found %d\n", len(threads))
		os.Exit(1)
	}

	thread := threads[0]
	if thread.Comment == nil || thread.Comment.ReplyTo != nil {
		fmt.Fprintln(os.Stderr, "smoke-m1-helper: comment thread root missing")
		os.Exit(1)
	}
	if len(thread.Replies) != 1 {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: expected 1 reply, found %d\n", len(thread.Replies))
		os.Exit(1)
	}
	reply := thread.Replies[0]
	if reply.ReplyTo == nil || *reply.ReplyTo != thread.Comment.ID {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: reply target = %v, want %s\n", reply.ReplyTo, thread.Comment.ID)
		os.Exit(1)
	}

	// git ls-files -- .devsys：每个受管文件都是严格 UTF-8 且不含 NUL；任何
	// 被错误追踪进 .devsys/local/ 或 .devsys/.cache/ 的文件都拒绝。
	checkManagedFiles(root)
}

// checkManagedFiles 走 git ls-files -z -- .devsys，对每个文件验证 UTF-8 且
// 不含 NUL；路径命中 .devsys/local/ 或 .devsys/.cache/ 直接拒绝。git 不可
// 用或命令出错都按失败处理。
func checkManagedFiles(root string) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z", "--", ".devsys")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "smoke-m1-helper: git ls-files failed: %v\n", err)
		os.Exit(1)
	}
	if len(bytes.TrimRight(out, "\x00")) == 0 {
		fmt.Fprintln(os.Stderr, "smoke-m1-helper: no .devsys files tracked by git")
		os.Exit(1)
	}
	for _, path := range bytes.Split(out, []byte{0}) {
		if len(path) == 0 {
			continue
		}
		rel := string(path)
		// 拒绝被错误追踪的 local/.cache：方案 §14.2 要求这两条不提交。
		if strings.HasPrefix(rel, ".devsys/local/") || strings.HasPrefix(rel, ".devsys/.cache/") {
			fmt.Fprintf(os.Stderr, "smoke-m1-helper: excluded path tracked: %s\n", rel)
			os.Exit(1)
		}
		full := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(full)
		if err != nil {
			fmt.Fprintf(os.Stderr, "smoke-m1-helper: read %s: %v\n", rel, err)
			os.Exit(1)
		}
		if bytes.IndexByte(data, 0) >= 0 {
			fmt.Fprintf(os.Stderr, "smoke-m1-helper: %s contains NUL byte\n", rel)
			os.Exit(1)
		}
		if !utf8.Valid(data) {
			fmt.Fprintf(os.Stderr, "smoke-m1-helper: %s is not valid UTF-8\n", rel)
			os.Exit(1)
		}
	}
}

// equalStrings 是 []string 的按位置逐项比较。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func exit(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "smoke-m1-helper:", err)
		os.Exit(1)
	}
}
