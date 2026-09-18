package harness

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// The adapter is exercised against helper processes of this same test binary:
// no shell dialect, no external tools, and every stream is under our control.
const (
	roleEnv    = "DEVSYS_HARNESS_HELPER"
	exitEnv    = "DEVSYS_HARNESS_EXIT"
	bytesEnv   = "DEVSYS_HARNESS_BYTES"
	unitEnv    = "DEVSYS_HARNESS_UNIT"
	countEnv   = "DEVSYS_HARNESS_COUNT"
	ticksEnv   = "DEVSYS_HARNESS_TICKS"
	intervalMS = 50
)

func TestMain(m *testing.M) {
	if role := os.Getenv(roleEnv); role != "" {
		helperProcess(role)
		return
	}
	os.Exit(m.Run())
}

func helperProcess(role string) {
	switch role {
	case "emit":
		os.Stdout.WriteString("out-1\nout-2\n")
		os.Stderr.WriteString("err-1\n")
		code, _ := strconv.Atoi(os.Getenv(exitEnv))
		os.Exit(code)
	case "longline":
		unit := os.Getenv(unitEnv)
		if unit == "" {
			unit = "x"
		}
		count, _ := strconv.Atoi(os.Getenv(countEnv))
		os.Stdout.WriteString(strings.Repeat(unit, count))
		os.Stdout.WriteString("\ntail\n")
		os.Exit(0)
	case "tree":
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), roleEnv+"=grandchild")
		child.Stdout, child.Stderr = nil, nil
		if err := child.Start(); err != nil {
			os.Exit(3)
		}
		// Block until the process tree is terminated. A bare select{} would
		// trip the runtime's deadlock detector (exit 2) and orphan the
		// grandchild before the adapter ever gets to kill it.
		time.Sleep(time.Hour)
		os.Exit(0)
	case "burst":
		count, _ := strconv.Atoi(os.Getenv(countEnv))
		var b strings.Builder
		for i := 0; i < count; i++ {
			fmt.Fprintf(&b, "line-%d\n", i)
		}
		os.Stdout.WriteString(b.String())
		os.Exit(0)
	case "carriage":
		os.Stdout.WriteString(strings.Repeat("x", lineBuffer))
		os.Stdout.WriteString("\r")
		os.Stdout.WriteString("y\n")
		os.Exit(0)
	case "grandchild":
		path := os.Getenv(ticksEnv)
		for {
			if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				f.WriteString("tick\n")
				f.Close()
			}
			time.Sleep(intervalMS * time.Millisecond)
		}
	}
	os.Exit(2)
}

