package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"workloom/internal/domain"
	"workloom/internal/events"
	"workloom/internal/harness"
	"workloom/internal/run"
	"workloom/internal/storage"
	"workloom/internal/workflow"
	"workloom/internal/workspace"
)

// syncEvery bounds how much of the output stream can be lost in a crash: the
// stream is written line by line and flushed to stable storage every this many
// lines, on every control record, and when the attempt ends.
const syncEvery = 128

// stopGrace bounds how long a failing path waits for a stopped attempt to be
// collected, so an abandoned attempt cannot hold a caller open.
const stopGrace = 10 * time.Second

// continuationDelay is how long after a clean exit the dispatch tick should
// check whether the session still needs to make progress (§4.8); M6.4 owns the
// scheduling, this only states the intent.
const continuationDelay = 30 * time.Second

// RunExecRequest is one attempt driven through a harness adapter (实施计划
// M6.1/M6.3). Sink receives every streamed line as it is persisted; nil means
// nobody is watching (dispatch, background execution).
type RunExecRequest struct {
	RunID   string
	Argv    []string
	Timeout time.Duration
	// Round selects the session round; 0 continues the session (round 1 when
	// nothing ran yet). The first round carries the full prompt, later rounds
	// the delta only (方案 §4.8).
	Round int
	// Harness names the harness adapter to drive (方案 §9.3). When set, the
	// adapter builds the command from the assembled prompt and Argv must be
	// empty; when empty, Argv is run as given (the shell path).
	Harness string
	Model   string
	Actor   string
	Reason  string
	Sink    func(harness.Line)
}

// RunExecView is the outcome of one attempt. ExitCode is the command's own
// exit code (-1 when it has none: killed by a signal, or never started) —
// devsys's own exit code stays reserved for devsys operations (0 ran and
// recorded, 2 usage, 3 precondition, 4 untrusted state, 1 internal).
type RunExecView struct {
	RunID      string `json:"run_id"`
	Adapter    string `json:"adapter"`
	Command    string `json:"command"`
	Dir        string `json:"dir"`
	Round      int    `json:"round"`
	Mode       string `json:"prompt_mode"`
	PromptHash string `json:"prompt_hash"`
	PromptFile string `json:"prompt_file,omitempty"`
	Phase      string `json:"phase"`
	// Status is how this round ended; RunStatus is where the run stands after
	// it. A clean round leaves the run running — the session may continue
	// (§4.8 schedules a continuation check), and only completion, failure,
	// timeout, stall or cancellation ends the attempt.
	Status     string   `json:"status"`
	RunStatus  string   `json:"run_status"`
	ExitCode   int      `json:"exit_code"`
	TimedOut   bool     `json:"timed_out"`
	Canceled   bool     `json:"canceled"`
	DurationMS int64    `json:"duration_ms"`
	Lines      int      `json:"lines"`
	Log        string   `json:"log"`
	Warnings   []string `json:"warnings,omitempty"`
}

