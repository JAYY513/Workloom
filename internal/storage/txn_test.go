package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestTransactionAppliesEveryOpAndCleansUp(t *testing.T) {
	s, root := newTestStore(t)
	seedDevsysFile(t, root, stateRel, []byte(seedStateV1))
	seedDevsysFile(t, root, eventsRel, []byte(seedEvent))

	err := s.Write(context.Background(), func(tx *Tx) error {
		if err := tx.Put(stateRel, []byte(newStateV2), ExpectHash(HashBytes([]byte(seedStateV1)))); err != nil {
			return err
		}
		return tx.AppendJSONL(eventsRel, map[string]string{"event": "created", "id": "evt-1"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(readDevsysFile(t, root, stateRel)); got != newStateV2 {
		t.Errorf("state = %q, want %q", got, newStateV2)
	}
	if got := string(readDevsysFile(t, root, eventsRel)); got != seedEvent+newEvent {
		t.Errorf("events = %q, want %q", got, seedEvent+newEvent)
	}
	if dirs := txnDirs(t, root); len(dirs) != 0 {
		t.Errorf("transaction directories left behind: %v", dirs)
	}
	rep, err := s.Recover(context.Background())
	if err != nil || !rep.Empty() {
		t.Errorf("recover on a clean project = %+v, %v", rep, err)
	}
}

// TestAppendJSONLRawStagesOneBatch appends several records sharing one shard
// in a single staged append and extends the file in a later transaction.
func TestAppendJSONLRawStagesOneBatch(t *testing.T) {
	s, root := newTestStore(t)
	payload := "{\"event\":\"a\"}\n{\"event\":\"b\"}\n"
	if err := s.Write(context.Background(), func(tx *Tx) error {
		return tx.AppendJSONLRaw(eventsRel, []byte(payload))
	}); err != nil {
		t.Fatal(err)
	}
	if got := string(readDevsysFile(t, root, eventsRel)); got != payload {
		t.Errorf("events = %q, want %q", got, payload)
	}
	if err := s.Write(context.Background(), func(tx *Tx) error {
		return tx.AppendJSONLRaw(eventsRel, []byte("{\"event\":\"c\"}\n"))
	}); err != nil {
		t.Fatal(err)
	}
	if got := string(readDevsysFile(t, root, eventsRel)); got != payload+"{\"event\":\"c\"}\n" {
		t.Errorf("events after second append = %q", got)
	}
}

// TestAppendJSONLRawRejectsMalformedPayload locks the payload contract:
// complete LF-terminated JSON lines only.
func TestAppendJSONLRawRejectsMalformedPayload(t *testing.T) {
	s, root := newTestStore(t)
	cases := []struct{ name, payload string }{
		{"empty", ""},
		{"missing terminator", `{"event":"a"}`},
		{"empty line", "{\"event\":\"a\"}\n\n"},
		{"leading blank line", "\n{\"event\":\"a\"}\n"},
		{"carriage return", "{\"event\":\"a\"}\r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.Write(context.Background(), func(tx *Tx) error {
				return tx.AppendJSONLRaw(eventsRel, []byte(tc.payload))
			})
			if err == nil {
				t.Fatalf("payload %q accepted", tc.payload)
			}
		})
	}
	if _, err := os.Stat(devsysAbs(root, eventsRel)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("rejected payloads touched the file: %v", err)
	}
}

// TestCrashRecoveryMatrix interrupts a two-op transaction at every durable
// stage and checks that reopening the project recovers to one complete state
// and that replaying again changes nothing.
func TestCrashRecoveryMatrix(t *testing.T) {
	stages := []string{
		stageJournalDurable,
		stageCommitted,
		stageApplied(0),
		stageApplied(1),
		stageCleaned,
	}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			_, root := newTestStore(t)
			seedDevsysFile(t, root, stateRel, []byte(seedStateV1))
			seedDevsysFile(t, root, eventsRel, []byte(seedEvent))

			errFile := filepath.Join(t.TempDir(), "helper-err.txt")
			code, out := runHelper(t, "crash", root, map[string]string{envCrashAt: stage, envErrFile: errFile})
			if code != exitCodeCrash {
				detail, _ := os.ReadFile(errFile)
				t.Fatalf("helper exit = %d, want %d (helper error: %s)\n%s", code, exitCodeCrash, detail, out)
			}

			s, err := Open(root, Options{})
			if err != nil {
				t.Fatal(err)
			}
			rep, err := s.Recover(context.Background())
			if err != nil {
				t.Fatalf("recover: %v", err)
			}
			if dirs := txnDirs(t, root); len(dirs) != 0 {
				t.Fatalf("recovery left transaction directories behind: %v", dirs)
			}

			wantState, wantEvents := seedStateV1, seedEvent
			uncommitted := stage == stageJournalDurable
			if !uncommitted {
				wantState, wantEvents = newStateV2, seedEvent+newEvent
			}
			switch {
			case uncommitted && rep.Discarded != 1:
				t.Errorf("report = %+v, want one discarded transaction", rep)
			case !uncommitted && rep.Discarded != 0:
				t.Errorf("report = %+v, want no discarded transaction", rep)
			}
			if got := string(readDevsysFile(t, root, stateRel)); got != wantState {
				t.Errorf("state = %q, want %q", got, wantState)
			}
			if got := string(readDevsysFile(t, root, eventsRel)); got != wantEvents {
				t.Errorf("events = %q, want %q", got, wantEvents)
			}

			// A second pass must be a no-op: replay stays idempotent and never
			// duplicates the appended event.
			rep2, err := s.Recover(context.Background())
			if err != nil {
				t.Fatalf("second recover: %v", err)
			}
			if !rep2.Empty() {
				t.Errorf("second recover = %+v, want nothing to do", rep2)
			}
			if got := string(readDevsysFile(t, root, eventsRel)); got != wantEvents {
				t.Errorf("events after second recover = %q, want %q", got, wantEvents)
			}
			if got := string(readDevsysFile(t, root, stateRel)); got != wantState {
				t.Errorf("state after second recover = %q, want %q", got, wantState)
			}
		})
	}
}

