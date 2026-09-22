package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// lineBuffer bounds one emitted line: a longer line is emitted as several
// chunks of at most this many bytes, so a runaway single-line writer cannot
// exhaust memory and no output is dropped.
const lineBuffer = 64 << 10

// waitDelay bounds how long the attempt waits for its output streams after the
// process exited. Without it, a descendant that inherited the streams and
// outlives the command would hold the attempt open forever.
const waitDelay = 2 * time.Second

// Shell runs an arbitrary command as argv, without shell interpretation: to
// get shell features the caller passes them explicitly ("sh -c …", "cmd /c
// …"). It is the minimal adapter of §9.3 and the baseline the real harness
// adapters are measured against.
type Shell struct{}

// NewShell returns the shell adapter.
func NewShell() *Shell { return &Shell{} }

// Name implements Adapter.
func (s *Shell) Name() string { return "shell" }

// Capabilities implements Adapter: the shell runs non-interactively, streams
// its output and works inside any workspace directory.
func (s *Shell) Capabilities() Capabilities {
	return Capabilities{NonInteractive: true, Streaming: true, Worktree: true}
}

// Probe implements Adapter. The shell adapter can always spawn argv, so it is
// available wherever devsys runs; the report names the host shell it would
// use for callers that want shell features.
func (s *Shell) Probe(ctx context.Context) (Availability, error) {
	name, hint := "sh", "POSIX"
	if runtime.GOOS == "windows" {
		name, hint = "cmd", "Windows"
	}
	detail := fmt.Sprintf("argv passthrough on %s (%s)", runtime.GOOS, hint)
	path, err := exec.LookPath(name)
	if err != nil {
		// Still installed: argv works without a shell on PATH.
		return Availability{Installed: true, Detail: detail + "; " + name + " not on PATH"}, nil
	}
	return Availability{Installed: true, Version: path, Detail: detail}, nil
}

// Prepare implements Adapter: the execution directory must exist and be a
// directory. The workspace invariants of §4.8 (root containment, worktree
// creation, hooks) arrive with M6.2.
func (s *Shell) Prepare(ctx context.Context, ws Workspace) error {
	if strings.TrimSpace(ws.Path) == "" {
		return errors.New("workspace path is required")
	}
	info, err := os.Stat(ws.Path)
	if err != nil {
		return fmt.Errorf("workspace %s: %w", ws.Path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace %s is not a directory", ws.Path)
	}
	return nil
}

// Command implements Adapter: validate the request and freeze the invocation.
func (s *Shell) Command(req Request) (Command, error) {
	if len(req.Command) == 0 || strings.TrimSpace(req.Command[0]) == "" {
		return Command{}, errors.New("command is required")
	}
	dir := req.Dir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return Command{}, fmt.Errorf("resolve working directory: %w", err)
		}
		dir = wd
	}
	info, err := os.Stat(dir)
	if err != nil {
		return Command{}, fmt.Errorf("working directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return Command{}, fmt.Errorf("working directory %s is not a directory", dir)
	}
	argv := make([]string, len(req.Command))
	copy(argv, req.Command)
	env := make([]string, len(req.Env))
	copy(env, req.Env)
	stdin := make([]byte, len(req.Stdin))
	copy(stdin, req.Stdin)
	return Command{Argv: argv, Dir: dir, Env: env, Timeout: req.Timeout, Stdin: stdin}, nil
}

// Start implements Adapter: spawn the command, stream both output streams
// line by line, and terminate the whole process tree on timeout or stop.
func (s *Shell) Start(ctx context.Context, cmd Command) (Session, error) {
	if len(cmd.Argv) == 0 {
		return nil, errors.New("command is required")
	}
	proc := exec.Command(cmd.Argv[0], cmd.Argv[1:]...)
	proc.Dir = cmd.Dir
	if len(cmd.Env) > 0 {
		proc.Env = append(os.Environ(), cmd.Env...)
	}
	// A dedicated process group (POSIX) so stopping an attempt cannot leave
	// grandchildren behind (方案 §4.8).
	configureProcessGroup(proc)

	sess := &shellSession{
		lines:   make(chan Line, 256),
		discard: make(chan struct{}),
		started: time.Now().UTC(),
		done:    make(chan struct{}),
	}
	// Output is captured through writers, not pipes we read ourselves: os/exec
	// then owns the copying, so Wait returns only after both streams are
	// drained (calling Wait while we still read the pipes would close them
	// under the reader and silently drop buffered output), and WaitDelay
	// bounds the case where a descendant keeps the streams open.
	proc.Stdout = &lineWriter{sess: sess, stream: "stdout"}
	proc.Stderr = &lineWriter{sess: sess, stream: "stderr"}
	proc.WaitDelay = waitDelay

	if len(cmd.Stdin) > 0 {
		pipe, err := proc.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("stdin pipe: %w", err)
		}
		go feedStdin(pipe, cmd.Stdin, sess)
	}
	if err := proc.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cmd.Argv[0], err)
	}
	sess.proc = proc

	go sess.reap()

	if cmd.Timeout > 0 {
		sess.timer = time.AfterFunc(cmd.Timeout, func() {
			sess.markTimedOut()
			sess.kill()
		})
	}
	go func() {
		select {
		case <-ctx.Done():
			sess.markCanceled()
			sess.kill()
		case <-sess.done:
		}
	}()
	return sess, nil
}

