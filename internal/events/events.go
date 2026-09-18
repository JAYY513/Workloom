// Package events appends and filters the project event stream under
// .devsys/events/<YYYY-MM>.jsonl (方案 §4.6, 实施计划 M1.3). One event is one
// JSONL line; the shard follows the event time in UTC, and comment events
// carry a one-level reply_to.
package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"workloom/internal/domain"
	"workloom/internal/storage"
)

const dirRel = "events"

// Errors reported to callers.
var (
	ErrNotInitialized   = storage.ErrNotInitialized
	ErrIncompleteTail   = storage.ErrIncompleteTail
	ErrReplyNotFound    = errors.New("reply_to does not reference a comment of the same subject")
	ErrReplyNotOneLevel = errors.New("reply_to must reference a top-level comment")
	ErrBadTime          = errors.New("event time must be a valid UTC time")
)

// Store binds event operations to a project root.
type Store struct {
	root string
}

// New binds the event stream to a project root (the directory containing
// .devsys, which must exist).
func New(root string) *Store { return &Store{root: root} }

func (s *Store) store() (*storage.Store, error) { return storage.Open(s.root, storage.Options{}) }

// ShardRel maps a UTC time to its managed file, relative to .devsys.
func ShardRel(t time.Time) string {
	return fmt.Sprintf("%s/%04d-%02d.jsonl", dirRel, t.UTC().Year(), int(t.UTC().Month()))
}

// Append validates and writes one event. The shard follows the event's own
// time, so a December event written on January 1st still lands in the
// December file. The write is a JSONL append inside a storage transaction,
// so interrupted runs are recovered before the append is retried.
func (s *Store) Append(ctx context.Context, ev *domain.Event) error {
	if err := prepareEvent(ev); err != nil {
		return err
	}
	st, err := s.store()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(st.DevsysDir(), dirRel), 0o755); err != nil {
		return fmt.Errorf("create events directory: %w", err)
	}
	return st.Write(ctx, func(tx *storage.Tx) error {
		return tx.AppendJSONL(ShardRel(ev.Time), ev)
	})
}

// prepareEvent fills the derived fields of an event and validates its shape:
// time defaults to UTC now, the ID derives from the time, and the schema
// version is stamped. reply_to stays restricted to comment events.
func prepareEvent(ev *domain.Event) error {
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	if ev.ID == "" {
		ev.ID = newEventID(ev.Time)
	}
	if ev.SchemaVersion == 0 {
		ev.SchemaVersion = domain.SchemaVersion
	}
	if ev.Type == "" {
		return fmt.Errorf("%w: type is empty", ErrBadTime)
	}
	if ev.Subject.Type == "" || ev.Subject.ID == "" {
		return fmt.Errorf("event subject must reference type and id")
	}
	if ev.ReplyTo != nil && ev.Type != "comment" {
		return fmt.Errorf("reply_to is only valid on comment events")
	}
	return nil
}

// AppendTx stages one event into an existing storage transaction.
// supplies its own tx (already holding the project lock). ev.ID is assigned
// from ev.Time if empty; ev.Time is set to UTC now if zero. ev.SchemaVersion
// is set to domain.SchemaVersion if zero. Use this instead of reaching into
// tx.AppendJSONL directly: it centralises shard-path derivation and ID
// generation so callers don't reinvent the rules.
func AppendTx(tx *storage.Tx, ev *domain.Event) error {
	if err := prepareEvent(ev); err != nil {
		return err
	}
	return tx.AppendJSONL(ShardRel(ev.Time), ev)
}

// AppendBatchTx stages several events into one transaction. The transaction
// contract stages each JSONL file once, so events sharing a month shard are
// concatenated into a single append; events of different shards keep their
// own append. This keeps a completion (status change plus propagation
// events) atomic.
func AppendBatchTx(tx *storage.Tx, evs []*domain.Event) error {
	if len(evs) == 0 {
		return nil
	}
	byShard := map[string][]*domain.Event{}
	var shards []string
	for _, ev := range evs {
		if err := prepareEvent(ev); err != nil {
			return err
		}
		rel := ShardRel(ev.Time)
		if _, ok := byShard[rel]; !ok {
			shards = append(shards, rel)
		}
		byShard[rel] = append(byShard[rel], ev)
	}
	sort.Strings(shards)
	for _, rel := range shards {
		var payload []byte
		for _, ev := range byShard[rel] {
			line, err := storage.MarshalJSONL(ev)
			if err != nil {
				return err
			}
			payload = append(payload, line...)
		}
		if err := tx.AppendJSONLRaw(rel, payload); err != nil {
			return err
		}
	}
	return nil
}

// newEventID derives a collision-resistant ID from the event time plus four
// random bytes; there is no global counter to keep in sync. Several events of
// one transaction share a microsecond, so the random suffix carries the
// disambiguation.
func newEventID(t time.Time) string {
	t = t.UTC()
	var rnd [4]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		panic(err) // crypto/rand failure is a process-level fault
	}
	return fmt.Sprintf("ev-%s-%s", t.Format("20060102150405.000000"), hex.EncodeToString(rnd[:]))
}

