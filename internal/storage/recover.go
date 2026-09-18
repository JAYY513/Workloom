package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RecoveryReport summarizes one recovery pass.
type RecoveryReport struct {
	// Discarded counts transactions that never reached the commit point and
	// were removed (their targets were verified untouched).
	Discarded int
	// Replayed counts committed transactions whose changes were re-applied.
	Replayed int
	// Skipped counts committed operations already present on disk (the
	// idempotent part of the replay).
	Skipped int
}

// Empty reports that nothing needed recovery.
func (r RecoveryReport) Empty() bool {
	return r.Discarded == 0 && r.Replayed == 0 && r.Skipped == 0
}

// Recover performs the deterministic recovery of interrupted transactions
// under the exclusive project lock (方案 §15.2: 中断后先按日志完成确定性恢复).
func (s *Store) Recover(ctx context.Context) (RecoveryReport, error) {
	lock, err := acquireLock(ctx, s.lockPath, lockExclusive, s.opts.LockTimeout, s.opts.Now)
	if err != nil {
		return RecoveryReport{}, err
	}
	defer lock.release()
	return s.recoverLocked()
}

// pendingTxns lists transaction directories in deterministic (time) order.
func (s *Store) pendingTxns() ([]string, error) {
	entries, err := os.ReadDir(s.txnDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan %s: %w", s.txnDir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// recoverLocked recovers every pending transaction. The first failure stops the
// pass and is reported as ErrRecoveryFailed; recovery never guesses.
func (s *Store) recoverLocked() (RecoveryReport, error) {
	names, err := s.pendingTxns()
	if err != nil {
		return RecoveryReport{}, err
	}
	var rep RecoveryReport
	for _, id := range names {
		dir := filepath.Join(s.txnDir, id)
		replayed, skipped, discarded, err := s.recoverTxn(dir, id)
		if err != nil {
			return rep, fmt.Errorf(
				"%w: %v; unfinished transaction logs are recovery material, not cache (方案 §15.2) — resolve before reading or writing valid state",
				ErrRecoveryFailed, err)
		}
		if discarded {
			rep.Discarded++
		}
		rep.Replayed += replayed
		rep.Skipped += skipped
	}
	return rep, nil
}

// recoverTxn handles one transaction directory: uncommitted transactions are
// verified untouched and discarded; committed ones are replayed idempotently.
func (s *Store) recoverTxn(dir, id string) (replayed, skipped int, discarded bool, err error) {
	journalBytes, err := os.ReadFile(filepath.Join(dir, journalFileName))
	if err != nil {
		return 0, 0, false, fmt.Errorf("txn %s: read journal: %v", id, err)
	}
	var j journal
	if err := DecodeYAML(journalBytes, &j); err != nil {
		return 0, 0, false, fmt.Errorf("txn %s: parse journal: %v", id, err)
	}
	if j.TxnID != id {
		return 0, 0, false, fmt.Errorf("txn %s: journal names %q", id, j.TxnID)
	}
	if err := j.validate(); err != nil {
		return 0, 0, false, err
	}

	markerBytes, err := os.ReadFile(filepath.Join(dir, commitFileName))
	if errors.Is(err, fs.ErrNotExist) {
		// No commit point: nothing was applied yet. Verify that while
		// interrupted, no target changed underneath us, then discard.
		if err := s.verifyUntouched(&j); err != nil {
			return 0, 0, false, err
		}
		if err := removeTxnDir(dir); err != nil {
			return 0, 0, false, err
		}
		return 0, 0, true, nil
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("txn %s: read commit marker: %v", id, err)
	}
	var m commitMarker
	if err := DecodeYAML(markerBytes, &m); err != nil {
		return 0, 0, false, fmt.Errorf("txn %s: parse commit marker: %v", id, err)
	}
	if m.TxnID != id {
		return 0, 0, false, fmt.Errorf("txn %s: commit marker names %q", id, m.TxnID)
	}
	if m.JournalSHA256 != sha256Hex(journalBytes) {
		return 0, 0, false, fmt.Errorf("txn %s: journal changed after commit (sha256 mismatch)", id)
	}
	if m.OpCount != len(j.Ops) {
		return 0, 0, false, fmt.Errorf("txn %s: commit marker counts %d ops, journal has %d", id, m.OpCount, len(j.Ops))
	}
	replayed, skipped, err = s.replayTxn(dir, &j)
	if err != nil {
		return replayed, skipped, false, fmt.Errorf("txn %s: %v", id, err)
	}
	if err := removeTxnDir(dir); err != nil {
		return replayed, skipped, false, err
	}
	return replayed, skipped, false, nil
}

// verifyUntouched checks that an uncommitted transaction would still be safe to
// discard: every Put target must match its guard and every JSONL target must
// have exactly its recorded size. A mismatch means someone changed state
// outside devsys and a human must look before anything is discarded.
func (s *Store) verifyUntouched(j *journal) error {
	for i := range j.Ops {
		o := &j.Ops[i]
		abs, err := s.resolve(o.Rel)
		if err != nil {
			return fmt.Errorf("op %d: %v", i, err)
		}
		switch o.Kind {
		case opKindPut:
			expect, err := parseExpect(o.Expect)
			if err != nil {
				return fmt.Errorf("op %d: %v", i, err)
			}
			cur, exists, err := readFileMaybe(abs)
			if err != nil {
				return fmt.Errorf("op %d (%s): %v", i, o.Rel, err)
			}
			if !expect.satisfiedBy(cur, exists) {
				return fmt.Errorf("op %d (%s): target changed outside devsys while the transaction was interrupted (expected %s, found %s)",
					i, o.Rel, expect, describeCurrent(cur, exists))
			}
		case opKindAppend:
			tail, err := InspectJSONL(abs)
			if err != nil {
				return fmt.Errorf("op %d (%s): %v", i, o.Rel, err)
			}
			if tail.Size != o.PreSize {
				return fmt.Errorf("op %d (%s): file is %d bytes, expected %d", i, o.Rel, tail.Size, o.PreSize)
			}
		case opKindDelete:
			expect, err := parseExpect(o.Expect)
			if err != nil {
				return fmt.Errorf("op %d: %v", i, err)
			}
			cur, exists, err := readFileMaybe(abs)
			if err != nil {
				return fmt.Errorf("op %d (%s): %v", i, o.Rel, err)
			}
			if !expect.satisfiedBy(cur, exists) {
				return fmt.Errorf("op %d (%s): target changed outside devsys while the transaction was interrupted (expected %s, found %s)",
					i, o.Rel, expect, describeCurrent(cur, exists))
			}
		}
	}
	return nil
}

func (s *Store) replayTxn(dir string, j *journal) (applied, skipped int, err error) {
	for i := range j.Ops {
		o := &j.Ops[i]
		abs, err := s.resolve(o.Rel)
		if err != nil {
			return applied, skipped, fmt.Errorf("op %d: %v", i, err)
		}
		switch o.Kind {
		case opKindPut:
			payload, err := readTxnPayload(dir, o)
			if err != nil {
				return applied, skipped, fmt.Errorf("op %d (%s): %v", i, o.Rel, err)
			}
			expect, err := parseExpect(o.Expect)
			if err != nil {
				return applied, skipped, fmt.Errorf("op %d: %v", i, err)
			}
			cur, exists, err := readFileMaybe(abs)
			if err != nil {
				return applied, skipped, fmt.Errorf("op %d (%s): %v", i, o.Rel, err)
			}
			if exists && sha256Hex(cur) == o.PayloadSHA256 {
				skipped++
				continue
			}
			if !expect.satisfiedBy(cur, exists) {
				return applied, skipped, fmt.Errorf(
					"op %d (%s): target is neither the expected old version (%s, found %s) nor the committed new content; refusing to guess",
					i, o.Rel, expect, describeCurrent(cur, exists))
			}
			if err := AtomicWrite(abs, payload, 0o644); err != nil {
				return applied, skipped, err
			}
			applied++
		case opKindAppend:
			payload, err := readTxnPayload(dir, o)
			if err != nil {
				return applied, skipped, fmt.Errorf("op %d (%s): %v", i, o.Rel, err)
			}
			tail, err := InspectJSONL(abs)
			if err != nil {
				return applied, skipped, fmt.Errorf("op %d (%s): %v", i, o.Rel, err)
			}
			switch {
			case tail.Size == o.PreSize:
				if !tail.Complete {
					return applied, skipped, fmt.Errorf("%w: op %d (%s): %d bytes without line terminator", ErrIncompleteTail, i, o.Rel, tail.Size)
				}
				if err := appendJSONL(abs, payload, o.PreSize); err != nil {
					return applied, skipped, err
				}
				applied++
			case tail.Size >= o.PreSize+o.PayloadSize:
				region, err := readRegion(abs, o.PreSize, o.PayloadSize)
				if err != nil {
					return applied, skipped, fmt.Errorf("op %d (%s): %v", i, o.Rel, err)
				}
				if !bytes.Equal(region, payload) {
					return applied, skipped, fmt.Errorf("op %d (%s): the appended region does not match the committed payload", i, o.Rel)
				}
				skipped++
			default:
				return applied, skipped, fmt.Errorf("op %d (%s): file is %d bytes, expected %d or at least %d",
					i, o.Rel, tail.Size, o.PreSize, o.PreSize+o.PayloadSize)
			}
		case opKindDelete:
			expect, err := parseExpect(o.Expect)
			if err != nil {
				return applied, skipped, fmt.Errorf("op %d: %v", i, err)
			}
			cur, exists, err := readFileMaybe(abs)
			if err != nil {
				return applied, skipped, fmt.Errorf("op %d (%s): %v", i, o.Rel, err)
			}
			if !exists {
				skipped++
				continue
			}
			if !expect.satisfiedBy(cur, exists) {
				return applied, skipped, fmt.Errorf(
					"op %d (%s): target is neither the expected old version (%s, found %s) nor absent; refusing to guess",
					i, o.Rel, expect, describeCurrent(cur, exists))
			}
			if err := os.Remove(abs); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return applied, skipped, fmt.Errorf("op %d (%s): delete: %v", i, o.Rel, err)
			}
			applied++
		}
	}
	return applied, skipped, nil
}

// readTxnPayload loads and verifies one payload of a committed transaction.
func readTxnPayload(dir string, o *journalOp) ([]byte, error) {
	rel, err := payloadRel(o.Payload)
	if err != nil {
		return nil, err
	}
	payload, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return nil, fmt.Errorf("read payload: %v", err)
	}
	if sha256Hex(payload) != o.PayloadSHA256 {
		return nil, errors.New("payload changed since commit (sha256 mismatch)")
	}
	return payload, nil
}

