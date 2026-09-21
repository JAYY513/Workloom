// Package run persists Run records under .devsys/runs/<id>.yaml (方案 §14.3,
// 实施计划 M1.4). IDs are run-<YYYYMMDD>-<seq>; the sequence is re-derived
// from the directory inside the create transaction, and the create itself is
// a CAS, so concurrent creates never share an ID.
package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/storage"
)

const dirRel = "runs"

// Errors reported to callers.
var (
	ErrNotFound = errors.New("run not found")
	ErrBadID    = errors.New("invalid run id")
)

var idPattern = regexp.MustCompile(`^run-[0-9]{8}-[0-9]+$`)

// Store binds Run operations to a project root.
type Store struct {
	root string
}

// New binds the run layer to a project root (the directory containing
// .devsys, which must exist).
func New(root string) *Store { return &Store{root: root} }

func (s *Store) store() (*storage.Store, error) { return storage.Open(s.root, storage.Options{}) }

// ValidID reports whether id is a well-formed run identifier.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Create persists a new run, assigning the next sequence number for today's
// date when the ID is empty. The write is guarded with ExpectAbsent, so two
// concurrent creates of the same number have exactly one winner; the loser
// rescan and retries.
func (s *Store) Create(ctx context.Context, r *domain.Run) (string, error) {
	st, err := s.store()
	if err != nil {
		return "", err
	}
	const maxAttempts = 5
	for range maxAttempts {
		var id string
		err = st.Write(ctx, func(tx *storage.Tx) error {
			if r.ID == "" {
				next, scanErr := nextNumber(st, timeBase(r.StartedAt))
				if scanErr != nil {
					return scanErr
				}
				r.ID = fmt.Sprintf("run-%s-%d", timeBase(r.StartedAt), next)
			}
			id = r.ID
			if !ValidID(id) {
				return fmt.Errorf("%w: %q", ErrBadID, id)
			}
			if r.SchemaVersion == 0 {
				r.SchemaVersion = domain.SchemaVersion
			}
			if r.Attempt == 0 {
				r.Attempt = 1
			}
			if r.Status == "" {
				r.Status = "running"
			}
			return tx.PutYAML(dirRel+"/"+id+".yaml", r, storage.ExpectAbsent())
		})
		if err == nil {
			return id, nil
		}
		if errors.Is(err, storage.ErrConflict) {
			r.ID = ""
			continue
		}
		return "", err
	}
	return "", fmt.Errorf("create run: gave up after %d conflicts", maxAttempts)
}

// CreateTx stages a new run inside an existing storage transaction. The caller
// holds the project lock (via storage.Write) so the date-sequence scan and
// ExpectAbsent put both succeed or both fail with the caller's tx; concurrent
// claims never see a half-written run record. r.ID must be empty so the
// helper can derive it; the assigned ID is written back into r.
func (s *Store) CreateTx(tx *storage.Tx, r *domain.Run) error {
	if r.ID != "" {
		return fmt.Errorf("%w: CreateTx requires an empty ID", ErrBadID)
	}
	st, err := s.store()
	if err != nil {
		return err
	}
	next, err := nextNumber(st, timeBase(r.StartedAt))
	if err != nil {
		return err
	}
	r.ID = fmt.Sprintf("run-%s-%d", timeBase(r.StartedAt), next)
	if !ValidID(r.ID) {
		return fmt.Errorf("%w: %q", ErrBadID, r.ID)
	}
	if r.SchemaVersion == 0 {
		r.SchemaVersion = domain.SchemaVersion
	}
	if r.Attempt == 0 {
		r.Attempt = 1
	}
	if r.Status == "" {
		r.Status = "running"
	}
	return tx.PutYAML(dirRel+"/"+r.ID+".yaml", r, storage.ExpectAbsent())
}

func timeBase(t time.Time) string { return t.UTC().Format("20060102") }