func TestRecoveryFailsOnMissingPayload(t *testing.T) {
	_, root := newTestStore(t)
	seedDevsysFile(t, root, stateRel, []byte(seedStateV1))
	seedDevsysFile(t, root, eventsRel, []byte(seedEvent))
	runHelper(t, "crash", root, map[string]string{envCrashAt: stageCommitted, envErrFile: filepath.Join(t.TempDir(), "e.txt")})

	dirs := txnDirs(t, root)
	if len(dirs) != 1 {
		t.Fatalf("transaction directories = %v, want 1", dirs)
	}
	payloadDir := filepath.Join(root, DevsysDirName, "local", txnDirName, dirs[0], payloadDirName)
	if err := os.RemoveAll(payloadDir); err != nil {
		t.Fatal(err)
	}

	s, err := Open(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recover(context.Background()); !errors.Is(err, ErrRecoveryFailed) {
		t.Fatalf("recover error = %v, want ErrRecoveryFailed", err)
	}
	if err := s.Read(context.Background(), func(r *Reader) error { return nil }); !errors.Is(err, ErrRecoveryFailed) {
		t.Errorf("read error = %v, want ErrRecoveryFailed", err)
	}
	if err := s.Write(context.Background(), func(tx *Tx) error {
		return tx.Put(stateRel, []byte("x"), ExpectAny())
	}); !errors.Is(err, ErrRecoveryFailed) {
		t.Errorf("write error = %v, want ErrRecoveryFailed", err)
	}

	d, err := s.Diagnose()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Pending) != 1 {
		t.Fatalf("diagnostics = %+v, want one pending transaction", d)
	}
	if d.Pending[0].State != "unreadable" || !strings.Contains(d.Pending[0].Detail, "payload") {
		t.Errorf("diagnostics do not name the damaged payload: %+v", d.Pending[0])
	}
}

