// Package workitem — M3.6 workflow instance tests: step advancement by
// declaration-ordered conditions, refusal of jumps and unsatisfied
// conditions with the allowed candidates, status requirements, pause /
// resume / cancel, and the guarantee that instance operations touch neither
// the work item status nor any scheduling material.
package workitem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workloom/internal/domain"
	"workloom/internal/events"
	"workloom/internal/workflow"
)

func flowPolicy(t *testing.T, source string) *workflow.Policy {
	t.Helper()
	policy, issues := workflow.Parse("workflows/flow.md", []byte(source))
	if policy == nil {
		t.Fatalf("policy: %v", issues)
	}
	return policy
}

const branchFlow = `---
id: flow
name: 分支流程
version: 1
steps:
  - id: inspect
    type: inspect
  - id: implement
    type: execute
  - id: clarify
    type: clarify
transitions:
  - from: inspect
    to: implement
    when: workitem.clarification_needed == false
  - from: inspect
    to: clarify
    when: workitem.clarification_needed == true
---
`

func mustStart(t *testing.T, s *Store, id string, policy *workflow.Policy) *domain.WorkItem {
	t.Helper()
	ctx := context.Background()
	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	wi, err := s.WorkflowStart(ctx, id, WorkflowStartOptions{
		Policy: policy, Actor: "test", Reason: "start", Expected: raw,
	})
	if err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	return wi
}

