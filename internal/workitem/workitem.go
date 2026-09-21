// Package workitem persists WorkItem records as one file per task under
// .devsys/workitems/<id>.yaml (方案 §14.2, 实施计划 M1.2). IDs are
// <PREFIX>-<N>; sequence numbers are re-derived from the directory on every
// create, and the create itself is a CAS (ExpectAbsent) so a concurrent
// create of the same ID has exactly one winner.
package workitem

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

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/storage"
)

const dirRel = "workitems"

// Errors reported to callers. ErrIDTaken means the target file appeared
// between the scan and the write; Create retries with a new number.
var (
	ErrNotInitialized = storage.ErrNotInitialized
	ErrNotFound       = errors.New("workitem not found")
	ErrConflict       = errors.New("workitem changed concurrently")
	ErrBadPrefix      = errors.New("invalid workitem prefix")
	// ErrBadID reports a malformed work item id before any path is built, so a
	// caller sees "invalid workitem id" instead of the storage layer's
	// "unsafe managed path" fallback.
	ErrBadID = errors.New("invalid workitem id")
)

// Store binds WorkItem operations to a project root.
type Store struct {
	root string
}

// New binds the workitem layer to a project root (the directory containing
// .devsys, which must exist).
func New(root string) *Store { return &Store{root: root} }

var idPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-[0-9]+$`)

// ValidID reports whether id is a well-formed <PREFIX>-<N> identifier.
func ValidID(id string) bool { return idPattern.MatchString(id) }

func (s *Store) store() (*storage.Store, error) { return storage.Open(s.root, storage.Options{}) }

// Create assigns the next free sequence number for prefix, writes the record
// and returns its ID. Inside the same storage transaction the number is
// derived from the directory and the file is guarded with ExpectAbsent, so
// two concurrent creates never share an ID: the loser retries and lands on
// the next free number.
func (s *Store) Create(ctx context.Context, wi *domain.WorkItem, prefix string) (string, error) {
	prefix = strings.ToUpper(prefix)
	if !regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`).MatchString(prefix) {
		return "", fmt.Errorf("%w: %q", ErrBadPrefix, prefix)
	}
	if wi.Status == "" {
		// Default: a freshly-created task lives in draft until the user
		// transitions it forward (§6.1). Defaulting here keeps the existing
		// smoke scripts unchanged while still routing every other status
		// through the validation below.
		wi.Status = domain.StatusDraft
	}
	if !domain.UnclaimedInitial[wi.Status] {
		return "", fmt.Errorf("%w: initial status %q not in {draft, backlog, ready, blocked}", ErrInvalidInput, wi.Status)
	}
	st, err := s.store()
	if err != nil {
		return "", err
	}
	const maxAttempts = 5
	for range maxAttempts {
		var id string
		err = st.Write(ctx, func(tx *storage.Tx) error {
			next, scanErr := nextNumber(st, prefix)
			if scanErr != nil {
				return scanErr
			}
			id = fmt.Sprintf("%s-%d", prefix, next)
			wi.ID = id
			if wi.SchemaVersion == 0 {
				wi.SchemaVersion = domain.SchemaVersion
			}
			return tx.PutYAML(dirRel+"/"+id+".yaml", wi, storage.ExpectAbsent())
		})
		if err == nil {
			return id, nil
		}
		if errors.Is(err, storage.ErrConflict) {
			continue // someone else took the number; rescan and retry.
		}
		return "", err
	}
	return "", fmt.Errorf("create %s work item: gave up after %d conflicts", prefix, maxAttempts)
}

// Get decodes one work item.
func (s *Store) Get(ctx context.Context, id string) (*domain.WorkItem, error) {
	if !ValidID(id) {
		return nil, fmt.Errorf("%w: %q", ErrBadID, id)
	}
	st, err := s.store()
	if err != nil {
		return nil, err
	}
	var wi *domain.WorkItem
	err = st.Read(ctx, func(r *storage.Reader) error {
		data, exists, err := r.Read(dirRel + "/" + id + ".yaml")
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		wi = &domain.WorkItem{}
		return domain.DecodeYAML(data, wi)
	})
	if err != nil {
		return nil, err
	}
	return wi, nil
}