// RunExec runs one attempt round for a run: it enforces the workspace
// invariants and the before_run hook (§4.8), assembles the round's prompt,
// streams both output streams into the run's event stream
// (.devsys/runs/<id>.jsonl, §14.2) while forwarding them to the sink, then
// records the evidence on the run and in the event log — including the §4.8
// phase progression and the terminal status.
func (s *Service) RunExec(ctx context.Context, req RunExecRequest) (RunExecView, error) {
	if req.RunID == "" {
		return RunExecView{}, Usagef("run exec requires a run id")
	}
	if req.Harness == "" && len(req.Argv) == 0 {
		return RunExecView{}, Usagef("run exec requires a command (devsys run exec --id <run-id> -- <command...>) or --harness <name>")
	}
	if req.Harness != "" && len(req.Argv) > 0 {
		return RunExecView{}, Usagef("run exec takes either --harness or a command, not both")
	}
	if req.Actor == "" || req.Reason == "" {
		return RunExecView{}, Usagef("run exec requires actor and reason")
	}
	md, err := s.project()
	if err != nil {
		return RunExecView{}, err
	}
	r, err := run.New(s.Root).Get(ctx, req.RunID)
	if err != nil {
		return RunExecView{}, s.storeError(err)
	}
	if isTerminalRun(r.Status) {
		return RunExecView{}, Preconditionf("run %s already ended as %s", r.ID, r.Status)
	}
	wi, err := s.WorkitemGet(ctx, r.WorkItemID)
	if err != nil {
		return RunExecView{}, err
	}

	// Workspace invariants (§4.8): an attempt runs in the run's workspace and
	// nowhere else. A run without one falls back to the project root — that is
	// an operator's ad-hoc execution, not a dispatched attempt.
	dir := r.Workspace.Path
	if dir == "" {
		dir = s.Root
	} else {
		configured := ""
		if md.Config != nil {
			configured = md.Config.WorkspaceRoot
		}
		root, err := workspace.Root(s.Root, configured)
		if err != nil {
			return RunExecView{}, Preconditionf("%v", err)
		}
		if err := workspace.Validate(root, dir); err != nil {
			return RunExecView{}, Preconditionf("run workspace: %v", err)
		}
	}

	res, err := s.policyForWorkItem(ctx, wi.Item)
	if err != nil {
		return RunExecView{}, s.errWorkflowPolicy(err)
	}
	if res.Policy != nil && res.Issue != nil {
		return RunExecView{}, s.errWorkflowPolicy(fmt.Errorf(
			"workflow policy %q is invalid: %s; attempts stay blocked until the file is fixed",
			res.ID, res.Issue.String()))
	}
	hooks := workspaceHooks(res.Policy)

	// Round bookkeeping and the round cap (§4.8): the session's rounds are
	// counted from its own stream, so a fresh process continues where the last
	// one stopped.
	lines, err := readRunStream(s.Root, r.ID)
	if err != nil {
		return RunExecView{}, Internalf("read run stream: %v", err)
	}
	// The next round is derived from the stream, never invented by a caller:
	// an explicit round is only accepted when it is that next one, so a
	// session cannot replay a round or slip past the bound.
	round := nextRound(lines)
	if req.Round > 0 && req.Round != round {
		return RunExecView{}, Preconditionf(
			"run %s is at round %d; --round %d would not continue the session", r.ID, round, req.Round)
	}
	if cap := roundCap(res.Policy); cap > 0 && round > cap {
		return RunExecView{}, Preconditionf(
			"run %s has used %d of %d rounds (limits.max_attempts); start a new attempt instead",
			r.ID, round-1, cap)
	}

	// The harness is chosen before anything runs: an unavailable one is a
	// refusal, never a silent fallback to another harness (方案 §9.3).
	adapter := harness.Adapter(harness.NewShell())
	if req.Harness != "" {
		if strings.EqualFold(strings.TrimSpace(req.Harness), "shell") {
			return RunExecView{}, Usagef("the shell adapter runs an explicit command: pass one after --, or set dispatch_command for dispatched attempts")
		}
		chosen, ok := harness.ByName(req.Harness)
		if !ok {
			return RunExecView{}, Usagef("unknown harness %q (known: %s)", req.Harness, strings.Join(harness.Names(), ", "))
		}
		avail, err := chosen.Probe(ctx)
		if err != nil {
			return RunExecView{}, Internalf("probe harness %q: %v", req.Harness, err)
		}
		if !avail.Installed {
			return RunExecView{}, Preconditionf("harness %q is not available: %s", req.Harness, avail.Detail)
		}
		adapter = chosen
	}
	if err := adapter.Prepare(ctx, harness.Workspace{Path: dir, Branch: r.Workspace.Branch, Worktree: r.Workspace.Worktree}); err != nil {
		return RunExecView{}, Preconditionf("%v", err)
	}
	promptView, err := s.RunPrompt(ctx, RunPromptRequest{RunID: r.ID, Round: round, Write: true})
	if err != nil {
		return RunExecView{}, err
	}
	env := append(workspace.HookEnv(s.Root, dir, r.Workspace.Branch, wi.Item.ID, ""),
		"DEVSYS_RUN_ID="+r.ID,
		"DEVSYS_ROUND="+fmt.Sprint(round),
		"DEVSYS_PROMPT_FILE="+filepath.Join(s.Root, filepath.FromSlash(promptView.Path)),
	)
	var cmd harness.Command
	if req.Harness != "" {
		// The adapter builds the command from the round's prompt (方案 §9.2
		// build_command): the prompt is the attempt's whole input.
		cmd, err = adapter.Command(harness.Request{
			Prompt: promptView.Text, Dir: dir, Env: env, Timeout: req.Timeout, Model: req.Model,
		})
	} else {
		cmd, err = adapter.Command(harness.Request{Command: req.Argv, Dir: dir, Env: env, Timeout: req.Timeout})
	}
	if err != nil {
		return RunExecView{}, Preconditionf("%v", err)
	}
	commandLine := quoteArgv(cmd.Argv)

	stream, err := openRunStream(s.Root, r.ID)
	if err != nil {
		return RunExecView{}, Internalf("open run stream: %v", err)
	}
	defer stream.Close()

	if err := s.advancePhase(ctx, stream, r.ID, "building_prompt"); err != nil {
		return RunExecView{}, err
	}
	if err := stream.write(streamRecord{
		Type: "round", Round: round, Mode: promptView.Mode,
		PromptHash: promptView.Hash, PromptFile: promptView.Path, Refs: promptView.Refs,
		// The context snapshot travels with the round record: what the agent
		// saw is part of the attempt's evidence (方案 §11.3).
		Context: snapshotRecord(promptView.Snapshot),
	}, true); err != nil {
		return RunExecView{}, Internalf("write run stream: %v", err)
	}
	if err := s.appendInputRefs(ctx, r.ID, promptView.Refs); err != nil {
		return RunExecView{}, err
	}

	// before_run is fatal: a hook that fails aborts the attempt before any
	// agent process starts (§4.8).
	if hook, ok := hooks[workspace.HookBeforeRun]; ok && strings.TrimSpace(hook.Command) != "" {
		_, herr := workspace.RunHook(ctx, workspace.RunHookOptions{
			Name: workspace.HookBeforeRun, Hook: hook, Dir: dir,
			Env: append(env, "DEVSYS_HOOK="+workspace.HookBeforeRun),
		})
		if herr != nil {
			return s.abortAttempt(ctx, stream, r, req, dir, round, commandLine, promptView, herr)
		}
	}
	// The attempt's own record says which harness drove it (方案 §5.4): the
	// acceptance compares runs across harnesses by exactly this field.
	if req.Harness != "" {
		if err := s.recordAgent(ctx, r.ID, req.Harness, req.Model); err != nil {
			return RunExecView{}, err
		}
	}
	if err := s.advancePhase(ctx, stream, r.ID, "launching_agent"); err != nil {
		return RunExecView{}, err
	}
	if err := s.markRunning(ctx, r.ID); err != nil {
		return RunExecView{}, err
	}
	if err := stream.write(streamRecord{
		Type: "start", Adapter: adapter.Name(), Command: commandLine, Argv: cmd.Argv,
		Cwd: cmd.Dir, TimeoutMS: cmd.Timeout.Milliseconds(), Round: round,
	}, true); err != nil {
		return RunExecView{}, Internalf("write run stream: %v", err)
	}
	if err := events.New(s.Root).Append(ctx, &domain.Event{
		Type:      "run_exec_started",
		Subject:   domain.Reference{Type: "run", ID: r.ID},
		ProjectID: r.ProjectID,
		Actor:     req.Actor,
		Related:   []domain.Reference{{Type: "workitem", ID: r.WorkItemID}},
		Content:   fmt.Sprintf("round %d (%s): %s", round, promptView.Mode, commandLine),
		Time:      s.now(),
	}); err != nil {
		return RunExecView{}, s.storeError(err)
	}

	sess, err := adapter.Start(ctx, cmd)
	if err != nil {
		note := fmt.Sprintf("command failed to start: %s (%s)", err, commandLine)
		code := -1
		if werr := stream.write(streamRecord{Type: "exit", Code: &code, Text: note}, true); werr != nil {
			return RunExecView{}, Internalf("write run stream: %v", werr)
		}
		if werr := s.recordExecOutcome(ctx, r, req.Actor, commandLine, note, note); werr != nil {
			return RunExecView{}, werr
		}
		if werr := s.advancePhase(ctx, stream, r.ID, "finishing"); werr != nil {
			return RunExecView{}, werr
		}
		if werr := s.finishAttempt(ctx, r.ID, RunFailed, req.Actor, note, note, stream); werr != nil {
			return RunExecView{}, werr
		}
		return RunExecView{}, Preconditionf("%v", err)
	}
	// However this function leaves — a failed stream write, a caller that
	// stopped reading — the attempt must not keep running. Stop is a no-op
	// once the process has been collected.
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), stopGrace)
		defer cancel()
		_ = sess.Stop(stopCtx)
	}()
	if err := s.advancePhase(ctx, stream, r.ID, "streaming_turns"); err != nil {
		return RunExecView{}, err
	}

	lineCount := 0
	for line := range sess.Lines() {
		lineCount++
		if err := stream.write(streamRecord{Type: "output", Stream: line.Stream, Line: line.Text, Time: line.Time}, false); err != nil {
			return RunExecView{}, Internalf("write run stream: %v", err)
		}
		if req.Sink != nil {
			req.Sink(line)
		}
	}
	// Collection is not abortable: the stream has closed, so the attempt is
	// over, and its outcome is the evidence this path exists to record.
	result, err := sess.Wait(context.Background())
	if err != nil {
		return RunExecView{}, Internalf("collect result: %v", err)
	}
	if err := s.advancePhase(ctx, stream, r.ID, "finishing"); err != nil {
		return RunExecView{}, err
	}

	// after_run never changes the outcome: it runs after the attempt and its
	// failure is recorded (§4.8).
	var warnings []string
	if hook, ok := hooks[workspace.HookAfterRun]; ok && strings.TrimSpace(hook.Command) != "" {
		rep, herr := workspace.RunHook(ctx, workspace.RunHookOptions{
			Name: workspace.HookAfterRun, Hook: hook, Dir: dir,
			Env: append(env, "DEVSYS_HOOK="+workspace.HookAfterRun),
		})
		if herr != nil {
			// after_run never changes the outcome: even failing to record
			// it must not stop the attempt from being closed out.
			warnings = append(warnings, herr.Error())
			if err := events.New(s.Root).Append(ctx, &domain.Event{
				Type:      "workspace_hook_failed",
				Subject:   domain.Reference{Type: "run", ID: r.ID},
				ProjectID: r.ProjectID,
				Actor:     req.Actor,
				Content:   herr.Error(),
				Time:      s.now(),
			}); err != nil {
				warnings = append(warnings, "recording the hook failure failed: "+err.Error())
			}
		} else if rep.Ran {
			warnings = append(warnings, fmt.Sprintf("%s hook ran in %dms", workspace.HookAfterRun, rep.DurationMS))
		}
	}

	roundStatus := terminalStatus(result)
	code := result.ExitCode
	exit := streamRecord{
		Type: "exit", Code: &code, TimedOut: result.TimedOut, Canceled: result.Canceled,
		DurationMS: result.Duration().Milliseconds(), Text: result.Err, Round: round, Status: roundStatus,
	}
	if roundStatus == RunSucceeded {
		// A clean exit is not the end of the attempt: the session may still
		// have work to do, so the tick gets a continuation check (§4.8).
		due := s.now().Add(continuationDelay)
		exit.DueAt = &due
	}
	if err := stream.write(exit, true); err != nil {
		return RunExecView{}, Internalf("write run stream: %v", err)
	}
	note := execNote(result, commandLine)
	if err := s.recordExecOutcome(ctx, r, req.Actor, commandLine, note, execSummary(result, commandLine)); err != nil {
		return RunExecView{}, err
	}
	if roundStatus != RunSucceeded {
		if err := s.finishAttempt(ctx, r.ID, roundStatus, req.Actor, execSummary(result, commandLine), note, stream); err != nil {
			return RunExecView{}, err
		}
	}

	view := RunExecView{
		RunID: r.ID, Adapter: adapter.Name(), Command: commandLine, Dir: cmd.Dir,
		Round: round, Mode: promptView.Mode, PromptHash: promptView.Hash, PromptFile: promptView.Path,
		Phase: "finishing", Status: roundStatus,
		ExitCode: result.ExitCode, TimedOut: result.TimedOut, Canceled: result.Canceled,
		DurationMS: result.Duration().Milliseconds(), Lines: lineCount, Log: logRelFor(r.ID),
		Warnings: warnings,
	}
	if fresh, err := s.RunGet(ctx, r.ID); err == nil {
		view.RunStatus = fresh.Run.Status
	}
	return view, nil
}