// Get decodes one run.
func (s *Store) Get(ctx context.Context, id string) (*domain.Run, error) {
	if !ValidID(id) {
		return nil, fmt.Errorf("%w: %q", ErrBadID, id)
	}
	st, err := s.store()
	if err != nil {
		return nil, err
	}
	var r *domain.Run
	err = st.Read(ctx, func(rd *storage.Reader) error {
		data, exists, err := rd.Read(dirRel + "/" + id + ".yaml")
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		r = &domain.Run{}
		return domain.DecodeYAML(data, r)
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ReadSnapshot returns a run with the raw bytes it was decoded from, for
// read-modify-write cycles that pass the bytes back into Update.
func (s *Store) ReadSnapshot(ctx context.Context, id string) (*domain.Run, []byte, error) {
	if !ValidID(id) {
		return nil, nil, fmt.Errorf("%w: %q", ErrBadID, id)
	}
	st, err := s.store()
	if err != nil {
		return nil, nil, err
	}
	var (
		r    *domain.Run
		data []byte
	)
	err = st.Read(ctx, func(rd *storage.Reader) error {
		var exists bool
		var derr error
		data, exists, derr = rd.Read(dirRel + "/" + id + ".yaml")
		if derr != nil {
			return derr
		}
		if !exists {
			return ErrNotFound
		}
		r = &domain.Run{}
		return domain.DecodeYAML(data, r)
	})
	if err != nil {
		return nil, nil, err
	}
	return r, data, nil
}

// List decodes every run, ordered by (date, numeric suffix) — stable and
// independent of directory enumeration.
func (s *Store) List(ctx context.Context) ([]*domain.Run, error) {
	st, err := s.store()
	if err != nil {
		return nil, err
	}
	var items []*domain.Run
	err = st.Read(ctx, func(rd *storage.Reader) error {
		ids, err := listIDs(st)
		if err != nil {
			return err
		}
		for _, id := range ids {
			data, exists, err := rd.Read(dirRel + "/" + id + ".yaml")
			if err != nil {
				return err
			}
			if !exists {
				continue
			}
			r := &domain.Run{}
			if err := domain.DecodeYAML(data, r); err != nil {
				return fmt.Errorf("%s: %w", id, err)
			}
			items = append(items, r)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// Update replaces a run only while the file still matches expected, the raw
// bytes from the caller's read snapshot; a concurrent edit surfaces as
// storage.ErrConflict (乐观并发, 方案 §15.2).
func (s *Store) Update(ctx context.Context, r *domain.Run, expected []byte) error {
	st, err := s.store()
	if err != nil {
		return err
	}
	return st.Write(ctx, func(tx *storage.Tx) error {
		return tx.PutYAML(dirRel+"/"+r.ID+".yaml", r, storage.ExpectHash(storage.HashBytes(expected)))
	})
}

// listIDs enumerates runs/*.yaml with well-formed names, sorted by
// (date, numeric suffix).
func listIDs(st *storage.Store) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(st.DevsysDir(), filepath.FromSlash(dirRel)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dirRel, err)
	}
	type parsed struct {
		id   string
		date string
		num  int
	}
	var found []parsed
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		id := strings.TrimSuffix(name, ".yaml")
		if !ValidID(id) {
			continue
		}
		num, err := strconv.Atoi(id[strings.LastIndex(id, "-")+1:])
		if err != nil {
			continue
		}
		found = append(found, parsed{id: id, date: id[4:12], num: num})
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].date != found[j].date {
			return found[i].date < found[j].date
		}
		return found[i].num < found[j].num
	})
	ids := make([]string, len(found))
	for i, f := range found {
		ids[i] = f.id
	}
	return ids, nil
}

// nextNumber returns max(existing sequence for date)+1.
func nextNumber(st *storage.Store, date string) (int, error) {
	ids, err := listIDs(st)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, id := range ids {
		if !strings.HasPrefix(id, "run-"+date+"-") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(id, "run-"+date+"-"))
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max + 1, nil
}
