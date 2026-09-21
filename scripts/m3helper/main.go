// Command m3helper prepares and inspects M3.3 smoke fixtures for
// scripts/smoke-m3.*: stage gates, the claim-time quality gate and
// completion propagation. It operates on a project root given as the second
// argument and prints deterministic KEY=VALUE lines the shell script evals.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/events"
	"github.com/JAYY513/Workloom/internal/record"
	"github.com/JAYY513/Workloom/internal/workitem"
)

const workflowID = "quick-fix"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "m3helper: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: m3helper <setup|lowq|evidence|improve|status|propagated> <root> [id]")
	}
	ctx := context.Background()
	switch args[0] {
	case "setup":
		return setup(ctx, args[1])
	case "lowq":
		return lowq(ctx, args[1])
	case "evidence":
		return evidence(ctx, args[1:])
	case "improve":
		return improve(ctx, args[1:])
	case "status":
		return status(ctx, args[1:])
	case "propagated":
		return propagated(ctx, args[1:])
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

// setup creates a parent, a direct sibling and a completing child, walks the
// child to verification, declares the quick-fix workflow on it and links one
// decision and one finding to it.
func setup(ctx context.Context, root string) error {
	items := workitem.New(root)
	now := time.Now().UTC()
	parent, err := create(ctx, items, nil, "M3 smoke parent", "parent task for propagation")
	if err != nil {
		return err
	}
	sibling, err := create(ctx, items, &parent, "M3 smoke sibling", "direct sibling task")
	if err != nil {
		return err
	}
	child, err := create(ctx, items, &parent, "M3 smoke completing task", "the child that completes")
	if err != nil {
		return err
	}
	for _, target := range []string{domain.StatusBacklog, domain.StatusReady, domain.StatusVerification} {
		if err := step(ctx, items, child, target); err != nil {
			return err
		}
	}
	cur, raw, err := items.ReadSnapshot(ctx, child)
	if err != nil {
		return err
	}
	cur.Workflow = &domain.WorkflowInstance{ID: workflowID, Step: "verify", StepEnteredAt: now}
	if err := items.Update(ctx, cur, raw); err != nil {
		return err
	}
	recs := record.New(root)
	if _, err := recs.CreateDecision(ctx, &domain.Decision{
		ProjectID: "demo", Title: "M3 smoke decision", Status: "accepted",
		RelatedWorkItems: []string{child},
	}); err != nil {
		return err
	}
	if _, err := recs.CreateFinding(ctx, &domain.Finding{
		ProjectID: "demo", Title: "M3 smoke finding", Type: "risk", Severity: "low",
		RelatedWorkItems: []string{child},
	}); err != nil {
		return err
	}
	fmt.Printf("PARENT=%s\nSIBLING=%s\nCHILD=%s\n", parent, sibling, child)
	return nil
}

// lowq creates a deliberately thin, ready work item on the quick-fix policy
// so the claim quality gate rejects it.
func lowq(ctx context.Context, root string) error {
	items := workitem.New(root)
	now := time.Now().UTC()
	id, err := create(ctx, items, nil, "fix", "x")
	if err != nil {
		return err
	}
	for _, target := range []string{domain.StatusBacklog, domain.StatusReady} {
		if err := step(ctx, items, id, target); err != nil {
			return err
		}
	}
	cur, raw, err := items.ReadSnapshot(ctx, id)
	if err != nil {
		return err
	}
	cur.Workflow = &domain.WorkflowInstance{ID: workflowID, Step: "implement", StepEnteredAt: now}
	if err := items.Update(ctx, cur, raw); err != nil {
		return err
	}
	fmt.Printf("LOWQ=%s\n", id)
	return nil
}

// evidence adds the test-results artifact and one comment the done gate wants.
func evidence(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: m3helper evidence <root> <workitem-id>")
	}
	root, child := args[0], args[1]
	artifact, err := record.New(root).CreateArtifact(ctx, &domain.Artifact{
		ProjectID: "demo", Type: "test", Name: "test-results", Status: "ready",
		RelatedWorkItems: []string{child},
	})
	if err != nil {
		return err
	}
	if err := events.New(root).Append(ctx, &domain.Event{
		ProjectID: "demo", Type: "comment",
		Subject: domain.Reference{Type: "workitem", ID: child},
		Actor:   "smoke", Content: "verification evidence ready",
	}); err != nil {
		return err
	}
	fmt.Printf("ARTIFACT=%s\n", artifact)
	return nil
}

// improve rewrites a thin work item into one that passes the quality gate.
func improve(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: m3helper improve <root> <workitem-id>")
	}
	root, id := args[0], args[1]
	items := workitem.New(root)
	cur, raw, err := items.ReadSnapshot(ctx, id)
	if err != nil {
		return err
	}
	cur.Title = "实现质量门与门禁的 CLI 接线（M3.3）"
	cur.Description = "背景：领取前需要确定性质量门。\n- 见 internal/cli/cli.go 的接线\n- 验收：go test ./... 通过"
	cur.AcceptanceCriteria = []string{"低质量任务被拒且返回改进项"}
	if err := items.Update(ctx, cur, raw); err != nil {
		return err
	}
	fmt.Printf("IMPROVED=%s\n", id)
	return nil
}

// status prints one work item's status and context refs.
func status(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: m3helper status <root> <workitem-id>")
	}
	root, id := args[0], args[1]
	wi, err := workitem.New(root).Get(ctx, id)
	if err != nil {
		return err
	}
	fmt.Printf("ID=%s STATUS=%s REFS=%s\n", wi.ID, wi.Status, strings.Join(wi.ContextRefs, ","))
	return nil
}

// propagated prints the number of record_propagated events on one subject.
func propagated(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: m3helper propagated <root> <workitem-id>")
	}
	root, id := args[0], args[1]
	evs, err := events.New(root).Read(ctx, events.Filter{
		Subject: &domain.Reference{Type: "workitem", ID: id},
		Type:    "record_propagated",
	})
	if err != nil {
		return err
	}
	fmt.Printf("PROPAGATED=%d\n", len(evs))
	return nil
}

func create(ctx context.Context, items *workitem.Store, parent *string, title, description string) (string, error) {
	now := time.Now().UTC()
	wi := &domain.WorkItem{
		ProjectID: "demo", Type: "task", Title: title, Description: description,
		Status: domain.StatusDraft, ParentID: parent, CreatedAt: now, UpdatedAt: now,
	}
	return items.Create(ctx, wi, "WLM")
}

func step(ctx context.Context, items *workitem.Store, id, target string) error {
	_, raw, err := items.ReadSnapshot(ctx, id)
	if err != nil {
		return err
	}
	_, err = items.Transition(ctx, id, workitem.TransitionRequest{
		TargetStatus: target, Actor: "smoke", Reason: "fixture",
	}, raw)
	return err
}
