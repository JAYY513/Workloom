package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"workloom/internal/dispatch"
	"workloom/internal/domain"
	"workloom/internal/events"
	"workloom/internal/reconcile"
	"workloom/internal/retry"
	"workloom/internal/run"
	"workloom/internal/workflow"
	"workloom/internal/workitem"
)

// DispatchRequest is one scheduling tick (方案 §4.8): recover, reconcile, then
// start the attempts the plan allows. Nothing here is a background service —
// a tick runs, reports, and returns.
type DispatchRequest struct {
	Actor  string
	Reason string
	// Max bounds how many attempts this tick starts (0 = the caps decide).
	Max int
	// DryRun plans and reports without recovering, claiming, preparing a
	// workspace or starting anything.
	DryRun bool
	// Spawn starts one attempt and reports its process id. Nil uses this
	// devsys binary as a child process; tests inject a recorder.
	Spawn AttemptSpawner
}

// AttemptSpawner starts one attempt: argv is the devsys command to run
// (run exec …) and logPath receives the attempt's stdout/stderr.
type AttemptSpawner func(root, runID string, argv []string, logPath string) (int, error)

// DispatchReport is what a tick did — or, under DryRun, what it would do.
type DispatchReport struct {
	DryRun  bool                     `json:"dry_run"`
	Recover *reconcile.RecoverReport `json:"recover,omitempty"`
	Plan    dispatch.Report          `json:"plan"`
	Started []DispatchedAttempt      `json:"started"`
	// Swept lists the failed or stalled attempts the tick ended and requeued.
	Swept   []SweptAttempt `json:"swept,omitempty"`
	Notices []string       `json:"notices,omitempty"`
}

// DispatchedAttempt is one attempt a tick started.
type DispatchedAttempt struct {
	WorkitemID string `json:"workitem_id"`
	RunID      string `json:"run_id"`
	Workspace  string `json:"workspace,omitempty"`
	Command    string `json:"command"`
	PID        int    `json:"pid,omitempty"`
	Log        string `json:"log"`
}

