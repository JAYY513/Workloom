package harness

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// probeTimeout bounds a capability probe: a harness that cannot answer
// `--version` quickly is not usable for an attempt.
const probeTimeout = 10 * time.Second

// cliAdapter is a harness driven through its own non-interactive CLI (方案
// §9.3). It owns the two things that differ between harnesses — the command
// line it builds and what it can do — and delegates execution to the shell
// adapter, so streaming, timeouts and process-tree termination stay one
// implementation.
type cliAdapter struct {
	name  string
	bin   string
	caps  Capabilities
	build func(req Request) (argv []string, stdin []byte, err error)
	shell *Shell
}

// Name implements Adapter.
func (a *cliAdapter) Name() string { return a.name }

// Capabilities implements Adapter.
func (a *cliAdapter) Capabilities() Capabilities { return a.caps }

// Prepare implements Adapter: the workspace invariants are the shell
// adapter's, unchanged (方案 §4.8).
func (a *cliAdapter) Prepare(ctx context.Context, ws Workspace) error {
	return a.shell.Prepare(ctx, ws)
}

// Probe implements Adapter: it asks the CLI for its version. A harness that is
// not installed is reported as such — never silently swapped for another one.
func (a *cliAdapter) Probe(ctx context.Context) (Availability, error) {
	path, err := exec.LookPath(a.bin)
	if err != nil {
		return Availability{Installed: false, Detail: a.bin + " is not on PATH"}, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, path, "--version").Output()
	if err != nil {
		return Availability{Installed: true, Detail: fmt.Sprintf("%s found at %s but --version failed: %v", a.name, path, err)}, nil
	}
	return Availability{Installed: true, Version: strings.TrimSpace(string(out)), Detail: path}, nil
}

// Command implements Adapter. The builder returns the arguments; the binary
// name leads the argv (the acceptance run caught exactly this: the adapters
// started "exec" instead of "codex exec").
func (a *cliAdapter) Command(req Request) (Command, error) {
	args, stdin, err := a.build(req)
	if err != nil {
		return Command{}, err
	}
	shellReq := req
	shellReq.Command = append([]string{a.bin}, args...)
	shellReq.Stdin = stdin
	return a.shell.Command(shellReq)
}

// Start implements Adapter: the shell adapter runs the process.
func (a *cliAdapter) Start(ctx context.Context, cmd Command) (Session, error) {
	return a.shell.Start(ctx, cmd)
}

// Codex drives codex-cli's non-interactive mode. The prompt goes to stdin
// (`-`): a full round's brief is far larger than a command line should carry.
func Codex() Adapter {
	return &cliAdapter{
		name: "codex",
		bin:  "codex",
		caps: Capabilities{
			NonInteractive: true, JSONOutput: true, SessionResume: true, MCP: true,
			Worktree: true, Streaming: true, ModelSelection: true, Approval: true,
		},
		shell: NewShell(),
		build: func(req Request) ([]string, []byte, error) {
			if strings.TrimSpace(req.Prompt) == "" {
				return nil, nil, errors.New("codex needs a prompt")
			}
			// workspace-write is what an unattended attempt needs: devsys
			// already isolates the attempt in its own worktree (方案 §4.8),
			// and codex's default sandbox is read-only, which would leave the
			// agent unable to do the work at all.
			argv := []string{"exec", "--json", "-s", "workspace-write", "-C", req.Dir}
			if req.Model != "" {
				argv = append(argv, "-m", req.Model)
			}
			if req.SessionID != "" {
				argv = append(argv, "resume", req.SessionID)
			} else {
				argv = append(argv, "-")
			}
			return argv, []byte(req.Prompt), nil
		},
	}
}

// OpenCode drives `opencode run`. It has no stdin entry, so the prompt is the
// positional argument — a documented length caveat.
func OpenCode() Adapter {
	return &cliAdapter{
		name: "opencode",
		bin:  "opencode",
		caps: Capabilities{
			NonInteractive: true, JSONOutput: true, SessionResume: true,
			Worktree: true, Streaming: true, ModelSelection: true,
		},
		shell: NewShell(),
		build: func(req Request) ([]string, []byte, error) {
			if strings.TrimSpace(req.Prompt) == "" {
				return nil, nil, errors.New("opencode needs a prompt")
			}
			argv := []string{"run", "--format", "json", "--dir", req.Dir}
			if req.Model != "" {
				argv = append(argv, "-m", req.Model)
			}
			if req.SessionID != "" {
				argv = append(argv, "--session", req.SessionID)
			}
			return append(argv, req.Prompt), nil, nil
		},
	}
}

// ClaudeCode drives `claude -p`. The prompt goes to stdin, like codex.
func ClaudeCode() Adapter {
	return &cliAdapter{
		name: "claude",
		bin:  "claude",
		caps: Capabilities{
			NonInteractive: true, JSONOutput: true, SessionResume: true,
			Worktree: true, Streaming: true, ModelSelection: true,
		},
		shell: NewShell(),
		build: func(req Request) ([]string, []byte, error) {
			if strings.TrimSpace(req.Prompt) == "" {
				return nil, nil, errors.New("claude needs a prompt")
			}
			// The attempt is already isolated in its own worktree (方案 §4.8),
			// so the unattended mode is the documented one.
			argv := []string{"-p", "--output-format", "stream-json", "--dangerously-skip-permissions"}
			if req.Model != "" {
				argv = append(argv, "--model", req.Model)
			}
			if req.SessionID != "" {
				argv = append(argv, "--resume", req.SessionID)
			}
			return argv, []byte(req.Prompt), nil
		},
	}
}

// ByName resolves a harness name to its adapter. The name set is fixed: an
// unknown name is a caller error, not something to guess at.
func ByName(name string) (Adapter, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "shell":
		return NewShell(), true
	case "codex":
		return Codex(), true
	case "opencode":
		return OpenCode(), true
	case "claude":
		return ClaudeCode(), true
	default:
		return nil, false
	}
}

// Names lists the harnesses devsys can drive.
func Names() []string { return []string{"shell", "codex", "opencode", "claude"} }
