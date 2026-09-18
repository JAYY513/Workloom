// Package harness is the execution layer: one adapter interface over the
// harnesses devsys can drive (方案 §9.1/§9.2) plus the shell adapter that
// runs an arbitrary command with line-level streaming.
//
// The package is pure execution: it knows nothing about .devsys state, runs
// or events. The application service wires a session's output to the run
// event stream (方案 §14.2) and to whatever is watching (CLI, dispatch).
package harness

import (
	"context"
	"time"
)

// Capabilities is the §9.2 capability probe. A false flag means the caller
// must not rely on the behaviour; it is not a rejection.
type Capabilities struct {
	NonInteractive bool `json:"supports_non_interactive"`
	JSONOutput     bool `json:"supports_json_output"`
	SessionResume  bool `json:"supports_session_resume"`
	MCP            bool `json:"supports_mcp"`
	Worktree       bool `json:"supports_worktree"`
	Streaming      bool `json:"supports_streaming"`
	ModelSelection bool `json:"supports_model_selection"`
	Approval       bool `json:"supports_approval"`
}

// Availability is what check_available found on this machine.
type Availability struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// Workspace is the execution directory a run is bound to (方案 §4.8). M6.1
// only checks it exists; M6.2 adds the worktree invariants.
type Workspace struct {
	Path     string `json:"path"`
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree,omitempty"`
}

// Request is what the caller asks the adapter to execute. Prompt, Model and
// SessionID are the harness-level knobs of §9.2; the shell adapter ignores
// them and the real adapters (M6.7) render them into their own command line.
type Request struct {
	Command []string
	Dir     string
	Env     []string
	Timeout time.Duration
	// Stdin is fed to the process and then closed. Harness CLIs that read the
	// prompt from stdin use it (方案 §9.3): a prompt is too large to trust to a
	// command line's length limit.
	Stdin     []byte
	Prompt    string
	Model     string
	SessionID string
}

// Command is the concrete invocation build_command produced: argv plus the
// directory and environment it must run in.
type Command struct {
	Argv    []string
	Dir     string
	Env     []string
	Timeout time.Duration
	Stdin   []byte
}

// Line is one streamed output line (stream_output, §9.2). Text carries no
// line terminator. Lines longer than the reader's buffer are emitted as
// several consecutive chunks so no output is dropped.
type Line struct {
	Stream string // "stdout" or "stderr"
	Text   string
	Time   time.Time
}

// Result is collect_result (§9.2): how the attempt ended. Err is a spawn or
// wait failure; a non-zero ExitCode is a normal result, not an error.
type Result struct {
	ExitCode  int
	TimedOut  bool
	Canceled  bool
	StartedAt time.Time
	EndedAt   time.Time
	Err       string
}

// Duration is how long the attempt ran.
func (r Result) Duration() time.Duration { return r.EndedAt.Sub(r.StartedAt) }

// Session is one started attempt. Streamed output, stopping and collecting
// the result live on the session handle rather than on the adapter, so an
// adapter never holds mutable per-attempt state (the Go shape of §9.2's
// stream_output / stop / collect_result).
type Session interface {
	// Lines returns the output stream. The channel is closed once the
	// process has exited and every line has been emitted.
	Lines() <-chan Line
	// Wait blocks until the attempt ended and reports its result.
	Wait(ctx context.Context) (Result, error)
	// Stop terminates the attempt and its process tree.
	Stop(ctx context.Context) error
}

// Adapter is the unified execution interface of §9.2. The mapping to the
// spec's method list is:
//
//	get_name            -> Name
//	check_available     -> Probe
//	get_capabilities    -> Capabilities
//	prepare_workspace   -> Prepare
//	build_command       -> Command
//	start               -> Start (returns a Session)
//	stream_output       -> Session.Lines
//	stop                -> Session.Stop
//	collect_result      -> Session.Wait
type Adapter interface {
	Name() string
	Probe(ctx context.Context) (Availability, error)
	Capabilities() Capabilities
	Prepare(ctx context.Context, ws Workspace) error
	Command(req Request) (Command, error)
	Start(ctx context.Context, cmd Command) (Session, error)
}