// Filter describes which events a reader wants. Zero fields match anything.
type Filter struct {
	// From and FromExclusive bound Time: inclusive and exclusive lower edge.
	From          time.Time
	FromExclusive time.Time
	// To bounds Time inclusively.
	To time.Time
	// Subject keeps events whose subject matches both fields.
	Subject *domain.Reference
	// Type keeps only events of this type.
	Type string
}

// matches applies the filter to one event.
func (f Filter) matches(ev *domain.Event) bool {
	if !f.From.IsZero() && ev.Time.Before(f.From) {
		return false
	}
	if !f.FromExclusive.IsZero() && !ev.Time.After(f.FromExclusive) {
		return false
	}
	if !f.To.IsZero() && ev.Time.After(f.To) {
		return false
	}
	if f.Subject != nil && (ev.Subject.Type != f.Subject.Type || ev.Subject.ID != f.Subject.ID) {
		return false
	}
	if f.Type != "" && ev.Type != f.Type {
		return false
	}
	return true
}

// Read returns the events of the stream in time order, filtered by f.
// Shards are read in ascending month order, so the result ascends even
// across month boundaries.
func (s *Store) Read(ctx context.Context, f Filter) ([]*domain.Event, error) {
	st, err := s.store()
	if err != nil {
		return nil, err
	}
	var shards []string
	err = st.Read(ctx, func(r *storage.Reader) error {
		var lerr error
		shards, lerr = listShards(st, f)
		return lerr
	})
	if err != nil {
		return nil, err
	}
	var out []*domain.Event
	for _, rel := range shards {
		if err := s.readShard(ctx, rel, f, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) readShard(ctx context.Context, rel string, f Filter, out *[]*domain.Event) error {
	st, err := s.store()
	if err != nil {
		return err
	}
	return st.Read(ctx, func(r *storage.Reader) error {
		data, exists, err := r.Read(rel)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if line == "" {
				continue
			}
			ev := &domain.Event{}
			if err := domain.DecodeJSON([]byte(line), ev); err != nil {
				return fmt.Errorf("%s: %w", rel, err)
			}
			if f.matches(ev) {
				*out = append(*out, ev)
			}
		}
		return nil
	})
}

// listShards returns the managed shard paths overlapping the filter window,
// in ascending month order.
func listShards(st *storage.Store, f Filter) ([]string, error) {
	entries, err := filepath.Glob(filepath.Join(st.DevsysDir(), dirRel, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	var rels []string
	for _, abs := range entries {
		name := strings.TrimSuffix(filepath.Base(abs), ".jsonl")
		if len(name) != 7 || name[4] != '-' {
			continue
		}
		month, err := time.Parse("2006-01", name)
		if err != nil {
			continue
		}
		if !overlaps(f, month) {
			continue
		}
		rels = append(rels, dirRel+"/"+name+".jsonl")
	}
	return rels, nil
}

// overlaps reports whether the filter window may contain events in month.
func overlaps(f Filter, month time.Time) bool {
	monthEnd := month.AddDate(0, 1, 0)
	if !f.To.IsZero() && f.To.Before(month) {
		return false
	}
	if !f.From.IsZero() && !f.From.Before(monthEnd) {
		return false
	}
	if !f.FromExclusive.IsZero() && !f.FromExclusive.Before(monthEnd) {
		return false
	}
	return true
}

// CommentThread is one top-level comment with its one-level replies, ordered
// by time.
type CommentThread struct {
	Comment *domain.Event
	Replies []*domain.Event
}

// Comments aggregates the comment events of a subject into threads. A reply
// must reference a comment of the same subject; deeper nesting (a reply to a
// reply) is rejected, so the thread tree is always two levels deep.
func (s *Store) Comments(ctx context.Context, subject domain.Reference) ([]*CommentThread, error) {
	all, err := s.Read(ctx, Filter{Type: "comment", Subject: &subject})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*domain.Event, len(all))
	for _, ev := range all {
		byID[ev.ID] = ev
	}
	var threads []*CommentThread
	index := make(map[string]int, len(all))
	for _, ev := range all {
		if ev.ReplyTo == nil {
			index[ev.ID] = len(threads)
			threads = append(threads, &CommentThread{Comment: ev})
			continue
		}
		parent, ok := byID[*ev.ReplyTo]
		if !ok || parent.Subject != ev.Subject {
			return nil, fmt.Errorf("%w: %s -> %s", ErrReplyNotFound, ev.ID, *ev.ReplyTo)
		}
		if parent.ReplyTo != nil {
			return nil, fmt.Errorf("%w: %s -> %s", ErrReplyNotFound, ev.ID, *ev.ReplyTo)
		}
		i, ok := index[parent.ID]
		if !ok {
			return nil, fmt.Errorf("%w: parent %s not a top-level comment", ErrReplyNotFound, parent.ID)
		}
		threads[i].Replies = append(threads[i].Replies, ev)
	}
	return threads, nil
}
