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
)

// syncEvery bounds how much of the output stream can be lost in a crash: the
// stream is written line by line and flushed to stable storage every this many
// lines, on every control record, and when the attempt ends.
const syncEvery = 128

// stopGrace bounds how long a failing path waits for a stopped attempt to be
// collected, so an abandoned attempt cannot hold a caller open.
const stopGrace = 10 * time.Second

// RunExecRequest is one attempt driven through a harness adapter (实施计划
// M6.1). Sink receives every streamed line as it is persisted; nil means
// nobody is watching (dispatch, background execution).
type RunExecRequest struct {
	RunID   string
	Argv    []string
	Timeout time.Duration
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
	ExitCode   int    `json:"exit_code"`
	TimedOut   bool   `json:"timed_out"`
	Canceled   bool   `json:"canceled"`
	DurationMS int64  `json:"duration_ms"`
	Lines      int    `json:"lines"`
	Log        string `json:"log"`
}

// RunExec runs one command for a run: it resolves the adapter, streams both
// output streams into the run's event stream (.devsys/runs/<id>.jsonl, 方案
// §14.2) while forwarding them to the sink, then records the evidence on the
// run and in the event log. Lifecycle transitions (status/phase, retries) stay
// with the executor paths (M6.3/M6.4); this records what happened.
func (s *Service) RunExec(ctx context.Context, req RunExecRequest) (RunExecView, error) {
	if req.RunID == "" {
		return RunExecView{}, Usagef("run exec requires a run id")
	}
	if len(req.Argv) == 0 {
		return RunExecView{}, Usagef("run exec requires a command (devsys run exec --id <run-id> -- <command...>)")
	}
	if req.Actor == "" || req.Reason == "" {
		return RunExecView{}, Usagef("run exec requires actor and reason")
	}
	if _, err := s.project(); err != nil {
		return RunExecView{}, err
	}
	r, err := run.New(s.Root).Get(ctx, req.RunID)
	if err != nil {
		return RunExecView{}, s.storeError(err)
	}

	adapter := harness.NewShell()
	dir := r.Workspace.Path
	if dir == "" {
		dir = s.Root
	}
	if err := adapter.Prepare(ctx, harness.Workspace{Path: dir, Branch: r.Workspace.Branch, Worktree: r.Workspace.Worktree}); err != nil {
		return RunExecView{}, Preconditionf("%v", err)
	}
	cmd, err := adapter.Command(harness.Request{Command: req.Argv, Dir: dir, Timeout: req.Timeout})
	if err != nil {
		return RunExecView{}, Preconditionf("%v", err)
	}
	commandLine := strings.Join(cmd.Argv, " ")
	logRel := logRelFor(r.ID)

	stream, err := openRunStream(s.Root, r.ID)
	if err != nil {
		return RunExecView{}, Internalf("open run stream: %v", err)
	}
	defer stream.Close()
	if err := stream.write(streamRecord{
		Type: "start", Adapter: adapter.Name(), Command: commandLine, Argv: cmd.Argv,
		Cwd: cmd.Dir, TimeoutMS: cmd.Timeout.Milliseconds(),
	}, true); err != nil {
		return RunExecView{}, Internalf("write run stream: %v", err)
	}
	if err := events.New(s.Root).Append(ctx, &domain.Event{
		Type:      "run_exec_started",
		Subject:   domain.Reference{Type: "run", ID: r.ID},
		ProjectID: r.ProjectID,
		Actor:     req.Actor,
		Related:   []domain.Reference{{Type: "workitem", ID: r.WorkItemID}},
		Content:   fmt.Sprintf("%s (%s in %s)", commandLine, adapter.Name(), cmd.Dir),
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

	lines := 0
	for line := range sess.Lines() {
		lines++
		if err := stream.write(streamRecord{Type: "output", Stream: line.Stream, Line: line.Text, Time: line.Time}, false); err != nil {
			return RunExecView{}, Internalf("write run stream: %v", err)
		}
		if req.Sink != nil {
			req.Sink(line)
		}
	}
	// Collection is not abortable: the stream has closed, so the attempt is
	// over, and its outcome is the evidence this path exists to record.
	res, err := sess.Wait(context.Background())
	if err != nil {
		return RunExecView{}, Internalf("collect result: %v", err)
	}
	code := res.ExitCode
	if err := stream.write(streamRecord{
		Type: "exit", Code: &code, TimedOut: res.TimedOut, Canceled: res.Canceled,
		DurationMS: res.Duration().Milliseconds(), Text: res.Err,
	}, true); err != nil {
		return RunExecView{}, Internalf("write run stream: %v", err)
	}
	if err := s.recordExecOutcome(ctx, r, req.Actor, commandLine, execNote(res, commandLine), execSummary(res, commandLine)); err != nil {
		return RunExecView{}, err
	}

	return RunExecView{
		RunID: r.ID, Adapter: adapter.Name(), Command: commandLine, Dir: cmd.Dir,
		ExitCode: res.ExitCode, TimedOut: res.TimedOut, Canceled: res.Canceled,
		DurationMS: res.Duration().Milliseconds(), Lines: lines, Log: logRel,
	}, nil
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

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// streamRecord is one line of a run's event stream. One struct covers every
// record type so the on-disk shape is documented in one place; omitempty keeps
// each record small. code is -1 when the command has no exit status.
type streamRecord struct {
	Time       time.Time `json:"t"`
	Type       string    `json:"type"`
	Stream     string    `json:"stream,omitempty"`
	Line       string    `json:"line,omitempty"`
	Adapter    string    `json:"adapter,omitempty"`
	Command    string    `json:"command,omitempty"`
	Argv       []string  `json:"argv,omitempty"`
	Cwd        string    `json:"cwd,omitempty"`
	TimeoutMS  int64     `json:"timeout_ms,omitempty"`
	Code       *int      `json:"code,omitempty"`
	TimedOut   bool      `json:"timed_out,omitempty"`
	Canceled   bool      `json:"canceled,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Text       string    `json:"text,omitempty"`
	Removed    int64     `json:"removed_bytes,omitempty"`
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