// abortAttempt records an attempt that a fatal before_run hook stopped before
// it started, keeping the evidence symmetric with attempts that ran.
func (s *Service) abortAttempt(ctx context.Context, stream *runStream, r *domain.Run, req RunExecRequest, dir string, round int, commandLine string, pv RunPromptView, cause error) (RunExecView, error) {
	code := -1
	note := cause.Error()
	if err := stream.write(streamRecord{Type: "exit", Code: &code, Text: note, Round: round, Status: RunFailed}, true); err != nil {
		return RunExecView{}, Internalf("write run stream: %v", err)
	}
	if err := events.New(s.Root).Append(ctx, &domain.Event{
		Type:      "workspace_hook_failed",
		Subject:   domain.Reference{Type: "run", ID: r.ID},
		ProjectID: r.ProjectID,
		Actor:     req.Actor,
		Related:   []domain.Reference{{Type: "workitem", ID: r.WorkItemID}},
		Content:   note,
		Time:      s.now(),
	}); err != nil {
		return RunExecView{}, s.storeError(err)
	}
	if err := s.recordExecOutcome(ctx, r, req.Actor, commandLine, note, note); err != nil {
		return RunExecView{}, err
	}
	if err := s.finishAttempt(ctx, r.ID, RunFailed, req.Actor, note, note, stream); err != nil {
		return RunExecView{}, err
	}
	return RunExecView{
		RunID: r.ID, Adapter: "shell", Command: commandLine, Dir: dir,
		Round: round, Mode: pv.Mode, PromptHash: pv.Hash, PromptFile: pv.Path,
		Phase: "building_prompt", Status: RunFailed, ExitCode: -1, Log: logRelFor(r.ID),
	}, Preconditionf("%v", cause)
}

