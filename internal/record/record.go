// Package record persists Decision, Finding and Artifact records as one file
// per record under .devsys/{decisions,findings,artifacts}/<id>.yaml (方案
// §14.3, 实施计划 M1.5). IDs are decision-<N> / finding-<N> / artifact-<N>
// with a per-kind sequence; Artifact versions are immutable files chained by
// previous_id (方案 §5.5).
package record

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

	"workloom/internal/domain"
	"workloom/internal/storage"
)

// Kind selects the record family and its directory.
type Kind string

const (
	KindDecision Kind = "decision"
	KindFinding  Kind = "finding"
	KindArtifact Kind = "artifact"
)

func (k Kind) dir() string { return string(k) + "s" }

// Errors reported to callers.
var (
	ErrNotFound = errors.New("record not found")
	ErrBadID    = errors.New("invalid record id")
	ErrBadKind  = errors.New("unknown record kind")
	ErrConflict = storage.ErrConflict
)

var idPattern = map[Kind]*regexp.Regexp{
	KindDecision: regexp.MustCompile(`^decision-[0-9]+$`),
	KindFinding:  regexp.MustCompile(`^finding-[0-9]+$`),
	KindArtifact: regexp.MustCompile(`^artifact-[0-9]+$`),
}

// Store binds record operations to a project root.
type Store struct {
	root string
}

// New binds the record layer to a project root (the directory containing
// .devsys, which must exist).
func New(root string) *Store { return &Store{root: root} }

func (s *Store) store() (*storage.Store, error) { return storage.Open(s.root, storage.Options{}) }

// ValidID reports whether id is well-formed for kind.
func (k Kind) ValidID(id string) bool { return idPattern[k].MatchString(id) }

// assignID stamps schema_version and the assigned ID onto a record before
// its bytes are computed inside the transaction.
type stager interface {
	setID(id string)
}

// nextNumber returns max(existing sequence for kind)+1.
func nextNumber(st *storage.Store, kind Kind) (int, error) {
	ids, err := listIDs(st, kind)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, id := range ids {
		n, err := strconv.Atoi(strings.TrimPrefix(id, string(kind)+"-"))
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max + 1, nil
}

// listIDs enumerates <dir>/*.yaml with well-formed names for kind, ordered
// by numeric suffix.
func listIDs(st *storage.Store, kind Kind) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(st.DevsysDir(), kind.dir()))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", kind.dir(), err)
	}
	var found []int
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		id := strings.TrimSuffix(name, ".yaml")
		if !kind.ValidID(id) {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(id, string(kind)+"-"))
		if err != nil {
			continue
		}
		found = append(found, n)
	}
	sort.Ints(found)
	ids := make([]string, len(found))
	for i, n := range found {
		ids[i] = fmt.Sprintf("%s-%d", kind, n)
	}
	return ids, nil
}

// create assigns the next sequence number and stages the record with
// ExpectAbsent, so concurrent creates of the same number have exactly one
// winner; the loser rescans and retries.
func (s *Store) create(ctx context.Context, kind Kind, v any, setID func(int)) (string, error) {
	if idPattern[kind] == nil {
		return "", fmt.Errorf("%w: %q", ErrBadKind, kind)
	}
	st, err := s.store()
	if err != nil {
		return "", err
	}
	const maxAttempts = 5
	for range maxAttempts {
		var id string
		err = st.Write(ctx, func(tx *storage.Tx) error {
			next, scanErr := nextNumber(st, kind)
			if scanErr != nil {
				return scanErr
			}
			id = fmt.Sprintf("%s-%d", kind, next)
			setID(next)
			return tx.PutYAML(kind.dir()+"/"+id+".yaml", v, storage.ExpectAbsent())
		})
		if err == nil {
			return id, nil
		}
		if errors.Is(err, storage.ErrConflict) {
			continue
		}
		return "", err
	}
	return "", fmt.Errorf("create %s record: gave up after %d conflicts", kind, maxAttempts)
}

// CreateDecision stores a decision and returns its assigned ID.
func (s *Store) CreateDecision(ctx context.Context, d *domain.Decision) (string, error) {
	return s.create(ctx, KindDecision, d, func(n int) {
		d.ID = fmt.Sprintf("%s-%d", KindDecision, n)
		d.SchemaVersion = domain.SchemaVersion
	})
}

// CreateFinding stores a finding and returns its assigned ID.
func (s *Store) CreateFinding(ctx context.Context, f *domain.Finding) (string, error) {
	return s.create(ctx, KindFinding, f, func(n int) {
		f.ID = fmt.Sprintf("%s-%d", KindFinding, n)
		f.SchemaVersion = domain.SchemaVersion
	})
}

// GetDecision decodes one decision.
func (s *Store) GetDecision(ctx context.Context, id string) (*domain.Decision, error) {
	d := &domain.Decision{}
	if err := s.get(ctx, KindDecision, id, d); err != nil {
		return nil, err
	}
	return d, nil
}

