// Workflow instance operations (实施计划 M3.6, 方案 §5.3). The instance state
// lives in the work item's `workflow` field and the step history is the event
// stream; every operation rewrites the field and appends its event in one
// storage transaction.
//
// These operations never touch the work item status, the scheduling state,
// lease files or runs: advancing a step is bookkeeping of the workflow
// instance, not of execution. Step declarations that require a work item
// status are enforced as requirements, never as implicit transitions.
package workitem

import (
	"context"
	"fmt"
	"strings"
	"time"

	"workloom/internal/domain"
	"workloom/internal/events"
	"workloom/internal/storage"
	"workloom/internal/workflow"
)

// Workflow terminal step sentinels.
const (
	workflowStepDone      = "done"
	workflowStepCancelled = "cancelled"
)

// WorkflowStepError reports a refused instance operation together with the
// candidates that were available from the current step.
type WorkflowStepError struct {
	Step    string
	Reason  string
	Allowed []workflow.StepCandidate
}

func (e *WorkflowStepError) Error() string {
	var b strings.Builder
	b.WriteString("workflow step ")
	if e.Step != "" {
		fmt.Fprintf(&b, "%q ", e.Step)
	}
	b.WriteString("refused: ")
	b.WriteString(e.Reason)
	if len(e.Allowed) > 0 {
		parts := make([]string, 0, len(e.Allowed))
		for _, c := range e.Allowed {
			entry := "to=" + c.To
			if c.When != "" {
				entry += fmt.Sprintf(" when=%q", c.When)
			}
			entry += fmt.Sprintf(" satisfied=%t", c.Satisfied)
			parts = append(parts, entry)
		}
		b.WriteString("; allowed next steps: " + strings.Join(parts, ", "))
	}
	return b.String()
}

// WorkflowStartOptions is the input to WorkflowStart.
type WorkflowStartOptions struct {
	Policy *workflow.Policy
	Actor  string
	Reason string
	// Owner and Token must match the active lease when the work item is
	// claimed; instance writes are otherwise refused (same fencing contract
	// as UpdateClaimed).
	Owner    string
	Token    string
	Expected []byte
	Now      time.Time
}

// WorkflowStepOptions is the input to WorkflowStepComplete.
type WorkflowStepOptions struct {
	Policy   *workflow.Policy
	To       string
	Actor    string
	Reason   string
	Owner    string
	Token    string
	Expected []byte
	Now      time.Time
}

// WorkflowSignalOptions is the input to pause / resume / cancel.
type WorkflowSignalOptions struct {
	Actor    string
	Reason   string
	Owner    string
	Token    string
	Expected []byte
	Now      time.Time
}

func validWorkflowAction(actor, reason string, expected []byte) error {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" || len(expected) == 0 {
		return fmt.Errorf("%w: actor, reason and expected snapshot required", ErrInvalidInput)
	}
	return nil
}

func terminalWorkflowStep(step string) bool {
	return step == workflowStepDone || step == workflowStepCancelled
}