// Dispatch runs one scheduling tick: it recovers interrupted state, releases
// expired and orphaned claims, plans the dispatchable work items (ordering,
// concurrency gates and the blocked_by precondition — §7.4), and starts the
// attempts the plan allows. Starting means: claim, prepare the workspace,
// spawn the attempt, mark the work item running — each attempt gets its own
// run, workspace and prompt (M6.2/M6.3).
//
// A candidate that cannot start for a precondition reason (invalid policy,
// quality gate, workspace failure) is reported in Notices and does not stop
// the tick: one broken item must not hold the queue.
func (s *Service) Dispatch(ctx context.Context, req DispatchRequest) (DispatchReport, error) {
	if req.Actor == "" || req.Reason == "" {
		return DispatchReport{}, Usagef("dispatch requires actor and reason")
	}
	md, err := s.project()
	if err != nil {
		return DispatchReport{}, err
	}
	rep := DispatchReport{DryRun: req.DryRun, Started: []DispatchedAttempt{}}
	if !req.DryRun {
		recovered, err := reconcile.Recover(ctx, s.Root, reconcile.Options{
			Actor: req.Actor, Reason: req.Reason, Now: s.now,
		})
		if err != nil {
			return DispatchReport{}, Internalf("dispatch: recover: %v", err)
		}
		rep.Recover = &recovered
	}

	items, err := s.items().List(ctx)
	if err != nil {
		return DispatchReport{}, s.storeError(err)
	}
	caps, notices := s.dispatchCaps(ctx, items)
	rep.Notices = append(rep.Notices, notices...)
	if !req.DryRun {
		// 到期重试与停滞评估（§4.8 的 tick 顺序）发生在计划之前：计划看到的
		// 是它们产出的状态。
		swept, sweepNotices, err := s.retrySweep(ctx, items, req)
		if err != nil {
			return rep, err
		}
		rep.Swept = swept
		rep.Notices = append(rep.Notices, sweepNotices...)
		items, err = s.items().List(ctx)
		if err != nil {
			return rep, s.storeError(err)
		}
	}
	rep.Plan = dispatch.Plan(dispatch.Input{Now: s.now(), Items: items, Caps: caps, Max: req.Max})
	if req.DryRun || len(rep.Plan.Start) == 0 {
		return rep, nil
	}

	command := ""
	if md.Config != nil {
		command = strings.TrimSpace(md.Config.DispatchCommand)
	}
	if command == "" {
		return rep, Preconditionf(
			"dispatch found %d candidate(s) but .devsys/config.yaml has no dispatch_command; the per-harness adapters of M6.7 replace it",
			len(rep.Plan.Start))
	}
	spawn := req.Spawn
	if spawn == nil {
		spawn = defaultSpawner()
	}

	// Every attempt re-plans against the state it just changed: the caps are
	// then enforced structurally (against what is genuinely in flight) instead
	// of being reasoned about, and a refused candidate frees its slot for the
	// next one — the cap bounds attempts in flight, not refusals.
	refused := map[string]string{}
	for {
		current, err := s.items().List(ctx)
		if err != nil {
			return rep, s.storeError(err)
		}
		remaining := make([]*domain.WorkItem, 0, len(current))
		for _, wi := range current {
			if _, skip := refused[wi.ID]; skip {
				continue
			}
			remaining = append(remaining, wi)
		}
		budget := 0
		if req.Max > 0 {
			budget = req.Max - len(rep.Started)
		}
		next := dispatch.Plan(dispatch.Input{Now: s.now(), Items: remaining, Caps: caps, Max: budget})
		if len(next.Start) == 0 {
			break
		}
		decision := next.Start[0]
		attempt, err := s.dispatchOne(ctx, decision.WorkitemID, command, projectID(md), req, spawn)
		if err != nil {
			// A refused candidate (blocked policy, unmet gate, workspace
			// failure) is reported and the queue proceeds; only a broken
			// environment stops the tick.
			var appErr *Error
			if errors.As(err, &appErr) && (appErr.Class() == KindPrecondition || appErr.Class() == KindInvalid) {
				rep.Notices = append(rep.Notices, fmt.Sprintf("%s did not start: %v", decision.WorkitemID, err))
				refused[decision.WorkitemID] = err.Error()
				continue
			}
			return rep, err
		}
		rep.Started = append(rep.Started, attempt)
	}
	if len(rep.Started) == 0 && len(refused) > 0 {
		// Nothing started and every attempt was refused: the tick did not
		// achieve what it was asked to do, so it must not report success.
		return rep, Preconditionf("dispatch started nothing: %d candidate(s) refused (see the report's notices)", len(refused))
	}
	return rep, nil
}

// dispatchCaps folds the concurrency declarations of the policies the
// dispatchable items declare (§5.3). A policy that cannot be parsed
// contributes no bound and a notice: the claim path refuses that item anyway.
func (s *Service) dispatchCaps(ctx context.Context, items []*domain.WorkItem) (dispatch.Caps, []string) {
	var declared []dispatch.Caps
	var notices []string
	for _, wi := range items {
		if wi.Status != domain.StatusReady || wi.Workflow == nil {
			continue
		}
		res, err := s.policyForWorkItem(ctx, wi)
		if err != nil {
			// One unreadable policy must not stop the queue: the item is
			// reported and the claim path refuses it when the tick gets there.
			notices = append(notices, fmt.Sprintf("%s declares a policy that cannot be read (%v); it contributes no concurrency bound and will be refused when claimed", wi.ID, err))
			continue
		}
		if res.Policy == nil {
			continue
		}
		if res.Issue != nil {
			notices = append(notices, fmt.Sprintf("%s declares an invalid policy (%s); it contributes no concurrency bound and will be refused when claimed", wi.ID, res.Issue.String()))
			continue
		}
		declared = append(declared, dispatch.Caps{
			Global:    res.Policy.Concurrency.Global,
			PerStatus: res.Policy.Concurrency.PerStatus,
		})
	}
	return dispatch.ResolveCaps(declared), notices
}