// readRegion returns size bytes at offset of path.
func readRegion(path string, offset, size int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if offset < 0 || size < 0 || offset+size > st.Size() {
		return nil, fmt.Errorf("region [%d,%d) outside %s (%d bytes)", offset, offset+size, path, st.Size())
	}
	buf := make([]byte, size)
	if _, err := f.ReadAt(buf, offset); err != nil {
		return nil, err
	}
	return buf, nil
}

func removeTxnDir(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clean up transaction directory %s: %w", dir, err)
	}
	_ = fsyncDir(filepath.Dir(dir))
	return nil
}

// Diagnostics is the read-only recovery view. It never fails on damaged
// transaction material: describing the damage is its purpose.
type Diagnostics struct {
	// TxnDir is the transaction material directory.
	TxnDir string
	// Lock is the best-effort holder description of the last exclusive holder.
	Lock string
	// Pending lists transaction directories still on disk.
	Pending []PendingTxn
}

// PendingTxn describes one transaction directory found by Diagnose.
type PendingTxn struct {
	ID string
	// State is "uncommitted" (discardable), "committed" (replayable) or
	// "unreadable" (needs a human).
	State string
	// Detail explains what recovery would do, or what is wrong.
	Detail string
	// Ops describes, per staged operation, what recovery would find on disk.
	Ops []PendingOp
}