// advancePhase records one §4.8 phase on the run and in its stream: the run
// record is what a supervisor polls, the stream is the ordered evidence.
func (s *Service) advancePhase(ctx context.Context, stream *runStream, runID, phase string) error {
	r, raw, err := readRun(ctx, s, runID)
	if err != nil {
		return err
	}
	if isTerminalRun(r.Status) {
		return Preconditionf("run %s already ended as %s", r.ID, r.Status)
	}
	r.Phase = phase
	if err := run.New(s.Root).Update(ctx, r, raw); err != nil {
		return s.storeError(err)
	}
	if err := stream.write(streamRecord{Type: "phase", Phase: phase}, false); err != nil {
		return Internalf("write run stream: %v", err)
	}
	return nil
}

// recordAgent stamps the harness (and model) that drives the attempt.
func (s *Service) recordAgent(ctx context.Context, runID, harnessName, model string) error {
	r, raw, err := readRun(ctx, s, runID)
	if err != nil {
		return err
	}
	if r.Agent.Harness == harnessName && r.Agent.Model == model {
		return nil
	}
	r.Agent.Harness = harnessName
	if model != "" {
		r.Agent.Model = model
	}
	if err := run.New(s.Root).Update(ctx, r, raw); err != nil {
		return s.storeError(err)
	}
	return nil
}