// WorkflowStart creates the instance at the policy's first step. The first
// step's declared status (if any) must already match the work item status:
// instance operations never move the work item status themselves.
func (s *Store) WorkflowStart(ctx context.Context, id string, opts WorkflowStartOptions) (*domain.WorkItem, error) {
	if opts.Policy == nil {
		return nil, fmt.Errorf("%w: policy required", ErrInvalidInput)
	}
	if err := validWorkflowAction(opts.Actor, opts.Reason, opts.Expected); err != nil {
		return nil, err
	}
	if len(opts.Policy.Steps) == 0 {
		return nil, fmt.Errorf("%w: policy has no steps", ErrInvalidInput)
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.workflowWrite(ctx, id, opts.Expected, now, opts.Owner, opts.Token, func(wi *domain.WorkItem) (*domain.Event, error) {
		if wi.Workflow != nil {
			return nil, &WorkflowStepError{Step: wi.Workflow.Step, Reason: "work item already has a workflow instance"}
		}
		if terminalWorkflowStep(wi.Status) {
			return nil, &WorkflowStepError{Reason: fmt.Sprintf("work item is %s; a new instance cannot start", wi.Status)}
		}
		first := opts.Policy.Steps[0]
		if first.Status != "" && first.Status != wi.Status {
			return nil, &WorkflowStepError{Step: first.ID,
				Reason: fmt.Sprintf("step %q requires status %q, work item is %q", first.ID, first.Status, wi.Status)}
		}
		wi.Workflow = &domain.WorkflowInstance{ID: opts.Policy.ID, Step: first.ID, StepEnteredAt: now}
		return &domain.Event{
			SchemaVersion: domain.SchemaVersion,
			ProjectID:     wi.ProjectID,
			Type:          "workflow_started",
			Subject:       domain.Reference{Type: "workitem", ID: id},
			Time:          now.UTC(),
			Actor:         opts.Actor,
			Content:       fmt.Sprintf("started %s at step %q: %s", opts.Policy.ID, first.ID, opts.Reason),
			Related:       []domain.Reference{{Type: "workflow", ID: opts.Policy.ID}},
		}, nil
	})
}

// WorkflowStepComplete advances the instance one step: the first satisfied
// candidate in declaration order, or the explicitly requested target when it
// is a declared transition whose condition currently holds.
func (s *Store) WorkflowStepComplete(ctx context.Context, id string, opts WorkflowStepOptions) (*domain.WorkItem, error) {
	if opts.Policy == nil {
		return nil, fmt.Errorf("%w: policy required", ErrInvalidInput)
	}
	if err := validWorkflowAction(opts.Actor, opts.Reason, opts.Expected); err != nil {
		return nil, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.workflowWrite(ctx, id, opts.Expected, now, opts.Owner, opts.Token, func(wi *domain.WorkItem) (*domain.Event, error) {
		inst := wi.Workflow
		if inst == nil {
			return nil, &WorkflowStepError{Reason: "work item has no workflow instance; run workflow start first"}
		}
		if terminalWorkflowStep(inst.Step) {
			return nil, &WorkflowStepError{Step: inst.Step, Reason: fmt.Sprintf("instance is already %s", inst.Step)}
		}
		if inst.Paused {
			return nil, &WorkflowStepError{Step: inst.Step, Reason: "instance is paused; resume before advancing"}
		}
		facts := workflow.FactsOf(wi)
		candidates := opts.Policy.StepCandidates(inst.Step, facts)
		if len(candidates) == 0 {
			return nil, &WorkflowStepError{Step: inst.Step,
				Reason: fmt.Sprintf("no transitions are declared from step %q", inst.Step)}
		}

		chosen := workflow.StepCandidate{}
		found := false
		if opts.To != "" {
			if opts.To == inst.Step {
				return nil, &WorkflowStepError{Step: inst.Step, Allowed: candidates,
					Reason: "target equals the current step"}
			}
			for _, cand := range candidates {
				if cand.To != opts.To {
					continue
				}
				if !cand.Satisfied {
					return nil, &WorkflowStepError{Step: inst.Step, Allowed: candidates,
						Reason: fmt.Sprintf("condition for target %q is not satisfied", opts.To)}
				}
				chosen, found = cand, true
				break
			}
			if !found {
				return nil, &WorkflowStepError{Step: inst.Step, Allowed: candidates,
					Reason: fmt.Sprintf("target %q is not a declared transition from step %q", opts.To, inst.Step)}
			}
		} else {
			for _, cand := range candidates {
				if cand.To == inst.Step {
					continue // self-loops advance nothing
				}
				if cand.Satisfied {
					chosen, found = cand, true
					break
				}
			}
			if !found {
				return nil, &WorkflowStepError{Step: inst.Step, Allowed: candidates,
					Reason: "no transition condition is satisfied"}
			}
		}

		if target, ok := opts.Policy.StepByID(chosen.To); ok && target.Status != "" && target.Status != wi.Status {
			return nil, &WorkflowStepError{Step: inst.Step, Allowed: candidates,
				Reason: fmt.Sprintf("step %q requires status %q, work item is %q", chosen.To, target.Status, wi.Status)}
		}

		from := inst.Step
		inst.Step = chosen.To
		inst.StepEnteredAt = now
		return &domain.Event{
			SchemaVersion: domain.SchemaVersion,
			ProjectID:     wi.ProjectID,
			Type:          "workflow_step_completed",
			Subject:       domain.Reference{Type: "workitem", ID: id},
			Time:          now.UTC(),
			Actor:         opts.Actor,
			Content:       fmt.Sprintf("%s -> %s: %s", from, chosen.To, opts.Reason),
			Related:       []domain.Reference{{Type: "workflow", ID: opts.Policy.ID}},
		}, nil
	})
}

// WorkflowPause holds the instance without touching the work item status.
func (s *Store) WorkflowPause(ctx context.Context, id string, opts WorkflowSignalOptions) (*domain.WorkItem, error) {
	return s.workflowSignal(ctx, id, opts, "workflow_paused", func(wi *domain.WorkItem) error {
		if terminalWorkflowStep(wi.Workflow.Step) {
			return &WorkflowStepError{Step: wi.Workflow.Step, Reason: fmt.Sprintf("instance is already %s", wi.Workflow.Step)}
		}
		if wi.Workflow.Paused {
			return &WorkflowStepError{Step: wi.Workflow.Step, Reason: "instance is already paused"}
		}
		wi.Workflow.Paused = true
		return nil
	})
}

// WorkflowResume releases a paused instance.
func (s *Store) WorkflowResume(ctx context.Context, id string, opts WorkflowSignalOptions) (*domain.WorkItem, error) {
	return s.workflowSignal(ctx, id, opts, "workflow_resumed", func(wi *domain.WorkItem) error {
		if !wi.Workflow.Paused {
			return &WorkflowStepError{Step: wi.Workflow.Step, Reason: "instance is not paused"}
		}
		wi.Workflow.Paused = false
		return nil
	})
}

// WorkflowCancel ends the instance with the cancelled sentinel.
func (s *Store) WorkflowCancel(ctx context.Context, id string, opts WorkflowSignalOptions) (*domain.WorkItem, error) {
	return s.workflowSignal(ctx, id, opts, "workflow_cancelled", func(wi *domain.WorkItem) error {
		if wi.Workflow.Step == workflowStepCancelled {
			return &WorkflowStepError{Step: wi.Workflow.Step, Reason: "instance is already cancelled"}
		}
		if wi.Workflow.Step == workflowStepDone {
			return &WorkflowStepError{Step: wi.Workflow.Step, Reason: "instance is already done"}
		}
		wi.Workflow.Step = workflowStepCancelled
		wi.Workflow.Paused = false
		return nil
	})
}

func (s *Store) workflowSignal(ctx context.Context, id string, opts WorkflowSignalOptions, eventType string, mutate func(*domain.WorkItem) error) (*domain.WorkItem, error) {
	if err := validWorkflowAction(opts.Actor, opts.Reason, opts.Expected); err != nil {
		return nil, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.workflowWrite(ctx, id, opts.Expected, now, opts.Owner, opts.Token, func(wi *domain.WorkItem) (*domain.Event, error) {
		if wi.Workflow == nil {
			return nil, &WorkflowStepError{Reason: "work item has no workflow instance; run workflow start first"}
		}
		if err := mutate(wi); err != nil {
			return nil, err
		}
		return &domain.Event{
			SchemaVersion: domain.SchemaVersion,
			ProjectID:     wi.ProjectID,
			Type:          eventType,
			Subject:       domain.Reference{Type: "workitem", ID: id},
			Time:          now.UTC(),
			Actor:         opts.Actor,
			Content:       fmt.Sprintf("%s at step %q: %s", eventType, wi.Workflow.Step, opts.Reason),
			Related:       []domain.Reference{{Type: "workflow", ID: wi.Workflow.ID}},
		}, nil
	})
}

// WorkflowNext reports the candidates out of the current step. It only reads.
func (s *Store) WorkflowNext(ctx context.Context, id string, policy *workflow.Policy) (*domain.WorkItem, []workflow.StepCandidate, error) {
	if policy == nil {
		return nil, nil, fmt.Errorf("%w: policy required", ErrInvalidInput)
	}
	wi, _, err := s.ReadSnapshot(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if wi.Workflow == nil {
		return nil, nil, &WorkflowStepError{Reason: "work item has no workflow instance; run workflow start first"}
	}
	if terminalWorkflowStep(wi.Workflow.Step) {
		return wi, nil, nil
	}
	return wi, policy.StepCandidates(wi.Workflow.Step, workflow.FactsOf(wi)), nil
}

// workflowWrite runs one instance mutation: it decodes the work item inside
// the transaction, lets mutate rewrite the workflow field, writes it under
// the optimistic guard and appends the returned event in the same
// transaction. Status and scheduling fields are never touched here.
//
// While a lease is active the caller must present the current lease owner and
// token (the same fencing contract UpdateClaimed enforces); anyone else is
// refused, so instance state of a running work item stays with its holder.
func (s *Store) workflowWrite(ctx context.Context, id string, expected []byte, now time.Time, owner, token string, mutate func(wi *domain.WorkItem) (*domain.Event, error)) (*domain.WorkItem, error) {
	if !ValidID(id) || len(expected) == 0 {
		return nil, fmt.Errorf("%w: valid id and expected snapshot required", ErrInvalidInput)
	}
	st, err := s.store()
	if err != nil {
		return nil, err
	}
	var wi domain.WorkItem
	err = st.Write(ctx, func(tx *storage.Tx) error {
		raw, exists, err := tx.ReadForExpect(workitemRel(id))
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		if !bytesEqual(raw, expected) {
			return fmt.Errorf("%w: %s", storage.ErrConflict, id)
		}
		if err := domain.DecodeYAML(raw, &wi); err != nil {
			return err
		}
		if wi.LeaseOwner != "" && wi.LeaseUntil != nil {
			// The persisted snapshot carries no token (#342): fence against
			// the local sidecar, falling back to a legacy persisted token.
			tok, terr := tokenFromTx(tx, id, wi.LeaseToken)
			if terr != nil {
				return terr
			}
			if owner != wi.LeaseOwner || token != tok {
				return fmt.Errorf("%w: %s is claimed (lease active); pass the current lease owner and token", ErrInvalidInput, id)
			}
		}
		ev, err := mutate(&wi)
		if err != nil {
			return err
		}
		if ev == nil {
			return fmt.Errorf("%w: workflow mutation did not produce an event", ErrInvalidInput)
		}
		wi.UpdatedAt = now
		if err := tx.PutYAML(workitemRel(id), &wi, storage.ExpectHash(storage.HashBytes(raw))); err != nil {
			return err
		}
		return events.AppendTx(tx, ev)
	})
	if err != nil {
		return nil, err
	}
	return &wi, nil
}