// dispatchOne starts one attempt: claim, workspace, spawn, running.
func (s *Service) dispatchOne(ctx context.Context, workitemID, command string, projectIDValue string, req DispatchRequest, spawn AttemptSpawner) (DispatchedAttempt, error) {
	claim, err := s.WorkitemClaim(ctx, workitemID, dispatchOwner, req.Reason, "")
	if err != nil {
		return DispatchedAttempt{}, err
	}
	ws, err := s.WorkspacePrepare(ctx, WorkspacePrepareRequest{
		WorkItemID: workitemID, RunID: claim.RunID, Actor: req.Actor, Reason: req.Reason,
	})
	if err != nil {
		// The workspace is part of starting: a claim without one is released
		// so the next tick can try again instead of holding the item.
		s.releaseFailedClaim(ctx, workitemID, claim.Token, req, err)
		return DispatchedAttempt{}, err
	}
	logRel := filepath.ToSlash(filepath.Join(".devsys", "local", "runs", claim.RunID+".exec.log"))
	argv := []string{"run", "exec", "--id", claim.RunID, "--actor", req.Actor, "--reason", req.Reason, "--"}
	argv = append(argv, shellArgv(command)...)
	pid, err := spawn(s.Root, claim.RunID, argv, filepath.Join(s.Root, filepath.FromSlash(logRel)))
	if err != nil {
		s.releaseFailedClaim(ctx, workitemID, claim.Token, req, err)
		return DispatchedAttempt{}, Internalf("dispatch %s: start attempt: %v", workitemID, err)
	}
	if err := events.New(s.Root).Append(ctx, &domain.Event{
		Type:      "run_dispatched",
		Subject:   domain.Reference{Type: "run", ID: claim.RunID},
		ProjectID: projectIDValue,
		Actor:     req.Actor,
		Related:   []domain.Reference{{Type: "workitem", ID: workitemID}},
		Content:   fmt.Sprintf("%s in %s (pid %d): %s", workitemID, ws.Path, pid, command),
		Time:      s.now(),
	}); err != nil {
		return DispatchedAttempt{}, s.storeError(err)
	}
	return DispatchedAttempt{
		WorkitemID: workitemID, RunID: claim.RunID, Workspace: ws.Path,
		Command: command, PID: pid, Log: logRel,
	}, nil
}

// dispatchOwner is the identity a tick claims under: claims are attributed to
// the dispatcher, not to the operator who ran the tick.
const dispatchOwner = "dispatch"

// releaseFailedClaim gives a claim back when the attempt could not start, so
// the item is dispatchable again instead of waiting out its lease.
func (s *Service) releaseFailedClaim(ctx context.Context, workitemID, token string, req DispatchRequest, cause error) {
	reason := "dispatch could not start the attempt: " + cause.Error()
	if _, err := s.WorkitemRelease(ctx, workitemID, dispatchOwner, token, req.Actor, reason, ""); err != nil {
		// Releasing is best effort: the lease expires on its own.
		_ = err
	}
}

// defaultSpawner starts the attempt as a child process of this devsys
// invocation. The tick does not wait for it (§4.8 starts execution and
// returns): the attempt writes its own run stream, and its stdout/stderr land
// in the log file so a crash before the stream opens is still explainable.
func defaultSpawner() AttemptSpawner {
	return func(root, runID string, argv []string, logPath string) (int, error) {
		if testing.Testing() {
			// A test binary is not devsys: spawning it would re-run the test
			// suite as the "attempt", and that suite would spawn again.
			return 0, errors.New("dispatch cannot spawn attempts from a test binary; inject a spawner")
		}
		exe, err := os.Executable()
		if err != nil {
			return 0, fmt.Errorf("resolve devsys executable: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return 0, fmt.Errorf("create attempt log directory: %w", err)
		}
		log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return 0, fmt.Errorf("open attempt log: %w", err)
		}
		defer log.Close()
		cmd := exec.Command(exe, argv...)
		cmd.Dir = root
		cmd.Stdout, cmd.Stderr = log, log
		cmd.Stdin = nil
		if err := cmd.Start(); err != nil {
			return 0, err
		}
		pid := cmd.Process.Pid
		// The tick is not the attempt's owner: releasing the handle lets the
		// process outlive this invocation without becoming a zombie. A release
		// failure is not a start failure — the process is running, and
		// reporting an error here would hand the claim back to a live attempt.
		_ = cmd.Process.Release()
		return pid, nil
	}
}