// markRunning records that the attempt is executing. A run somebody else
// already ended stays ended.
func (s *Service) markRunning(ctx context.Context, runID string) error {
	r, raw, err := readRun(ctx, s, runID)
	if err != nil {
		return err
	}
	if isTerminalRun(r.Status) {
		return Preconditionf("run %s ended as %s before the attempt started", r.ID, r.Status)
	}
	if r.Status == "running" {
		return nil
	}
	r.Status = "running"
	if err := run.New(s.Root).Update(ctx, r, raw); err != nil {
		return s.storeError(err)
	}
	return nil
}

// finishAttempt moves the run to its terminal status. A run somebody else
// already ended keeps that decision: the outcome is still recorded in the
// stream, and the caller is told.
func (s *Service) finishAttempt(ctx context.Context, runID, status, actor, summary, note string, stream *runStream) error {
	view, err := s.RunGet(ctx, runID)
	if err != nil {
		return err
	}
	if isTerminalRun(view.Run.Status) {
		if view.Run.Status != status {
			return stream.write(streamRecord{
				Type: "note",
				Text: fmt.Sprintf("run already ended as %s; this attempt's outcome (%s) is recorded in the stream only", view.Run.Status, status),
			}, true)
		}
		return nil
	}
	if _, err := s.RunFinish(ctx, RunFinishRequest{
		RunID: runID, Expect: view.Version, Outcome: status,
		Actor: actor, Reason: summary, Note: note,
	}); err != nil {
		return err
	}
	return nil
}