// PendingOp is the on-disk status of one staged operation.
type PendingOp struct {
	Rel string
	// Status is "applied" (the new content is already in place), "pending"
	// (recovery would apply it), "conflict" (the target matches neither the
	// expected old nor the committed new content) or "unknown".
	Status string
	Detail string
}

// Diagnose reports the pending transaction material and lock state without
// changing anything. It does not take the project lock: its output is
// informational, not a consistency claim.
func (s *Store) Diagnose() (Diagnostics, error) {
	d := Diagnostics{TxnDir: s.txnDir}
	if data, err := os.ReadFile(s.lockPath); err == nil && len(data) > 0 {
		d.Lock = strings.Join(strings.Fields(string(data)), " ")
	}
	names, err := s.pendingTxns()
	if err != nil {
		return d, err
	}
	for _, id := range names {
		dir := filepath.Join(s.txnDir, id)
		d.Pending = append(d.Pending, s.diagnoseTxn(dir, id))
	}
	return d, nil
}

func (s *Store) diagnoseTxn(dir, id string) PendingTxn {
	pt := PendingTxn{ID: id}
	unreadable := func(format string, a ...any) PendingTxn {
		pt.State = "unreadable"
		pt.Detail = fmt.Sprintf(format, a...)
		return pt
	}

	journalBytes, err := os.ReadFile(filepath.Join(dir, journalFileName))
	if err != nil {
		return unreadable("read journal: %v", err)
	}
	var j journal
	if err := DecodeYAML(journalBytes, &j); err != nil {
		return unreadable("parse journal: %v", err)
	}
	if err := j.validate(); err != nil {
		return unreadable("%v", err)
	}

	markerBytes, err := os.ReadFile(filepath.Join(dir, commitFileName))
	if errors.Is(err, fs.ErrNotExist) {
		pt.State = "uncommitted"
		pt.Detail = "never reached the commit point; recovery verifies the targets and discards it"
		pt.Ops = s.opStatuses(&j)
		return pt
	}
	if err != nil {
		return unreadable("read commit marker: %v", err)
	}
	var m commitMarker
	if err := DecodeYAML(markerBytes, &m); err != nil {
		return unreadable("parse commit marker: %v", err)
	}
	if m.TxnID != id {
		return unreadable("commit marker names %q", m.TxnID)
	}
	if m.JournalSHA256 != sha256Hex(journalBytes) {
		return unreadable("journal changed after commit (sha256 mismatch)")
	}
	if m.OpCount != len(j.Ops) {
		return unreadable("commit marker counts %d ops, journal has %d", m.OpCount, len(j.Ops))
	}
	for i := range j.Ops {
		if j.Ops[i].Kind == opKindDelete {
			continue
		}
		if _, err := readTxnPayload(dir, &j.Ops[i]); err != nil {
			return unreadable("op %d (%s): %v", i, j.Ops[i].Rel, err)
		}
	}
	pt.State = "committed"
	pt.Detail = fmt.Sprintf("committed with %d op(s); recovery replays them idempotently", len(j.Ops))
	pt.Ops = s.opStatuses(&j)
	return pt
}

