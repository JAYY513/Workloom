package workspace

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"workloom/internal/harness"
)

// Hook names and semantics (方案 §4.8, Symphony SPEC §4.4):
//
//	after_create  workspace newly created      fatal: abort creation
//	before_run    before each attempt          fatal: abort the attempt
//	after_run     after each attempt           logged only
//	before_remove before workspace deletion    logged only
const (
	HookAfterCreate  = "after_create"
	HookBeforeRun    = "before_run"
	HookAfterRun     = "after_run"
	HookBeforeRemove = "before_remove"
)

// defaultHookTimeout is the unified timeout used when the policy declares
// none (Symphony SPEC §4.4 default hooks.timeout_ms = 60000).
const defaultHookTimeout = 60 * time.Second

// hookOutputTail bounds the output kept for one hook run: enough to explain a
// failure, not enough to fill a state file with build noise.
const hookOutputTail = 4 << 10

// Hook is one lifecycle hook as the workflow policy declares it.
type Hook struct {
	Command        string `json:"command,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// HookReport is what one hook execution produced; Ran is false when the
// policy declared no command for the hook.
type HookReport struct {
	Name       string `json:"name"`
	Ran        bool   `json:"ran"`
	Command    string `json:"command,omitempty"`
	ExitCode   int    `json:"exit_code,omitempty"`
	TimedOut   bool   `json:"timed_out,omitempty"`
	Canceled   bool   `json:"canceled,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Output     string `json:"output,omitempty"`
}

// HookError is a hook that failed, timed out or was canceled. Callers decide
// the consequence: after_create and before_run are fatal, after_run and
// before_remove are recorded only (方案 §4.8).
type HookError struct {
	Name     string
	ExitCode int
	TimedOut bool
	Canceled bool
	Timeout  time.Duration
	Output   string
}

func (e *HookError) Error() string {
	tail := ""
	if out := strings.TrimSpace(e.Output); out != "" {
		tail = ": " + lastLine(out)
	}
	switch {
	case e.TimedOut:
		return fmt.Sprintf("%s hook timed out after %s%s", e.Name, e.Timeout, tail)
	case e.Canceled:
		return fmt.Sprintf("%s hook was canceled%s", e.Name, tail)
	default:
		return fmt.Sprintf("%s hook failed (exit %d)%s", e.Name, e.ExitCode, tail)
	}
}

// RunHookOptions is one hook execution.
type RunHookOptions struct {
	Name           string
	Hook           Hook
	Dir            string
	Env            []string
	TimeoutSeconds int
}

// RunHook executes one lifecycle hook in dir. The command is a shell script:
// "sh -c" on POSIX hosts, "cmd /c" on Windows (the policy file is
// platform-specific by nature — 方案 §4.8 declares hooks as shell scripts).
//
// Execution goes through the harness shell adapter (M6.1), so a hook gets the
// same line streaming, timeout and process-tree termination an attempt does.
func RunHook(ctx context.Context, opts RunHookOptions) (HookReport, error) {
	rep := HookReport{Name: opts.Name}
	command := strings.TrimSpace(opts.Hook.Command)
	if command == "" {
		return rep, nil
	}
	seconds := opts.Hook.TimeoutSeconds
	if opts.TimeoutSeconds > 0 {
		seconds = opts.TimeoutSeconds
	}
	timeout := defaultHookTimeout
	if seconds > 0 {
		timeout = time.Duration(seconds) * time.Second
	}

	adapter := harness.NewShell()
	cmd, err := adapter.Command(harness.Request{
		Command: hookArgv(command),
		Dir:     opts.Dir,
		Env:     opts.Env,
		Timeout: timeout,
	})
	if err != nil {
		return rep, &HookError{Name: opts.Name, Output: err.Error()}
	}
	sess, err := adapter.Start(ctx, cmd)
	if err != nil {
		return rep, &HookError{Name: opts.Name, Output: err.Error()}
	}
	var out tailBuffer
	for line := range sess.Lines() {
		out.WriteString(line.Text)
		out.WriteByte('\n')
	}
	res, err := sess.Wait(ctx)
	if err != nil {
		return rep, &HookError{Name: opts.Name, Output: err.Error()}
	}

	rep.Ran = true
	rep.Command = command
	rep.ExitCode = res.ExitCode
	rep.TimedOut = res.TimedOut
	rep.Canceled = res.Canceled
	rep.DurationMS = res.Duration().Milliseconds()
	rep.Output = strings.TrimRight(out.String(), "\n")

	switch {
	case res.TimedOut:
		return rep, &HookError{Name: opts.Name, TimedOut: true, Timeout: timeout, ExitCode: res.ExitCode, Output: rep.Output}
	case res.Canceled:
		return rep, &HookError{Name: opts.Name, Canceled: true, Output: rep.Output}
	case res.ExitCode != 0:
		return rep, &HookError{Name: opts.Name, ExitCode: res.ExitCode, Output: rep.Output}
	}
	return rep, nil
}

// hookArgv wraps a hook script in the host shell.
func hookArgv(command string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", command}
	}
	return []string{"sh", "-c", command}
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) WriteString(s string) { t.Write([]byte(s)) }

// WriteByte appends one byte: the hook loop terminates each line with it.
func (t *tailBuffer) WriteByte(b byte) error {
	_, err := t.Write([]byte{b})
	return err
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	if t.max == 0 {
		t.max = hookOutputTail
	}
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return string(t.buf) }

// lastLine returns the final non-empty line of output, for one-line error
// rendering.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
