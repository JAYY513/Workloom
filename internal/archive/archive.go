// Package archive implements M8.3 (方案 §14.2): conservative archiving of
// the append-only JSONL streams — project events and run streams — to keep
// the working tree small without losing history.
//
// Archive layout (all committed, all ordinary files):
//
//	.devsys/archive/events/<YYYY-MM>.jsonl   archived event shards
//	.devsys/archive/runs/<run-id>.jsonl      archived run streams
//	.devsys/archive/manifest.yaml            what moved, when, by whom
//
// Reads merge live and archived state: events.Read covers both shard trees,
// readRunStream-equivalents fall back to the archived stream when the live
// one is gone. Search excludes archive/ (current-state retrieval only).
// There is deliberately no delete/purge shape: dropping archived history is
// a manual git operation (默认保守：不删，只归档).
package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"workloom/internal/domain"
	"workloom/internal/run"
	"workloom/internal/storage"
)

const schemaVersion = 1

// Managed rels, slash-separated relative to .devsys.
const (
	manifestRel  = "archive/manifest.yaml"
	eventsPrefix = "archive/events/"
	runsPrefix   = "archive/runs/"
	liveEvents   = "events/"
	liveRuns     = "runs/"
)

// Entry records one archived file: the pointer that keeps history queryable.
type Entry struct {
	Kind       string `json:"kind" yaml:"kind"` // "events" | "run_stream"
	Source     string `json:"source" yaml:"source"`
	SHA256     string `json:"sha256" yaml:"sha256"`
	Bytes      int64  `json:"bytes" yaml:"bytes"`
	Lines      int    `json:"lines" yaml:"lines"`
	ArchivedAt string `json:"archived_at" yaml:"archived_at"`
	ArchivedBy string `json:"archived_by" yaml:"archived_by"`
	Reason     string `json:"reason" yaml:"reason"`
}

// Manifest is the archive catalogue.
type Manifest struct {
	SchemaVersion int     `json:"schema_version" yaml:"schema_version"`
	Entries       []Entry `json:"entries" yaml:"entries"`
}

// Report is what an archive run moved.
type Report struct {
	Archived      []string `json:"archived"`
	Skipped       []string `json:"skipped,omitempty"`
	BytesArchived int64    `json:"bytes_archived"`
	BytesBefore   int64    `json:"bytes_before"`
	BytesAfter    int64    `json:"bytes_after"`
	Manifest      Manifest `json:"manifest"`
	DryRun        bool     `json:"dry_run,omitempty"`
	Note          string   `json:"note,omitempty"`
}

// Spec selects what to archive.
type Spec struct {
	// EventMonths lists live event shards (YYYY-MM) older than BeforeMonth.
	BeforeMonth string
	// RunIDs lists run streams to archive (each must be terminal).
	RunIDs []string
	Actor  string
	Reason string
	Now    time.Time
	DryRun bool
}

var monthPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}$`)

// liveSize sums the bytes of the live JSONL trees (events + run streams).
func liveSize(devsys string) int64 {
	var total int64
	for _, dir := range []string{"events", "runs"} {
		entries, err := os.ReadDir(filepath.Join(devsys, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			if st, err := e.Info(); err == nil {
				total += st.Size()
			}
		}
	}
	return total
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// PlanEvents lists the live event shards strictly older than beforeMonth
// (YYYY-MM, zero-padded so lexical order is chronological).
func PlanEvents(root, beforeMonth string) ([]string, error) {
	if !monthPattern.MatchString(beforeMonth) {
		return nil, fmt.Errorf("archive: --before must be YYYY-MM (got %q)", beforeMonth)
	}
	if _, err := storage.Open(root, storage.Options{}); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(root, ".devsys", liveEvents))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		month := strings.TrimSuffix(e.Name(), ".jsonl")
		if !monthPattern.MatchString(month) || month >= beforeMonth {
			continue
		}
		out = append(out, liveEvents+month+".jsonl")
	}
	sort.Strings(out)
	return out, nil
}

// PlanRuns validates run IDs and requires every referenced run to be
// terminal: archiving a running stream would break round bookkeeping
// (nextRound/lastExit) and the dispatch stall sweep.
func PlanRuns(root string, ids []string) ([]string, error) {
	st, err := storage.Open(root, storage.Options{})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	ctx := context.Background()
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !run.ValidID(id) {
			return nil, fmt.Errorf("archive: invalid run id %q", id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		var rec domain.Run
		err := st.Read(ctx, func(r *storage.Reader) error {
			data, exists, rerr := r.Read(liveRuns + id + ".yaml")
			if rerr != nil {
				return rerr
			}
			if !exists {
				return fmt.Errorf("archive: run %s not found", id)
			}
			return domain.DecodeYAML(data, &rec)
		})
		if err != nil {
			return nil, err
		}
		if !isTerminal(rec.Status) {
			return nil, fmt.Errorf("archive: run %s is %q, only terminal runs may be archived", id, rec.Status)
		}
		out = append(out, liveRuns+id+".jsonl")
	}
	sort.Strings(out)
	return out, nil
}

func isTerminal(status string) bool {
	switch status {
	case "succeeded", "failed", "timed_out", "stalled", "canceled":
		return true
	default:
		return false
	}
}

// Apply moves every source in spec into archive/ inside ONE storage
// transaction (read-copy-delete per file plus a single manifest rewrite),
// so an interruption replays to exactly one outcome. Manifest-only writes
// never happen: an empty plan is a no-op report.
func Apply(ctx context.Context, root string, spec Spec) (Report, error) {
	rep := Report{DryRun: spec.DryRun}
	if strings.TrimSpace(spec.Actor) == "" || strings.TrimSpace(spec.Reason) == "" {
		return rep, fmt.Errorf("archive: actor and reason are required")
	}
	now := spec.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var sources []string
	if spec.BeforeMonth != "" {
		months, err := PlanEvents(root, spec.BeforeMonth)
		if err != nil {
			return rep, err
		}
		sources = append(sources, months...)
	}
	if len(spec.RunIDs) > 0 {
		streams, err := PlanRuns(root, spec.RunIDs)
		if err != nil {
			return rep, err
		}
		sources = append(sources, streams...)
	}
	st, err := storage.Open(root, storage.Options{})
	if err != nil {
		return rep, err
	}
	devsys := st.DevsysDir()
	rep.BytesBefore = liveSize(devsys)
	if len(sources) == 0 {
		rep.Note = "nothing to archive"
		rep.BytesAfter = rep.BytesBefore
		return rep, nil
	}
	type staged struct {
		src, dst string
		data     []byte
		lines    int
	}
	var files []staged
	for _, src := range sources {
		data, err := os.ReadFile(filepath.Join(devsys, filepath.FromSlash(src)))
		if err != nil {
			if os.IsNotExist(err) {
				// A run without a stream (never executed) archives as a
				// skip, not an error: there is nothing to move.
				rep.Skipped = append(rep.Skipped, src+" (no stream)")
				continue
			}
			return rep, err
		}
		dst := eventsPrefix + strings.TrimPrefix(src, liveEvents)
		if strings.HasPrefix(src, liveRuns) {
			dst = runsPrefix + strings.TrimPrefix(src, liveRuns)
		}
		if _, err := os.Stat(filepath.Join(devsys, filepath.FromSlash(dst))); err == nil {
			rep.Skipped = append(rep.Skipped, src+" (already archived)")
			continue
		}
		lines := 0
		for _, ln := range strings.Split(string(data), "\n") {
			if ln != "" {
				lines++
			}
		}
		files = append(files, staged{src: src, dst: dst, data: data, lines: lines})
	}
	if spec.DryRun {
		for _, f := range files {
			rep.Archived = append(rep.Archived, f.src+" -> "+f.dst)
			rep.BytesArchived += int64(len(f.data))
		}
		rep.BytesAfter = rep.BytesBefore
		return rep, nil
	}
	if len(files) == 0 {
		rep.Note = "nothing to archive"
		rep.BytesAfter = rep.BytesBefore
		return rep, nil
	}
	// Manifest entries are built outside the write; the manifest itself is
	// read AND rewritten inside the transaction, so two concurrent archives
	// serialize on the manifest CAS instead of losing each other's entries
	// (方案 §15.2 乐观并发): the loser fails with storage.ErrConflict and
	// must retry the whole Apply.
	newEntries := func() []Entry {
		var out []Entry
		for _, f := range files {
			kind := "events"
			if strings.HasPrefix(f.src, liveRuns) {
				kind = "run_stream"
			}
			out = append(out, Entry{
				Kind: kind, Source: f.src, SHA256: sha256Hex(f.data),
				Bytes: int64(len(f.data)), Lines: f.lines,
				ArchivedAt: now.UTC().Format(time.RFC3339), ArchivedBy: spec.Actor,
				Reason: spec.Reason,
			})
		}
		return out
	}()
	var man Manifest
	err = st.Write(ctx, func(tx *storage.Tx) error {
		for _, f := range files {
			cur, exists, err := tx.ReadForExpect(f.src)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("archive: %s vanished during apply", f.src)
			}
			if sha256Hex(cur) != sha256Hex(f.data) {
				return fmt.Errorf("archive: %s changed during apply", f.src)
			}
			if err := tx.Put(f.dst, f.data, storage.ExpectAbsent()); err != nil {
				return err
			}
			if err := tx.Delete(f.src, storage.ExpectHash(storage.HashBytes(f.data))); err != nil {
				return err
			}
		}
		raw, exists, err := tx.ReadForExpect(manifestRel)
		if err != nil {
			return err
		}
		man = Manifest{}
		var guard storage.Expect
		if !exists {
			guard = storage.ExpectAbsent()
		} else {
			if err := domain.DecodeYAML(raw, &man); err != nil {
				return fmt.Errorf("archive: decode manifest: %w", err)
			}
			guard = storage.ExpectHash(storage.HashBytes(raw))
		}
		if man.SchemaVersion == 0 {
			man.SchemaVersion = schemaVersion
		} else if man.SchemaVersion != schemaVersion {
			return fmt.Errorf("archive: manifest schema_version %d unsupported", man.SchemaVersion)
		}
		man.Entries = append(man.Entries, newEntries...)
		sort.Slice(man.Entries, func(i, j int) bool { return man.Entries[i].Source < man.Entries[j].Source })
		return tx.PutYAML(manifestRel, man, guard)
	})
	if err != nil {
		return rep, err
	}
	for _, f := range files {
		rep.Archived = append(rep.Archived, f.src+" -> "+f.dst)
		rep.BytesArchived += int64(len(f.data))
	}
	rep.Manifest = man
	rep.BytesAfter = liveSize(devsys)
	return rep, nil
}