// feedStdin writes the prompt and closes the pipe. A write failure is part of
// the record, not a spawn failure: the process may have exited early, and its
// exit status is what the attempt is judged by.
func feedStdin(pipe io.WriteCloser, payload []byte, sess *shellSession) {
	if _, err := pipe.Write(payload); err != nil {
		sess.emit(Line{Stream: "stderr", Text: "workloom: stdin write failed: " + err.Error(), Time: time.Now().UTC()})
	}
	_ = pipe.Close()
}

type shellSession struct {
	proc    *exec.Cmd
	lines   chan Line
	discard chan struct{}
	started time.Time
	done    chan struct{}
	timer   *time.Timer

	mu       sync.Mutex
	res      Result
	stopOnce sync.Once
	timedOut bool
	canceled bool
}

func (s *shellSession) Lines() <-chan Line { return s.lines }

// Wait implements Session: it blocks until the process exited and both
// streams were drained, then reports the result.
func (s *shellSession) Wait(ctx context.Context) (Result, error) {
	select {
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.res, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

// Stop implements Session: terminate the process tree and wait for the
// attempt to be collected. Stopping an attempt that already ended is a no-op,
// so callers can defer it unconditionally.
func (s *shellSession) Stop(ctx context.Context) error {
	select {
	case <-s.done:
		return nil
	default:
	}
	s.markCanceled()
	s.kill()
	_, err := s.Wait(ctx)
	return err
}

func (s *shellSession) markTimedOut() {
	s.mu.Lock()
	s.timedOut = true
	s.mu.Unlock()
}

func (s *shellSession) markCanceled() {
	s.mu.Lock()
	s.canceled = true
	s.mu.Unlock()
}

// kill terminates the process tree once, whichever trigger fires first, and
// releases any writer still blocked on a full channel: once the attempt is
// being killed, output that has not been handed over yet is dropped rather
// than keeping the attempt (and its goroutines) alive.
func (s *shellSession) kill() {
	s.stopOnce.Do(func() {
		close(s.discard)
		if s.proc.Process != nil {
			_ = killTree(s.proc.Process)
		}
	})
}

// reap collects the exit status and closes the stream. By the time Wait
// returns, os/exec has drained both streams through the line writers.
func (s *shellSession) reap() {
	err := s.proc.Wait()
	close(s.lines)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.res.StartedAt = s.started
	s.res.EndedAt = time.Now().UTC()
	s.res.TimedOut = s.timedOut
	s.res.Canceled = s.canceled
	if state := s.proc.ProcessState; state != nil {
		// ExitCode is -1 when the process was killed by a signal: the record
		// then carries no exit status, which is why TimedOut/Canceled/Err
		// exist alongside it.
		s.res.ExitCode = state.ExitCode()
	} else {
		s.res.ExitCode = -1
	}
	switch {
	case err == nil:
	case errors.Is(err, exec.ErrWaitDelay):
		s.res.Err = "output streams stayed open after the command exited"
	default:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			s.res.Err = err.Error()
		}
	}
	close(s.done)
}

// lineWriter turns a byte stream into Lines: it buffers until a line
// terminator arrives, flushes an over-long line as a chunk on a rune boundary,
// and never splits a rune. Writes are called by os/exec's copying goroutines.
type lineWriter struct {
	sess   *shellSession
	stream string
	buf    []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		if i := bytes.IndexByte(w.buf, '\n'); i >= 0 {
			line := w.buf[:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1] // the CR of a CRLF terminator
			}
			w.sess.emit(Line{Stream: w.stream, Text: string(line), Time: time.Now().UTC()})
			w.buf = append(w.buf[:0], w.buf[i+1:]...)
			continue
		}
		if len(w.buf) >= lineBuffer {
			cut := lineBuffer
			if tail := incompleteTail(w.buf[:cut]); tail > 0 {
				cut -= tail // keep the split rune for the next chunk
			}
			w.sess.emit(Line{Stream: w.stream, Text: string(w.buf[:cut]), Time: time.Now().UTC()})
			w.buf = append(w.buf[:0], w.buf[cut:]...)
			continue
		}
		return len(p), nil
	}
}

// emit hands one line to the consumer, or drops it once the attempt is being
// killed and nobody may be listening any more.
func (s *shellSession) emit(line Line) {
	select {
	case s.lines <- line:
	case <-s.discard:
	}
}

// incompleteTail reports how many trailing bytes of b belong to a UTF-8 rune
// that is cut short (0 when b ends on a rune boundary).
func incompleteTail(b []byte) int {
	for i := 1; i <= utf8.UTFMax; i++ {
		idx := len(b) - i
		if idx < 0 {
			return 0
		}
		c := b[idx]
		if c < utf8.RuneSelf {
			return 0 // ASCII start byte: the rune is complete
		}
		if c&0xC0 == 0x80 {
			continue // continuation byte: keep scanning back
		}
		var want int
		switch {
		case c&0xE0 == 0xC0:
			want = 2
		case c&0xF0 == 0xE0:
			want = 3
		case c&0xF8 == 0xF0:
			want = 4
		default:
			return 0 // invalid start byte: not a truncated rune
		}
		if idx+want <= len(b) {
			return 0 // complete
		}
		return len(b) - idx
	}
	return 0
}