// List decodes every work item, ordered by numeric sequence per prefix — a
// stable order that does not depend on directory enumeration or mtime.
func (s *Store) List(ctx context.Context) ([]*domain.WorkItem, error) {
	st, err := s.store()
	if err != nil {
		return nil, err
	}
	items := []*domain.WorkItem{}
	err = st.Read(ctx, func(r *storage.Reader) error {
		ids, err := listIDs(st)
		if err != nil {
			return err
		}
		for _, id := range ids {
			data, exists, err := r.Read(dirRel + "/" + id + ".yaml")
			if err != nil {
				return err
			}
			if !exists {
				continue // removed between listing and read; next read sees it gone.
			}
			wi := &domain.WorkItem{}
			if err := domain.DecodeYAML(data, wi); err != nil {
				return fmt.Errorf("%s: %w", id, err)
			}
			items = append(items, wi)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// Update replaces a work item only while the file still matches the bytes
// the caller read; a concurrent edit surfaces as storage.ErrConflict instead
// of being overwritten (乐观并发, 方案 §15.2). expected is the raw file
// content the caller's read snapshot was decoded from.
//
// Update refuses to mutate the Status field: use Transition or ApplyRepair
// for status changes (§6). Other fields are written as supplied.
func (s *Store) Update(ctx context.Context, wi *domain.WorkItem, expected []byte) error {
	if wi == nil || !ValidID(wi.ID) {
		return fmt.Errorf("%w: %q", ErrBadID, wi.ID)
	}
	if !statusOnlyUnchangedAgainstSnapshot(wi, expected) {
		return fmt.Errorf("%w: status changes must go through Transition or ApplyRepair", ErrInvalidInput)
	}
	// When a lease is active, plain Update is forbidden: the claimer must
	// present owner/token via UpdateClaimed. This prevents a stale
	// claimant from bypassing lease fencing on metadata fields.
	if hasActiveLeaseInSnapshot(expected) {
		return fmt.Errorf("%w: work item %s has an active lease; release it first (`devsys workitem release --id %s --owner <owner> --token <token> --actor <you> --reason <why>`) or update through the claim holder", ErrInvalidInput, wi.ID, wi.ID)
	}
	st, err := s.store()
	if err != nil {
		return err
	}
	return st.Write(ctx, func(tx *storage.Tx) error {
		return tx.PutYAML(dirRel+"/"+wi.ID+".yaml", wi, storage.ExpectHash(storage.HashBytes(expected)))
	})
}

// statusOnlyUnchangedAgainstSnapshot returns true iff the Status field in
// the proposed WorkItem matches what is currently on disk. We rely on the
// CAS guard (ExpectHash(expected)) for fields other than Status: a caller
// that also wants to track Description / AcceptanceCriteria / etc. through
// Update continues to work, but Status changes must be expressed via
// Transition (normal flow) or ApplyRepair (repair flow).
func statusOnlyUnchangedAgainstSnapshot(wi *domain.WorkItem, expected []byte) bool {
	if len(expected) == 0 {
		return false
	}
	cur := &domain.WorkItem{}
	if err := domain.DecodeYAML(expected, cur); err != nil {
		return false
	}
	return cur.Status == wi.Status
}

// hasActiveLeaseInSnapshot inspects the expected bytes for any non-empty
// LeaseOwner + LeaseToken. Empty strings mean "no active lease"; a lease
// file alone (without the workitem fields) does not protect.
func hasActiveLeaseInSnapshot(expected []byte) bool {
	if len(expected) == 0 {
		return false
	}
	cur := &domain.WorkItem{}
	if err := domain.DecodeYAML(expected, cur); err != nil {
		return false
	}
	return cur.LeaseOwner != "" || cur.LeaseToken != ""
}

// ReadSnapshot returns a work item together with the raw bytes it was
// decoded from, for read-modify-write cycles that pass the bytes back into
// Update as the optimistic guard.
func (s *Store) ReadSnapshot(ctx context.Context, id string) (*domain.WorkItem, []byte, error) {
	if !ValidID(id) {
		return nil, nil, fmt.Errorf("%w: %q", ErrBadID, id)
	}
	st, err := s.store()
	if err != nil {
		return nil, nil, err
	}
	var (
		wi   *domain.WorkItem
		data []byte
	)
	err = st.Read(ctx, func(r *storage.Reader) error {
		var exists bool
		var derr error
		data, exists, derr = r.Read(dirRel + "/" + id + ".yaml")
		if derr != nil {
			return derr
		}
		if !exists {
			return ErrNotFound
		}
		wi = &domain.WorkItem{}
		return domain.DecodeYAML(data, wi)
	})
	if err != nil {
		return nil, nil, err
	}
	return wi, data, nil
}

// nextNumber returns max(existing sequence for prefix)+1.
func nextNumber(st *storage.Store, prefix string) (int, error) {
	ids, err := listIDs(st)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, id := range ids {
		n, err := strconv.Atoi(strings.TrimPrefix(id, prefix+"-"))
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max + 1, nil
}

// listIDs enumerates workitems/*.yaml whose names match the ID pattern,
// sorted by (prefix, numeric suffix).
func listIDs(st *storage.Store) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(st.DevsysDir(), filepath.FromSlash(dirRel)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dirRel, err)
	}
	type parsed struct {
		id  string
		num int
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
		n, err := strconv.Atoi(id[strings.LastIndex(id, "-")+1:])
		if err != nil {
			continue
		}
		found = append(found, parsed{id: id, num: n})
	}
	sort.SliceStable(found, func(i, j int) bool {
		pi, pj := found[i].id[:strings.LastIndex(found[i].id, "-")], found[j].id[:strings.LastIndex(found[j].id, "-")]
		if pi != pj {
			return pi < pj
		}
		return found[i].num < found[j].num
	})
	ids := make([]string, len(found))
	for i, f := range found {
		ids[i] = f.id
	}
	return ids, nil
}