func TestRecoveryFailsWhenTargetChangedExternally(t *testing.T) {
	_, root := newTestStore(t)
	seedDevsysFile(t, root, stateRel, []byte(seedStateV1))
	seedDevsysFile(t, root, eventsRel, []byte(seedEvent))
	runHelper(t, "crash", root, map[string]string{envCrashAt: stageCommitted, envErrFile: filepath.Join(t.TempDir(), "e.txt")})

	seedDevsysFile(t, root, stateRel, []byte("version 3, changed outside devsys\n"))

	s, err := Open(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recover(context.Background()); !errors.Is(err, ErrRecoveryFailed) {
		t.Fatalf("recover error = %v, want ErrRecoveryFailed", err)
	}
	d, err := s.Diagnose()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Pending) != 1 || d.Pending[0].State != "committed" {
		t.Fatalf("diagnostics = %+v", d.Pending)
	}
	ops := d.Pending[0].Ops
	if len(ops) != 2 || ops[0].Rel != stateRel || ops[0].Status != "conflict" {
		t.Errorf("op diagnostics = %+v, want a conflict on %s", ops, stateRel)
	}
}

func TestUncommittedTransactionIsNotDiscardedOverForeignChange(t *testing.T) {
	_, root := newTestStore(t)
	seedDevsysFile(t, root, stateRel, []byte(seedStateV1))
	seedDevsysFile(t, root, eventsRel, []byte(seedEvent))
	runHelper(t, "crash", root, map[string]string{envCrashAt: stageJournalDurable, envErrFile: filepath.Join(t.TempDir(), "e.txt")})

	seedDevsysFile(t, root, stateRel, []byte("version 3, changed outside devsys\n"))

	s, err := Open(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recover(context.Background()); !errors.Is(err, ErrRecoveryFailed) {
		t.Fatalf("recover error = %v, want ErrRecoveryFailed", err)
	}
	if dirs := txnDirs(t, root); len(dirs) != 1 {
		t.Errorf("transaction material was dropped despite the mismatch: %v", dirs)
	}
	d, err := s.Diagnose()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Pending) != 1 || d.Pending[0].State != "uncommitted" {
		t.Fatalf("diagnostics = %+v", d.Pending)
	}
	if ops := d.Pending[0].Ops; len(ops) != 2 || ops[0].Status != "conflict" {
		t.Errorf("op diagnostics = %+v, want a conflict on %s", ops, stateRel)
	}
}

func TestReadRecoversPendingTransactionFirst(t *testing.T) {
	_, root := newTestStore(t)
	seedDevsysFile(t, root, stateRel, []byte(seedStateV1))
	seedDevsysFile(t, root, eventsRel, []byte(seedEvent))
	runHelper(t, "crash", root, map[string]string{envCrashAt: stageApplied(0), envErrFile: filepath.Join(t.TempDir(), "e.txt")})

	s, err := Open(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var state []byte
	err = s.Read(context.Background(), func(r *Reader) error {
		data, exists, err := r.Read(stateRel)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("state file missing")
		}
		state = data
		return nil
	})
	if err != nil {
		t.Fatalf("read with pending transaction: %v", err)
	}
	if string(state) != newStateV2 {
		t.Errorf("state = %q, want %q", state, newStateV2)
	}
	if got := string(readDevsysFile(t, root, eventsRel)); got != seedEvent+newEvent {
		t.Errorf("events = %q, want %q", got, seedEvent+newEvent)
	}
	if dirs := txnDirs(t, root); len(dirs) != 0 {
		t.Errorf("transaction directories left behind: %v", dirs)
	}
}