// GetFinding decodes one finding.
func (s *Store) GetFinding(ctx context.Context, id string) (*domain.Finding, error) {
	f := &domain.Finding{}
	if err := s.get(ctx, KindFinding, id, f); err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Store) get(ctx context.Context, kind Kind, id string, into any) error {
	if !kind.ValidID(id) {
		return fmt.Errorf("%w: %q", ErrBadID, id)
	}
	st, err := s.store()
	if err != nil {
		return err
	}
	return st.Read(ctx, func(r *storage.Reader) error {
		data, exists, err := r.Read(kind.dir() + "/" + id + ".yaml")
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		return domain.DecodeYAML(data, into)
	})
}

// ListDecisions returns every decision in ascending ID order.
func (s *Store) ListDecisions(ctx context.Context) ([]*domain.Decision, error) {
	ids, err := s.list(ctx, KindDecision)
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Decision, 0, len(ids))
	for _, id := range ids {
		d := &domain.Decision{}
		if err := s.get(ctx, KindDecision, id, d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// ListFindings returns every finding in ascending ID order.
func (s *Store) ListFindings(ctx context.Context) ([]*domain.Finding, error) {
	ids, err := s.list(ctx, KindFinding)
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Finding, 0, len(ids))
	for _, id := range ids {
		f := &domain.Finding{}
		if err := s.get(ctx, KindFinding, id, f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// ListArtifacts returns every artifact version in ascending ID order.
func (s *Store) ListArtifacts(ctx context.Context) ([]*domain.Artifact, error) {
	ids, err := s.list(ctx, KindArtifact)
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Artifact, 0, len(ids))
	for _, id := range ids {
		a := &domain.Artifact{}
		if err := s.get(ctx, KindArtifact, id, a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *Store) list(ctx context.Context, kind Kind) ([]string, error) {
	st, err := s.store()
	if err != nil {
		return nil, err
	}
	var ids []string
	err = st.Read(ctx, func(r *storage.Reader) error {
		var lerr error
		ids, lerr = listIDs(st, kind)
		return lerr
	})
	return ids, err
}

// CreateArtifact stores the first version of an artifact (version 1, no
// previous link) and returns its assigned ID.
func (s *Store) CreateArtifact(ctx context.Context, a *domain.Artifact) (string, error) {
	return s.create(ctx, KindArtifact, a, func(n int) {
		a.ID = fmt.Sprintf("%s-%d", KindArtifact, n)
		a.SchemaVersion = domain.SchemaVersion
		a.Version = 1
	})
}

// GetArtifact decodes one artifact version.
func (s *Store) GetArtifact(ctx context.Context, id string) (*domain.Artifact, error) {
	a := &domain.Artifact{}
	if err := s.get(ctx, KindArtifact, id, a); err != nil {
		return nil, err
	}
	return a, nil
}

// NewArtifactVersion appends a new version of an artifact: it reads the
// current file, copies every field into next, then writes next as a NEW file
// with version+1 and previous_id pointing at the current ID. Existing files
// are never modified, so history stays immutable and traversable.
func (s *Store) NewArtifactVersion(ctx context.Context, currentID string, next *domain.Artifact) (string, error) {
	if !KindArtifact.ValidID(currentID) {
		return "", fmt.Errorf("%w: %q", ErrBadID, currentID)
	}
	st, err := s.store()
	if err != nil {
		return "", err
	}
	const maxAttempts = 5
	for range maxAttempts {
		var newID string
		err = st.Write(ctx, func(tx *storage.Tx) error {
			cur := &domain.Artifact{}
			data, exists, err := tx.ReadForExpect(KindArtifact.dir() + "/" + currentID + ".yaml")
			if err != nil {
				return err
			}
			if !exists {
				return ErrNotFound
			}
			if err := domain.DecodeYAML(data, cur); err != nil {
				return err
			}
			suffix, scanErr := nextNumber(st, KindArtifact)
			if scanErr != nil {
				return scanErr
			}
			newID = fmt.Sprintf("%s-%d", KindArtifact, suffix)
			*next = *cur
			next.ID = newID
			next.SchemaVersion = domain.SchemaVersion
			next.Version = cur.Version + 1
			prev := currentID
			next.PreviousID = &prev
			return tx.PutYAML(KindArtifact.dir()+"/"+newID+".yaml", next, storage.ExpectAbsent())
		})
		if err == nil {
			return newID, nil
		}
		if errors.Is(err, storage.ErrConflict) {
			continue
		}
		return "", err
	}
	return "", fmt.Errorf("append artifact version: gave up after %d conflicts", maxAttempts)
}

// ArtifactHistory walks the previous_id chain backwards from id and returns
// the versions newest-first; a broken link is reported, not skipped.
func (s *Store) ArtifactHistory(ctx context.Context, id string) ([]*domain.Artifact, error) {
	var out []*domain.Artifact
	seen := map[string]bool{}
	for cursor := id; cursor != ""; {
		if seen[cursor] {
			return nil, fmt.Errorf("%w: version chain loops at %s", ErrBadID, cursor)
		}
		seen[cursor] = true
		a, err := s.GetArtifact(ctx, cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
		if a.PreviousID == nil {
			break
		}
		if !KindArtifact.ValidID(*a.PreviousID) {
			return nil, fmt.Errorf("%w: %q", ErrBadID, *a.PreviousID)
		}
		cursor = *a.PreviousID
	}
	return out, nil
}
