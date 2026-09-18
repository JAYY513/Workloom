package harness

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func argvOf(t *testing.T, adapter Adapter, req Request) []string {
	t.Helper()
	cmd, err := adapter.Command(req)
	if err != nil {
		t.Fatalf("%s command: %v", adapter.Name(), err)
	}
	return cmd.Argv
}

// Each adapter builds the invocation its CLI documents, and delivers the
// prompt the way that CLI can read it.
func TestCLIAdaptersBuildTheirCommands(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name      string
		adapter   Adapter
		req       Request
		wantArgv  []string
		wantStdin bool
	}{
		{
			name:    "codex",
			adapter: Codex(),
			req:     Request{Prompt: "do the thing", Dir: dir},
			// The prompt goes to stdin: "-" tells codex to read it there.
			wantArgv:  []string{"codex", "exec", "--json", "-s", "workspace-write", "-C", dir, "-"},
			wantStdin: true,
		},
		{
			name:      "codex with model",
			adapter:   Codex(),
			req:       Request{Prompt: "do it", Dir: dir, Model: "gpt-5"},
			wantArgv:  []string{"codex", "exec", "--json", "-s", "workspace-write", "-C", dir, "-m", "gpt-5", "-"},
			wantStdin: true,
		},
		{
			name:    "opencode",
			adapter: OpenCode(),
			req:     Request{Prompt: "do the thing", Dir: dir},
			// opencode has no stdin entry: the prompt is the argument.
			wantArgv: []string{"opencode", "run", "--format", "json", "--dir", dir, "do the thing"},
		},
		{
			name:     "opencode with model",
			adapter:  OpenCode(),
			req:      Request{Prompt: "do it", Dir: dir, Model: "anthropic/claude-sonnet-4"},
			wantArgv: []string{"opencode", "run", "--format", "json", "--dir", dir, "-m", "anthropic/claude-sonnet-4", "do it"},
		},
		{
			name:      "claude",
			adapter:   ClaudeCode(),
			req:       Request{Prompt: "do the thing", Dir: dir},
			wantArgv:  []string{"claude", "-p", "--output-format", "stream-json", "--dangerously-skip-permissions"},
			wantStdin: true,
		},
		{
			name:      "claude with model and session",
			adapter:   ClaudeCode(),
			req:       Request{Prompt: "continue", Dir: dir, Model: "opus", SessionID: "sess-1"},
			wantArgv:  []string{"claude", "-p", "--output-format", "stream-json", "--dangerously-skip-permissions", "--model", "opus", "--resume", "sess-1"},
			wantStdin: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := tc.adapter.Command(tc.req)
			if err != nil {
				t.Fatalf("command: %v", err)
			}
			if strings.Join(cmd.Argv, " ") != strings.Join(tc.wantArgv, " ") {
				t.Fatalf("argv = %v, want %v", cmd.Argv, tc.wantArgv)
			}
			if tc.wantStdin && string(cmd.Stdin) != tc.req.Prompt {
				t.Fatalf("stdin = %q, want the prompt", cmd.Stdin)
			}
			if !tc.wantStdin && len(cmd.Stdin) != 0 {
				t.Fatalf("stdin = %q, want none", cmd.Stdin)
			}
			if cmd.Dir != dir {
				t.Fatalf("dir = %q, want %q", cmd.Dir, dir)
			}
		})
	}
}

// A harness that needs a prompt refuses without one instead of starting a CLI
// that would sit waiting.
func TestCLIAdaptersRequireAPrompt(t *testing.T) {
	for _, adapter := range []Adapter{Codex(), OpenCode(), ClaudeCode()} {
		if _, err := adapter.Command(Request{Dir: t.TempDir()}); err == nil {
			t.Fatalf("%s accepted an empty prompt", adapter.Name())
		}
	}
}

// Capabilities are what each CLI actually offers; a harness must not claim
// what it cannot do.
func TestCLIAdapterCapabilities(t *testing.T) {
	codex := Codex().Capabilities()
	if !codex.NonInteractive || !codex.Streaming || !codex.SessionResume || !codex.ModelSelection || !codex.MCP {
		t.Fatalf("codex capabilities = %+v", codex)
	}
	opencode := OpenCode().Capabilities()
	if !opencode.NonInteractive || !opencode.Streaming || !opencode.SessionResume || !opencode.ModelSelection {
		t.Fatalf("opencode capabilities = %+v", opencode)
	}
	if opencode.MCP || opencode.Approval {
		t.Fatalf("opencode claims a capability it does not have: %+v", opencode)
	}
	claude := ClaudeCode().Capabilities()
	if !claude.NonInteractive || !claude.Streaming || !claude.SessionResume || !claude.ModelSelection {
		t.Fatalf("claude capabilities = %+v", claude)
	}
}

// Probe reports what is installed on this machine, and never errors: an absent
// harness is a fact the caller turns into a refusal.
func TestCLIAdapterProbe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, name := range []string{"codex", "opencode", "claude"} {
		adapter, ok := ByName(name)
		if !ok {
			t.Fatalf("ByName(%q) failed", name)
		}
		avail, err := adapter.Probe(ctx)
		if err != nil {
			t.Fatalf("%s probe: %v", name, err)
		}
		_, onPath := exec.LookPath(name)
		if onPath != nil && avail.Installed {
			t.Fatalf("%s reported installed but is not on PATH", name)
		}
		if onPath == nil && !avail.Installed {
			t.Fatalf("%s is on PATH but reported missing", name)
		}
		if avail.Detail == "" {
			t.Fatalf("%s probe detail is empty", name)
		}
		if avail.Installed && avail.Version == "" {
			t.Fatalf("%s reported installed without a version", name)
		}
	}
}

func TestByNameAndNames(t *testing.T) {
	for _, name := range Names() {
		if _, ok := ByName(name); !ok {
			t.Fatalf("Names() lists %q but ByName does not resolve it", name)
		}
	}
	if adapter, ok := ByName("  Codex "); !ok || adapter.Name() != "codex" {
		t.Fatalf("ByName is not tolerant of case or spaces: %v %v", adapter, ok)
	}
	if _, ok := ByName("harness-that-does-not-exist"); ok {
		t.Fatal("an unknown harness name resolved")
	}
}

// The prompt really reaches the process: the helper echoes its stdin.
func TestShellFeedsStdin(t *testing.T) {
	adapter := NewShell()
	cmd, err := adapter.Command(Request{
		Command: []string{os.Args[0]}, Dir: t.TempDir(),
		Env:   []string{roleEnv + "=echo-stdin"},
		Stdin: []byte("the prompt arrives\n"),
	})
	if err != nil {
		t.Fatalf("command: %v", err)
	}
	sess, err := adapter.Start(context.Background(), cmd)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	stdout, _, res := collect(t, sess)
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d", res.ExitCode)
	}
	joined := strings.Join(stdout, "\n")
	if !strings.Contains(joined, "the prompt arrives") {
		t.Fatalf("stdout = %q, want the stdin payload echoed", joined)
	}
}