func TestWriteAbortLeavesNoTrace(t *testing.T) {
	s, root := newTestStore(t)
	boom := errors.New("aborted by the caller")
	err := s.Write(context.Background(), func(tx *Tx) error {
		if err := tx.Put(stateRel, []byte(newStateV2), ExpectAny()); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("write error = %v, want %v", err, boom)
	}
	if _, err := os.Stat(devsysAbs(root, stateRel)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("aborted transaction created %s", stateRel)
	}
	if dirs := txnDirs(t, root); len(dirs) != 0 {
		t.Errorf("transaction directories = %v, want none", dirs)
	}
}

func TestEmptyTransactionIsANoOp(t *testing.T) {
	s, root := newTestStore(t)
	if err := s.Write(context.Background(), func(tx *Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if dirs := txnDirs(t, root); len(dirs) != 0 {
		t.Errorf("transaction directories = %v, want none", dirs)
	}
}

func TestCASConflictWithinOneProcess(t *testing.T) {
	s1, root := newTestStore(t)
	s2, err := Open(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	seedDevsysFile(t, root, stateRel, []byte(seedStateV1))
	expect := ExpectHash(HashBytes([]byte(seedStateV1)))

	stores := []*Store{s1, s2}
	errs := make([]error, len(stores))
	var wg sync.WaitGroup
	for i, s := range stores {
		wg.Add(1)
		go func(i int, s *Store) {
			defer wg.Done()
			errs[i] = s.Write(context.Background(), func(tx *Tx) error {
				return tx.Put(stateRel, []byte(fmt.Sprintf("writer %d\n", i)), expect)
			})
		}(i, s)
	}
	wg.Wait()

	ok, conflicts := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatalf("successes = %d, conflicts = %d, want 1 and 1", ok, conflicts)
	}
}

func TestCASConflictAcrossProcesses(t *testing.T) {
	_, root := newTestStore(t)
	seedDevsysFile(t, root, stateRel, []byte(seedStateV1))

	start := filepath.Join(t.TempDir(), "start")
	const workers = 4
	cmds := make([]*exec.Cmd, 0, workers)
	for i := range workers {
		cmd := helperCmd(t, "racer", root, map[string]string{
			envStart:   start,
			envExpect:  ExpectHash(HashBytes([]byte(seedStateV1))).String(),
			envRacerID: strconv.Itoa(i),
			envErrFile: filepath.Join(t.TempDir(), "err.txt"),
		})
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
	}
	if err := os.WriteFile(start, []byte("go\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ok, conflicts := 0, 0
	for i, cmd := range cmds {
		switch err := cmd.Wait(); {
		case err == nil:
			ok++
		case exitCodeOf(err) == exitCodeConflict:
			conflicts++
		default:
			t.Fatalf("racer %d failed: %v", i, err)
		}
	}
	if ok != 1 || conflicts != workers-1 {
		t.Fatalf("winners = %d, conflicts = %d, want 1 and %d", ok, conflicts, workers-1)
	}
	if got := string(readDevsysFile(t, root, stateRel)); !strings.HasPrefix(got, "winner ") {
		t.Errorf("state = %q, want the winner's content", got)
	}
	if dirs := txnDirs(t, root); len(dirs) != 0 {
		t.Errorf("transaction directories left behind: %v", dirs)
	}
}

func TestStagingTheSameFileTwiceIsRejected(t *testing.T) {
	s, root := newTestStore(t)
	err := s.Write(context.Background(), func(tx *Tx) error {
		if err := tx.Put(stateRel, []byte("a\n"), ExpectAny()); err != nil {
			return err
		}
		return tx.Put(stateRel, []byte("b\n"), ExpectAny())
	})
	if err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("error = %v, want a duplicate-target error", err)
	}
	if _, statErr := os.Stat(devsysAbs(root, stateRel)); !errors.Is(statErr, fs.ErrNotExist) {
		t.Error("rejected transaction must not create the target")
	}
}