// startHelper runs the test binary in the given helper role.
func startHelper(t *testing.T, env map[string]string) (*Shell, Session, context.CancelFunc) {
	t.Helper()
	adapter := NewShell()
	ctx, cancel := context.WithCancel(context.Background())
	cmd, err := adapter.Command(Request{Command: []string{os.Args[0]}, Dir: t.TempDir()})
	if err != nil {
		cancel()
		t.Fatalf("command: %v", err)
	}
	cmd.Env = []string{roleEnv + "=" + env["role"]}
	for k, v := range env {
		if k != "role" {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	sess, err := adapter.Start(ctx, cmd)
	if err != nil {
		cancel()
		t.Fatalf("start: %v", err)
	}
	return adapter, sess, cancel
}

func collect(t *testing.T, sess Session) (stdout, stderr []string, res Result) {
	t.Helper()
	for line := range sess.Lines() {
		switch line.Stream {
		case "stdout":
			stdout = append(stdout, line.Text)
		case "stderr":
			stderr = append(stderr, line.Text)
		default:
			t.Fatalf("unknown stream %q", line.Stream)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := sess.Wait(ctx)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	return stdout, stderr, res
}

func TestShellStreamsBothStreams(t *testing.T) {
	_, sess, cancel := startHelper(t, map[string]string{"role": "emit"})
	defer cancel()
	stdout, stderr, res := collect(t, sess)
	if got, want := strings.Join(stdout, ","), "out-1,out-2"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got, want := strings.Join(stderr, ","), "err-1"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	if res.ExitCode != 0 || res.TimedOut || res.Canceled || res.Err != "" {
		t.Fatalf("result = %+v, want a clean exit", res)
	}
	if res.Duration() <= 0 {
		t.Fatalf("duration = %v, want a positive measurement", res.Duration())
	}
}

// A non-zero exit code is a result, not an error: the caller decides what a
// failing command means (M6.6 completion verification).
func TestShellExitCodeIsResultNotError(t *testing.T) {
	_, sess, cancel := startHelper(t, map[string]string{"role": "emit", exitEnv: "7"})
	defer cancel()
	_, _, res := collect(t, sess)
	if res.ExitCode != 7 || res.Err != "" {
		t.Fatalf("result = %+v, want exit 7 without a spawn error", res)
	}
}

// A line longer than the read buffer is emitted as chunks that concatenate
// back to the original text — no truncation, no silent loss.
func TestShellChunksLongLines(t *testing.T) {
	const size = 200000
	_, sess, cancel := startHelper(t, map[string]string{
		"role": "longline", unitEnv: "x", countEnv: strconv.Itoa(size),
	})
	defer cancel()
	stdout, _, res := collect(t, sess)
	if len(stdout) < 4 {
		t.Fatalf("got %d chunks, want several for a %d byte line", len(stdout), size)
	}
	for i, chunk := range stdout {
		if len(chunk) > lineBuffer {
			t.Fatalf("chunk %d is %d bytes, want at most %d", i, len(chunk), lineBuffer)
		}
	}
	body := strings.Join(stdout[:len(stdout)-1], "")
	if len(body) != size || strings.Trim(body, "x") != "" {
		t.Fatalf("reassembled %d bytes (clean=%v), want %d x's", len(body), strings.Trim(body, "x") == "", size)
	}
	if stdout[len(stdout)-1] != "tail" {
		t.Fatalf("last line = %q, want %q", stdout[len(stdout)-1], "tail")
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", res.ExitCode)
	}
}

// Chunk boundaries must not split a multi-byte rune: a chunk is always valid
// UTF-8 and the pieces reassemble byte for byte.
func TestShellChunksSplitOnRuneBoundary(t *testing.T) {
	const count = 40000 // 120000 bytes of 3-byte runes
	_, sess, cancel := startHelper(t, map[string]string{
		"role": "longline", unitEnv: "中", countEnv: strconv.Itoa(count),
	})
	defer cancel()
	stdout, _, _ := collect(t, sess)
	for i, chunk := range stdout[:len(stdout)-1] {
		if !utf8.ValidString(chunk) {
			t.Fatalf("chunk %d is not valid UTF-8: %q", i, chunk)
		}
	}
	body := strings.Join(stdout[:len(stdout)-1], "")
	if got := utf8.RuneCountInString(body); got != count {
		t.Fatalf("reassembled %d runes, want %d", got, count)
	}
}

// A timeout must terminate the whole tree: the grandchild keeps writing ticks
// until it is killed too.
func TestShellTimeoutKillsProcessTree(t *testing.T) {
	ticks := filepath.Join(t.TempDir(), "ticks.log")
	adapter := NewShell()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd, err := adapter.Command(Request{
		Command: []string{os.Args[0]}, Dir: t.TempDir(), Timeout: 700 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("command: %v", err)
	}
	cmd.Env = []string{roleEnv + "=tree", ticksEnv + "=" + ticks}
	sess, err := adapter.Start(ctx, cmd)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	_, _, res := collect(t, sess)
	if !res.TimedOut {
		t.Fatalf("result = %+v, want TimedOut", res)
	}
	assertTreeStopped(t, ticks)
}

// Stopping an attempt cancels it and kills the tree the same way.
func TestShellStopKillsProcessTree(t *testing.T) {
	ticks := filepath.Join(t.TempDir(), "ticks.log")
	adapter := NewShell()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd, err := adapter.Command(Request{Command: []string{os.Args[0]}, Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("command: %v", err)
	}
	cmd.Env = []string{roleEnv + "=tree", ticksEnv + "=" + ticks}
	sess, err := adapter.Start(ctx, cmd)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// Wait until the grandchild is demonstrably alive before stopping.
	waitForTicks(t, ticks)
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	if err := sess.Stop(stopCtx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	res, err := sess.Wait(context.Background())
	if err != nil {
		t.Fatalf("wait after stop: %v", err)
	}
	if !res.Canceled {
		t.Fatalf("result = %+v, want Canceled", res)
	}
	assertTreeStopped(t, ticks)
}

func waitForTicks(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if size := fileSize(path); size > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("grandchild never wrote to %s", path)
}

// assertTreeStopped proves the grandchild is gone: it polls until the tick
// file stops growing (the kill itself can take a moment on a cold system, so
// a fixed sleep would be flaky) and fails if it never does.
func assertTreeStopped(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	last := fileSize(path)
	stableSince := time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		size := fileSize(path)
		if size != last {
			last = size
			stableSince = time.Now()
			continue
		}
		if time.Since(stableSince) >= 500*time.Millisecond {
			return
		}
	}
	t.Fatalf("grandchild kept writing to %s: %d bytes", path, fileSize(path))
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func TestShellStartFailsForMissingExecutable(t *testing.T) {
	adapter := NewShell()
	ctx := context.Background()
	cmd, err := adapter.Command(Request{Command: []string{"devsys-no-such-binary-xyz"}, Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("command: %v", err)
	}
	if _, err := adapter.Start(ctx, cmd); err == nil {
		t.Fatal("start succeeded for a missing executable")
	}
}

func TestShellCommandAndPrepareValidation(t *testing.T) {
	adapter := NewShell()
	if _, err := adapter.Command(Request{Dir: t.TempDir()}); err == nil {
		t.Fatal("empty argv accepted")
	}
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Command(Request{Command: []string{os.Args[0]}, Dir: file}); err == nil {
		t.Fatal("file accepted as working directory")
	}
	if err := adapter.Prepare(context.Background(), Workspace{}); err == nil {
		t.Fatal("empty workspace accepted")
	}
	if err := adapter.Prepare(context.Background(), Workspace{Path: file}); err == nil {
		t.Fatal("file accepted as workspace")
	}
	if err := adapter.Prepare(context.Background(), Workspace{Path: t.TempDir()}); err != nil {
		t.Fatalf("prepare: %v", err)
	}
}

func TestShellProbeAndCapabilities(t *testing.T) {
	adapter := NewShell()
	if adapter.Name() != "shell" {
		t.Fatalf("name = %q", adapter.Name())
	}
	avail, err := adapter.Probe(context.Background())
	if err != nil || !avail.Installed {
		t.Fatalf("probe = %+v err=%v, want installed", avail, err)
	}
	caps := adapter.Capabilities()
	if !caps.NonInteractive || !caps.Streaming || !caps.Worktree {
		t.Fatalf("capabilities = %+v, want non-interactive streaming worktree", caps)
	}
	if caps.SessionResume || caps.MCP || caps.ModelSelection || caps.Approval {
		t.Fatalf("capabilities = %+v, want no harness-only features claimed", caps)
	}
}

// A command that writes a lot and exits immediately must have every line
// captured: reaping before the streams are drained would silently drop the
// tail (the stream exists to be the evidence trail).
func TestShellCapturesFullOutputOfFastWriter(t *testing.T) {
	const lines = 5000
	_, sess, cancel := startHelper(t, map[string]string{"role": "burst", countEnv: strconv.Itoa(lines)})
	defer cancel()
	stdout, _, res := collect(t, sess)
	if len(stdout) != lines {
		t.Fatalf("captured %d lines, want %d", len(stdout), lines)
	}
	for i, line := range stdout {
		if want := fmt.Sprintf("line-%d", i); line != want {
			t.Fatalf("line %d = %q, want %q", i, line, want)
		}
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0", res.ExitCode)
	}
}

// A carriage return that is content (not the CR of a CRLF terminator) is part
// of the evidence: only a CR immediately before the newline is stripped.
func TestShellKeepsContentCarriageReturn(t *testing.T) {
	_, sess, cancel := startHelper(t, map[string]string{"role": "carriage"})
	defer cancel()
	stdout, _, _ := collect(t, sess)
	body := strings.Join(stdout, "")
	if want := strings.Repeat("x", lineBuffer) + "\r" + "y"; body != want {
		t.Fatalf("reassembled %d bytes (trailing CR present=%v), want %d bytes ending in CR+y",
			len(body), strings.Contains(body, "\r"), len(want))
	}
}

// Stopping an attempt that already ended is a no-op: a deferred Stop must not
// relabel a completed attempt as canceled.
func TestShellStopAfterCompletionIsNoop(t *testing.T) {
	_, sess, cancel := startHelper(t, map[string]string{"role": "emit"})
	defer cancel()
	_, _, res := collect(t, sess)
	if err := sess.Stop(context.Background()); err != nil {
		t.Fatalf("stop after completion: %v", err)
	}
	again, err := sess.Wait(context.Background())
	if err != nil {
		t.Fatalf("wait after stop: %v", err)
	}
	if again.Canceled || again != res {
		t.Fatalf("result changed after a late stop: %+v -> %+v", res, again)
	}
}