func TestWorkflowBranchesOnCondition(t *testing.T) {
	ctx := context.Background()
	_, s := m3Root(t)
	policy := flowPolicy(t, branchFlow)

	clear := createWorkitem(t, s, nil, "clear task")
	unclear := createWorkitem(t, s, nil, "unclear task")
	mustStart(t, s, clear, policy)
	mustStart(t, s, unclear, policy)

	cur, raw, err := s.ReadSnapshot(ctx, unclear)
	if err != nil {
		t.Fatal(err)
	}
	cur.ClarificationNeeded = true
	if err := s.Update(ctx, cur, raw); err != nil {
		t.Fatal(err)
	}

	for id, want := range map[string]string{clear: "implement", unclear: "clarify"} {
		_, raw, err := s.ReadSnapshot(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := s.WorkflowStepComplete(ctx, id, WorkflowStepOptions{
			Policy: policy, Actor: "test", Reason: "advance", Expected: raw,
		})
		if err != nil {
			t.Fatalf("complete %s: %v", id, err)
		}
		if updated.Workflow == nil || updated.Workflow.Step != want {
			t.Errorf("%s step = %+v, want %s", id, updated.Workflow, want)
		}
	}
}

func TestWorkflowRejectsJumpAndUnsatisfiedCondition(t *testing.T) {
	ctx := context.Background()
	_, s := m3Root(t)
	policy := flowPolicy(t, branchFlow)
	id := createWorkitem(t, s, nil, "jump task")
	mustStart(t, s, id, policy)

	// clarification_needed is false: implement is satisfied, clarify is not.
	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.WorkflowStepComplete(ctx, id, WorkflowStepOptions{
		Policy: policy, To: "clarify", Actor: "test", Reason: "try", Expected: raw,
	})
	var se *WorkflowStepError
	if !errors.As(err, &se) || !strings.Contains(se.Reason, "not satisfied") {
		t.Fatalf("err = %v", err)
	}
	if len(se.Allowed) != 2 {
		t.Errorf("allowed = %+v", se.Allowed)
	}

	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.WorkflowStepComplete(ctx, id, WorkflowStepOptions{
		Policy: policy, To: "verify", Actor: "test", Reason: "jump", Expected: raw,
	})
	if !errors.As(err, &se) || !strings.Contains(se.Reason, "not a declared transition") {
		t.Fatalf("jump err = %v", err)
	}

	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.WorkflowStepComplete(ctx, id, WorkflowStepOptions{
		Policy: policy, Actor: "test", Reason: "auto", Expected: raw,
	})
	if err != nil || updated.Workflow.Step != "implement" {
		t.Fatalf("auto step = %+v, err = %v", updated.Workflow, err)
	}
}

const statusFlow = `---
id: flow
name: 状态要求
version: 1
steps:
  - id: start
    type: inspect
  - id: gate
    type: verify
    status: review
transitions:
  - from: start
    to: gate
---
`

func TestWorkflowEnforcesStepStatusRequirement(t *testing.T) {
	ctx := context.Background()
	_, s := m3Root(t)
	policy := flowPolicy(t, statusFlow)
	id := createWorkitem(t, s, nil, "status task")
	mustStart(t, s, id, policy)

	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.WorkflowStepComplete(ctx, id, WorkflowStepOptions{
		Policy: policy, Actor: "test", Reason: "try", Expected: raw,
	})
	var se *WorkflowStepError
	if !errors.As(err, &se) || !strings.Contains(se.Reason, `requires status "review"`) {
		t.Fatalf("err = %v", err)
	}

	walkTo(t, s, id, domain.StatusBacklog, domain.StatusReady, domain.StatusReview)
	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.WorkflowStepComplete(ctx, id, WorkflowStepOptions{
		Policy: policy, Actor: "test", Reason: "go", Expected: raw,
	})
	if err != nil || updated.Workflow.Step != "gate" {
		t.Fatalf("step = %+v, err = %v", updated.Workflow, err)
	}
}

func TestWorkflowPauseResumeCancel(t *testing.T) {
	ctx := context.Background()
	_, s := m3Root(t)
	policy := flowPolicy(t, branchFlow)
	id := createWorkitem(t, s, nil, "pause task")
	mustStart(t, s, id, policy)

	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	paused, err := s.WorkflowPause(ctx, id, WorkflowSignalOptions{Actor: "test", Reason: "hold", Expected: raw})
	if err != nil || !paused.Workflow.Paused {
		t.Fatalf("pause = %+v, err = %v", paused.Workflow, err)
	}
	var se *WorkflowStepError
	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.WorkflowPause(ctx, id, WorkflowSignalOptions{Actor: "test", Reason: "hold again", Expected: raw})
	if !errors.As(err, &se) || !strings.Contains(se.Reason, "already paused") {
		t.Fatalf("repeat pause err = %v", err)
	}

	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.WorkflowStepComplete(ctx, id, WorkflowStepOptions{
		Policy: policy, Actor: "test", Reason: "try", Expected: raw,
	})
	if !errors.As(err, &se) || !strings.Contains(se.Reason, "paused") {
		t.Fatalf("paused complete err = %v", err)
	}

	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := s.WorkflowResume(ctx, id, WorkflowSignalOptions{Actor: "test", Reason: "go", Expected: raw})
	if err != nil || resumed.Workflow.Paused {
		t.Fatalf("resume = %+v, err = %v", resumed.Workflow, err)
	}
	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.WorkflowResume(ctx, id, WorkflowSignalOptions{Actor: "test", Reason: "go again", Expected: raw})
	if !errors.As(err, &se) || !strings.Contains(se.Reason, "not paused") {
		t.Fatalf("repeat resume err = %v", err)
	}

	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := s.WorkflowCancel(ctx, id, WorkflowSignalOptions{Actor: "test", Reason: "stop", Expected: raw})
	if err != nil || cancelled.Workflow.Step != "cancelled" {
		t.Fatalf("cancel = %+v, err = %v", cancelled.Workflow, err)
	}

	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.WorkflowStepComplete(ctx, id, WorkflowStepOptions{
		Policy: policy, Actor: "test", Reason: "try", Expected: raw,
	})
	if !errors.As(err, &se) || !strings.Contains(se.Reason, "already cancelled") {
		t.Fatalf("terminal complete err = %v", err)
	}
}

const selfLoopFlow = `---
id: flow
name: 自环
version: 1
steps:
  - id: a
    type: inspect
  - id: b
    type: execute
transitions:
  - from: a
    to: a
  - from: a
    to: b
---
`

const linearFlow = `---
id: flow
name: 线性
version: 1
steps:
  - id: a
    type: inspect
  - id: b
    type: execute
transitions:
  - from: a
    to: b
  - from: b
    to: done
---
`

func TestWorkflowSelfLoopTargetRefusedAndSkipped(t *testing.T) {
	ctx := context.Background()
	_, s := m3Root(t)
	policy := flowPolicy(t, selfLoopFlow)
	id := createWorkitem(t, s, nil, "self loop task")
	mustStart(t, s, id, policy)

	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.WorkflowStepComplete(ctx, id, WorkflowStepOptions{
		Policy: policy, To: "a", Actor: "test", Reason: "try", Expected: raw,
	})
	var se *WorkflowStepError
	if !errors.As(err, &se) || !strings.Contains(se.Reason, "equals the current step") {
		t.Fatalf("self-loop err = %v", err)
	}

	// Auto mode skips the self-loop and takes the advancing edge.
	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.WorkflowStepComplete(ctx, id, WorkflowStepOptions{
		Policy: policy, Actor: "test", Reason: "auto", Expected: raw,
	})
	if err != nil || updated.Workflow.Step != "b" {
		t.Fatalf("auto = %+v, err = %v", updated.Workflow, err)
	}
}

// Instance writes are fenced by the active lease: the holder must present the
// current owner and token, everyone else is refused.
func TestWorkflowInstanceWritesRequireLeaseHolder(t *testing.T) {
	ctx := context.Background()
	root, s := m3Root(t)
	policy := flowPolicy(t, branchFlow)
	if err := os.MkdirAll(filepath.Join(root, ".devsys", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".devsys", "workflows", "flow.md"), []byte(branchFlow), 0o644); err != nil {
		t.Fatal(err)
	}

	id := createWorkitem(t, s, nil, "leased task")
	mustStart(t, s, id, policy)
	walkTo(t, s, id, domain.StatusBacklog, domain.StatusReady)
	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := s.Claim(ctx, id, ClaimOptions{Owner: "holder", Actor: "holder", Reason: "work", Expected: raw})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	// The claim rewrote the file; take a fresh guard before probing fencing.
	_, raw, err = s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.WorkflowStepComplete(ctx, id, WorkflowStepOptions{
		Policy: policy, Actor: "intruder", Reason: "try", Expected: raw,
	})
	if err == nil || !strings.Contains(err.Error(), "lease active") {
		t.Fatalf("intruder err = %v", err)
	}
	_, err = s.WorkflowStepComplete(ctx, id, WorkflowStepOptions{
		Policy: policy, Actor: "holder", Reason: "try", Owner: "holder", Token: "wrong", Expected: raw,
	})
	if err == nil || !strings.Contains(err.Error(), "lease active") {
		t.Fatalf("wrong token err = %v", err)
	}

	updated, err := s.WorkflowStepComplete(ctx, id, WorkflowStepOptions{
		Policy: policy, Actor: "holder", Reason: "go", Owner: "holder", Token: claim.Token, Expected: raw,
	})
	if err != nil {
		t.Fatalf("holder complete: %v", err)
	}
	if updated.Workflow.Step != "implement" {
		t.Errorf("step = %s", updated.Workflow.Step)
	}
	if updated.LeaseOwner != "holder" || updated.LeaseToken != claim.Token || updated.SchedulingState != domain.SchedulingClaimed {
		t.Errorf("lease fields changed: owner=%q token=%q scheduling=%q", updated.LeaseOwner, updated.LeaseToken, updated.SchedulingState)
	}
}

// A finished instance refuses pause and cancel with the terminal reason.
func TestWorkflowTerminalStatesRefuseSignals(t *testing.T) {
	ctx := context.Background()
	_, s := m3Root(t)
	policy := flowPolicy(t, linearFlow)
	id := createWorkitem(t, s, nil, "linear task")
	mustStart(t, s, id, policy)
	for i := range 2 {
		_, raw, err := s.ReadSnapshot(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.WorkflowStepComplete(ctx, id, WorkflowStepOptions{
			Policy: policy, Actor: "test", Reason: "go", Expected: raw,
		}); err != nil {
			t.Fatalf("complete %d: %v", i, err)
		}
	}
	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var se *WorkflowStepError
	_, err = s.WorkflowPause(ctx, id, WorkflowSignalOptions{Actor: "test", Reason: "hold", Expected: raw})
	if !errors.As(err, &se) || !strings.Contains(se.Reason, "already done") {
		t.Fatalf("pause after done = %v", err)
	}
	_, err = s.WorkflowCancel(ctx, id, WorkflowSignalOptions{Actor: "test", Reason: "stop", Expected: raw})
	if !errors.As(err, &se) || !strings.Contains(se.Reason, "already done") {
		t.Fatalf("cancel after done = %v", err)
	}
}

func TestWorkflowAdvanceTouchesNeitherStatusNorScheduling(t *testing.T) {
	ctx := context.Background()
	root, s := m3Root(t)
	policy := flowPolicy(t, branchFlow)
	id := createWorkitem(t, s, nil, "scheduling task")
	before, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	mustStart(t, s, id, policy)

	_, raw, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkflowStepComplete(ctx, id, WorkflowStepOptions{
		Policy: policy, Actor: "test", Reason: "advance", Expected: raw,
	}); err != nil {
		t.Fatal(err)
	}

	after, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Workflow == nil || after.Workflow.Step != "implement" {
		t.Fatalf("workflow = %+v", after.Workflow)
	}
	if after.Status != before.Status {
		t.Errorf("status changed: %s -> %s", before.Status, after.Status)
	}
	if after.SchedulingState != before.SchedulingState {
		t.Errorf("scheduling_state changed: %s -> %s", before.SchedulingState, after.SchedulingState)
	}
	if after.LeaseOwner != "" || after.LeaseToken != "" || after.ActiveRunID != "" {
		t.Errorf("lease fields touched: owner=%q token=%q run=%q", after.LeaseOwner, after.LeaseToken, after.ActiveRunID)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".devsys", "scheduling"))
	if err == nil && len(entries) > 0 {
		t.Errorf("scheduling files appeared: %v", entries)
	}

	evs, err := events.New(root).Read(ctx, events.Filter{
		Subject: &domain.Reference{Type: "workitem", ID: id},
	})
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, ev := range evs {
		types = append(types, ev.Type)
	}
	if got := strings.Join(types, ","); got != "workflow_started,workflow_step_completed" {
		t.Errorf("events = %v", types)
	}
}