// shellArgv wraps a configured command in the host shell (the same rule the
// workspace hooks use: policies and config are platform-specific by nature).
func shellArgv(command string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", command}
	}
	return []string{"sh", "-c", command}
}

// claimHeld reports whether a work item is currently held by a live claim: the
// tick recovers expired leases first, so what remains is genuinely in flight.
func claimHeld(state string) bool {
	return state == domain.SchedulingClaimed || state == domain.SchedulingRunning
}

// SweptAttempt is one failed or stalled attempt the tick acted on.
type SweptAttempt struct {
	WorkitemID    string     `json:"workitem_id"`
	RunID         string     `json:"run_id"`
	Action        string     `json:"action"` // retry_queued | released
	Reason        string     `json:"reason"`
	Attempts      int        `json:"attempts"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
}

// retrySweep ends the attempts that failed, timed out or stopped making
// progress and queues their next attempt with a reproducible backoff (§15.4).
// It runs inside the tick before planning, so the plan sees the state the
// sweep produced — and because the backoff is derived from the work item and
// the attempt number, a restarted scheduler reaches the same instant.
func (s *Service) retrySweep(ctx context.Context, items []*domain.WorkItem, req DispatchRequest) ([]SweptAttempt, []string, error) {
	var swept []SweptAttempt
	var notices []string
	runs, err := run.New(s.Root).List(ctx)
	if err != nil {
		return nil, nil, s.storeError(err)
	}
	latest := map[string]*domain.Run{}
	for _, r := range runs {
		if current, ok := latest[r.WorkItemID]; !ok || r.StartedAt.After(current.StartedAt) {
			latest[r.WorkItemID] = r
		}
	}
	for _, wi := range items {
		if !claimHeld(wi.SchedulingState) {
			continue
		}
		r, ok := latest[wi.ID]
		if !ok {
			continue
		}
		res, err := s.policyForWorkItem(ctx, wi)
		if err != nil {
			notices = append(notices, fmt.Sprintf("%s: policy unreadable, retry sweep skipped (%v)", wi.ID, err))
			continue
		}
		threshold := retry.DefaultStallThreshold
		if res.Policy != nil && res.Policy.Limits.StallThresholdSeconds > 0 {
			threshold = time.Duration(res.Policy.Limits.StallThresholdSeconds) * time.Second
		}
		reason, act := s.sweepTrigger(r, threshold, s.now())
		if !act {
			continue
		}
		if reason == "stalled" && r.Status == "running" {
			// A stalled attempt is over: it is recorded as such before the
			// next attempt is queued, so the evidence says what happened.
			if _, err := s.RunFinish(ctx, RunFinishRequest{
				RunID: r.ID, Outcome: RunStalled, Actor: req.Actor,
				Reason: fmt.Sprintf("no progress for %s", threshold),
			}); err != nil {
				return swept, notices, err
			}
		}
		lease, err := s.items().LeaseInspection(ctx, wi.ID)
		if err != nil {
			notices = append(notices, fmt.Sprintf("%s: lease unreadable, retry sweep skipped (%v)", wi.ID, err))
			continue
		}
		attempts := len(runsFor(runs, wi.ID))
		attempt, next, err := s.queueNextAttempt(ctx, wi, lease, attempts, res.Policy, reason, req)
		if err != nil {
			return swept, notices, err
		}
		entry := SweptAttempt{WorkitemID: wi.ID, RunID: r.ID, Attempts: attempts, Reason: reason}
		if next != nil {
			entry.Action, entry.NextAttemptAt = "retry_queued", next
		} else {
			entry.Action = "released"
			_ = attempt
		}
		swept = append(swept, entry)
	}
	return swept, notices, nil
}

// sweepTrigger decides whether an attempt needs the tick's attention: a failed
// or timed-out attempt is retried, a stalled one is ended first, and a
// deliberate cancellation is only released. act is false when the attempt is
// still making progress.
func (s *Service) sweepTrigger(r *domain.Run, threshold time.Duration, now time.Time) (string, bool) {
	switch r.Status {
	case RunFailed:
		return "failed", true
	case RunTimedOut:
		return "timed_out", true
	case RunStalled:
		return "stalled", true
	case RunCanceled:
		return "canceled", true
	case "running":
		if progress, err := s.lastProgress(r); err == nil && now.Sub(progress) > threshold {
			return "stalled", true
		}
	}
	return "", false
}

// lastProgress is the newest evidence an attempt produced: the last record of
// its stream, or its start time when nothing was streamed yet.
func (s *Service) lastProgress(r *domain.Run) (time.Time, error) {
	lines, err := readRunStream(s.Root, r.ID)
	if err != nil {
		return time.Time{}, err
	}
	if len(lines) == 0 {
		return r.StartedAt, nil
	}
	return lines[len(lines)-1].Time, nil
}

// queueNextAttempt gives the claim back and queues the next attempt, unless
// the attempts the policy allows are used up: then the claim is released and
// the exhaustion is recorded.
func (s *Service) queueNextAttempt(ctx context.Context, wi *domain.WorkItem, lease domain.SchedulingLease, attempts int, policy *workflow.Policy, reason string, req DispatchRequest) (int, *time.Time, error) {
	maxAttempts := 0
	base, ceiling := retry.DefaultBase, retry.DefaultMax
	if policy != nil {
		maxAttempts = policy.Limits.MaxAttempts
		if policy.Limits.BackoffMaxSeconds > 0 {
			ceiling = time.Duration(policy.Limits.BackoffMaxSeconds) * time.Second
		}
	}
	if maxAttempts > 0 && attempts >= maxAttempts {
		if _, err := s.WorkitemRelease(ctx, wi.ID, lease.Owner, lease.Token, req.Actor,
			fmt.Sprintf("%s: attempts exhausted (%d of %d)", reason, attempts, maxAttempts), ""); err != nil {
			return 0, nil, err
		}
		if err := events.New(s.Root).Append(ctx, &domain.Event{
			Type:      "retry_exhausted",
			Subject:   domain.Reference{Type: "workitem", ID: wi.ID},
			ProjectID: wi.ProjectID,
			Actor:     req.Actor,
			Content:   fmt.Sprintf("%s after %d attempt(s); the work item needs a human decision", reason, attempts),
			Time:      s.now(),
		}); err != nil {
			return 0, nil, s.storeError(err)
		}
		return attempts, nil, nil
	}
	attempt := attempts + 1
	delay := retry.Delay(base, ceiling, attempts, wi.ID)
	result, err := s.items().QueueRetry(ctx, wi.ID, workitem.RetryOptions{
		Owner: lease.Owner, Token: lease.Token, Attempt: attempt,
		Delay: delay, Actor: req.Actor,
		Reason: fmt.Sprintf("%s; retrying in %s (attempt %d)", reason, delay.Round(time.Second), attempt),
		Now:    s.now(),
	})
	if err != nil {
		return 0, nil, s.storeError(err)
	}
	next := result.NextAttemptAt
	return attempt, &next, nil
}

// runsFor lists the attempts a work item has already had.
func runsFor(runs []*domain.Run, workitemID string) []*domain.Run {
	var out []*domain.Run
	for _, r := range runs {
		if r.WorkItemID == workitemID {
			out = append(out, r)
		}
	}
	return out
}
