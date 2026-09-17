package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Helper exit codes shared by the parent tests and the child process.
const (
	exitCodeOK          = 0
	exitCodeConflict    = 3
	exitCodeCrash       = 9
	exitCodeHelperFault = 10
)

// Environment variables driving the helper process.
const (
	envHelper  = "DEVSYS_STORE_HELPER"
	envRoot    = "DEVSYS_STORE_ROOT"
	envCrashAt = "DEVSYS_STORE_CRASH_AT"
	envHoldMS  = "DEVSYS_STORE_HOLD_MS"
	envStart   = "DEVSYS_STORE_START_FILE"
	envExpect  = "DEVSYS_STORE_EXPECT"
	envErrFile = "DEVSYS_STORE_ERRFILE"
	envRacerID = "DEVSYS_STORE_RACER_ID"
)

// Seed and committed content used by the transaction tests.
const (
	seedStateV1 = "version 1\n"
	newStateV2  = "version 2\n"
	eventsRel   = "events/2026-09.jsonl"
	stateRel    = "state/current.yaml"
	seedEvent   = "{\"event\":\"seeded\"}\n"
	newEvent    = "{\"event\":\"created\",\"id\":\"evt-1\"}\n"
)

// TestHelperProcess is not a real test: it implements the child-process modes
// used by the storage tests. It runs only when DEVSYS_STORE_HELPER is set, and
// re-executing the test binary is how the package observes real process
// interruption and real cross-process locking.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv(envHelper)
	if mode == "" {
		t.Skip("helper mode not requested")
	}
	root := os.Getenv(envRoot)
	switch mode {
	case "crash":
		helperCrash(root)
	case "holder":
		helperHolder(root)
	case "racer":
		helperRacer(root)
	default:
		helperFault(fmt.Errorf("unknown helper mode %q", mode))
	}
	os.Exit(exitCodeOK)
}

func helperFault(err error) {
	if path := os.Getenv(envErrFile); path != "" {
		_ = os.WriteFile(path, []byte(err.Error()+"\n"), 0o644)
	}
	os.Exit(exitCodeHelperFault)
}

func helperOpen(root string) *Store {
	s, err := Open(root, Options{LockTimeout: 30 * time.Second})
	if err != nil {
		helperFault(err)
	}
	return s
}

// helperCrash runs one two-op transaction and exits the process at the requested
// durable stage, simulating an interruption between disk writes.
func helperCrash(root string) {
	want := os.Getenv(envCrashAt)
	txnStageHook = func(stage string) {
		if stage == want {
			os.Exit(exitCodeCrash)
		}
	}
	s := helperOpen(root)
	err := s.Write(context.Background(), func(tx *Tx) error {
		if err := tx.Put(stateRel, []byte(newStateV2), ExpectHash(HashBytes([]byte(seedStateV1)))); err != nil {
			return err
		}
		return tx.AppendJSONL(eventsRel, map[string]string{"event": "created", "id": "evt-1"})
	})
	if err != nil {
		helperFault(err)
	}
	helperFault(errors.New("requested crash stage was never reached"))
}

// helperHolder takes the project lock, records that it did so and holds it for
// a while, so a parent test can observe blocking behaviour.
func helperHolder(root string) {
	holdMS, _ := strconv.Atoi(os.Getenv(envHoldMS))
	if holdMS <= 0 {
		holdMS = 500
	}
	s := helperOpen(root)
	err := s.Write(context.Background(), func(tx *Tx) error {
		if start := os.Getenv(envStart); start != "" {
			_ = os.WriteFile(start, []byte("holding\n"), 0o644)
		}
		time.Sleep(time.Duration(holdMS) * time.Millisecond)
		return nil
	})
	if err != nil {
		helperFault(err)
	}
}

// helperRacer performs one compare-and-set update after a shared start signal,
// so several processes contend for the same expected version.
func helperRacer(root string) {
	waitForStart()
	expect, err := parseExpect(os.Getenv(envExpect))
	if err != nil {
		helperFault(err)
	}
	s := helperOpen(root)
	err = s.Write(context.Background(), func(tx *Tx) error {
		return tx.Put(stateRel, []byte("winner "+os.Getenv(envRacerID)+"\n"), expect)
	})
	switch {
	case err == nil:
		os.Exit(exitCodeOK)
	case errors.Is(err, ErrConflict):
		os.Exit(exitCodeConflict)
	default:
		helperFault(err)
	}
}

func waitForStart() {
	start := os.Getenv(envStart)
	if start == "" {
		return
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(start); err == nil {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	helperFault(errors.New("start signal never appeared"))
}

// waitForFile blocks until path exists, for tests that coordinate with a child
// process through a signal file.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("signal file %s never appeared", path)
}

// helperCmd builds a child process running TestHelperProcess in the given mode.
func helperCmd(t *testing.T, mode, root string, extra map[string]string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=TestHelperProcess", "-test.timeout=120s")
	cmd.Env = append(os.Environ(), envHelper+"="+mode, envRoot+"="+root)
	for k, v := range extra {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return cmd
}

// runHelper runs a child to completion and returns its exit code and output.
func runHelper(t *testing.T, mode, root string, extra map[string]string) (int, string) {
	t.Helper()
	cmd := helperCmd(t, mode, root, extra)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("run helper %s: %v\n%s", mode, err, out)
	}
	return ee.ExitCode(), string(out)
}

// newTestStore prepares a minimal initialized project and returns a store plus
// its root. The layout mirrors what devsys init creates, minus the parts the
// storage layer does not touch.
func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"state", "events", "local"} {
		if err := os.MkdirAll(filepath.Join(root, DevsysDirName, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return s, root
}

// seedDevsysFile writes a managed file directly, the way a previous devsys
// version or a hand edit would have left it.
func seedDevsysFile(t *testing.T, root, rel string, data []byte) string {
	t.Helper()
	abs := filepath.Join(root, DevsysDirName, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return abs
}

func devsysAbs(root, rel string) string {
	return filepath.Join(root, DevsysDirName, filepath.FromSlash(rel))
}

func readDevsysFile(t *testing.T, root, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(devsysAbs(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// countJSONL returns the number of complete lines in a JSONL file.
func countJSONL(t *testing.T, path string) int {
	t.Helper()
	n := 0
	if err := ScanJSONL(path, func([]byte) error { n++; return nil }); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return n
}

// txnDirs lists the pending transaction directories of a project.
func txnDirs(t *testing.T, root string) []string {
	t.Helper()
	s := &Store{txnDir: filepath.Join(root, DevsysDirName, "local", txnDirName)}
	names, err := s.pendingTxns()
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// exitCodeOf extracts the exit code of a finished child process.
func exitCodeOf(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// waitForLockHolder blocks until the exclusive lock has been taken by someone
// else (observable through the best-effort holder description) or fails the
// test.
func waitForLockHolder(t *testing.T, root string) {
	t.Helper()
	lockPath := filepath.Join(root, DevsysDirName, "local", lockFileName)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(lockPath); err == nil && strings.Contains(string(data), "pid:") {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("no lock holder appeared")
}