// appendInputRefs records what the round pointed the agent at (方案 §11.3)
// without duplicating pointers the run already carries.
func (s *Service) appendInputRefs(ctx context.Context, runID string, refs []string) error {
	if len(refs) == 0 {
		return nil
	}
	r, raw, err := readRun(ctx, s, runID)
	if err != nil {
		return err
	}
	changed := false
	for _, ref := range refs {
		if ref != "" && !contains(r.InputContextRefs, ref) {
			r.InputContextRefs = append(r.InputContextRefs, ref)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := run.New(s.Root).Update(ctx, r, raw); err != nil {
		return s.storeError(err)
	}
	return nil
}

// roundCap is the session round bound (方案 §5.3 轮数上限, stored as
// limits.max_attempts by M3.1); 0 means the policy declares none.
func roundCap(policy *workflow.Policy) int {
	if policy == nil {
		return 0
	}
	return policy.Limits.MaxAttempts
}

// terminalStatus maps how an attempt ended onto the §4.8 vocabulary.
func terminalStatus(res harness.Result) string {
	switch {
	case res.TimedOut:
		return RunTimedOut
	case res.Canceled:
		return RunCanceled
	case res.Err != "", res.ExitCode != 0:
		return RunFailed
	default:
		return RunSucceeded
	}
}

// recordExecOutcome writes the attempt's evidence where the rest of the system
// reads it: the run's own record (under the same version guard every other
// write uses, so a concurrent edit is reported rather than overwritten) and
// the event log. note is empty when the attempt has nothing to report.
func (s *Service) recordExecOutcome(ctx context.Context, r *domain.Run, actor, commandLine, note, summary string) error {
	fresh, raw, err := readRun(ctx, s, r.ID)
	if err != nil {
		return err
	}
	fresh.Commands = append(fresh.Commands, commandLine)
	if logRel := logRelFor(r.ID); !contains(fresh.Logs, logRel) {
		fresh.Logs = append(fresh.Logs, logRel)
	}
	if note != "" {
		fresh.Result.Errors = append(fresh.Result.Errors, note)
	}
	if err := run.New(s.Root).Update(ctx, fresh, raw); err != nil {
		return s.storeError(err)
	}
	if err := events.New(s.Root).Append(ctx, &domain.Event{
		Type:      "run_exec_finished",
		Subject:   domain.Reference{Type: "run", ID: r.ID},
		ProjectID: r.ProjectID,
		Actor:     actor,
		Related:   []domain.Reference{{Type: "workitem", ID: r.WorkItemID}},
		Content:   summary,
		Time:      s.now(),
	}); err != nil {
		return s.storeError(err)
	}
	return nil
}

// logRelFor is the stream reference recorded on the run.
func logRelFor(runID string) string {
	return filepath.ToSlash(filepath.Join(".devsys", "runs", runID+".jsonl"))
}

// execNote renders how the attempt ended for the run's error evidence: empty
// when the command exited zero, because that is not a problem to report.
func execNote(res harness.Result, command string) string {
	switch {
	case res.TimedOut:
		return fmt.Sprintf("command timed out: %s", command)
	case res.Canceled:
		return fmt.Sprintf("command canceled: %s", command)
	case res.Err != "":
		return fmt.Sprintf("command failed to run: %s (%s)", res.Err, command)
	case res.ExitCode != 0:
		return fmt.Sprintf("command exited %d: %s", res.ExitCode, command)
	default:
		return ""
	}
}

// execSummary renders the same outcome for the event log, where every attempt
// gets a sentence even when it succeeded.
func execSummary(res harness.Result, command string) string {
	if note := execNote(res, command); note != "" {
		return fmt.Sprintf("%s in %s", note, res.Duration().Round(time.Millisecond))
	}
	return fmt.Sprintf("command exited 0: %s in %s", command, res.Duration().Round(time.Millisecond))
}

// quoteArgv renders a command for evidence: the structured argv is recorded
// alongside it, this is the readable form, and an argument with spaces keeps
// its quotes so the line can be read back.
func quoteArgv(argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		if arg == "" || strings.ContainsAny(arg, " \t\"") {
			quoted = append(quoted, "\""+strings.ReplaceAll(arg, "\"", "\\\"")+"\"")
			continue
		}
		quoted = append(quoted, arg)
	}
	return strings.Join(quoted, " ")
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// runStream appends a run's event stream (.devsys/runs/<run-id>.jsonl).
type runStream struct {
	path    string
	file    *os.File
	pending int
}

// openRunStream opens (or creates) the stream and repairs a torn tail left by
// an interrupted writer, recording the repair in the stream itself.
func openRunStream(root, runID string) (*runStream, error) {
	st, err := storage.Open(root, storage.Options{})
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(st.DevsysDir(), "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create runs directory: %w", err)
	}
	path := filepath.Join(dir, runID+".jsonl")
	removed, err := storage.RepairTornTail(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open run stream: %w", err)
	}
	s := &runStream{path: path, file: f}
	if removed > 0 {
		if err := s.write(streamRecord{Type: "repair", Removed: removed, Text: "truncated a torn tail left by an interrupted writer"}, true); err != nil {
			f.Close()
			return nil, err
		}
	}
	return s, nil
}

// write appends one record. Control records and every syncEvery-th line are
// flushed to stable storage so a crash costs at most the lines in between.
func (s *runStream) write(rec streamRecord, sync bool) error {
	if rec.Time.IsZero() {
		rec.Time = time.Now().UTC()
	}
	payload, err := storage.MarshalJSONL(rec)
	if err != nil {
		return err
	}
	if _, err := s.file.Write(payload); err != nil {
		return fmt.Errorf("append %s: %w", s.path, err)
	}
	if rec.Type == "output" {
		s.pending++
	}
	if sync || s.pending >= syncEvery {
		if err := s.file.Sync(); err != nil {
			return fmt.Errorf("sync %s: %w", s.path, err)
		}
		s.pending = 0
	}
	return nil
}

// Close flushes and closes the stream.
func (s *runStream) Close() error {
	if s.file == nil {
		return nil
	}
	err := s.file.Sync()
	if cerr := s.file.Close(); err == nil {
		err = cerr
	}
	s.file = nil
	return err
}