// opStatuses classifies each staged op against the current disk state.
func (s *Store) opStatuses(j *journal) []PendingOp {
	out := make([]PendingOp, 0, len(j.Ops))
	for i := range j.Ops {
		o := &j.Ops[i]
		po := PendingOp{Rel: o.Rel}
		abs, err := s.resolve(o.Rel)
		if err != nil {
			po.Status, po.Detail = "unknown", err.Error()
			out = append(out, po)
			continue
		}
		switch o.Kind {
		case opKindPut:
			expect, err := parseExpect(o.Expect)
			if err != nil {
				po.Status, po.Detail = "unknown", err.Error()
				break
			}
			cur, exists, err := readFileMaybe(abs)
			if err != nil {
				po.Status, po.Detail = "unknown", err.Error()
				break
			}
			switch {
			case exists && sha256Hex(cur) == o.PayloadSHA256:
				po.Status = "applied"
			case expect.satisfiedBy(cur, exists):
				po.Status = "pending"
			default:
				po.Status = "conflict"
				po.Detail = fmt.Sprintf("found %s, expected %s or the committed content", describeCurrent(cur, exists), expect)
			}
		case opKindAppend:
			tail, err := InspectJSONL(abs)
			if err != nil {
				po.Status, po.Detail = "unknown", err.Error()
				break
			}
			switch {
			case tail.Size == o.PreSize:
				po.Status = "pending"
			case tail.Size >= o.PreSize+o.PayloadSize:
				region, err := readRegion(abs, o.PreSize, o.PayloadSize)
				if err != nil || sha256Hex(region) != o.PayloadSHA256 {
					po.Status = "conflict"
					po.Detail = "the appended region does not match the committed payload"
					break
				}
				po.Status = "applied"
			default:
				po.Status = "conflict"
				po.Detail = fmt.Sprintf("file is %d bytes, expected %d or at least %d", tail.Size, o.PreSize, o.PreSize+o.PayloadSize)
			}
		case opKindDelete:
			expect, err := parseExpect(o.Expect)
			if err != nil {
				po.Status, po.Detail = "unknown", err.Error()
				break
			}
			cur, exists, err := readFileMaybe(abs)
			if err != nil {
				po.Status, po.Detail = "unknown", err.Error()
				break
			}
			switch {
			case !exists:
				po.Status = "applied"
			case expect.satisfiedBy(cur, exists):
				po.Status = "pending"
			default:
				po.Status = "conflict"
				po.Detail = fmt.Sprintf("found %s, expected %s or absent", describeCurrent(cur, exists), expect)
			}
		}
		out = append(out, po)
	}
	return out
}
