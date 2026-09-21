package record

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/storage"
)

// ReadDecision reads one decision with its raw bytes: the version hash the
// surfaces report and the guard an update consumes both derive from them.
func (s *Store) ReadDecision(ctx context.Context, id string) (*domain.Decision, []byte, error) {
	return readSnapshot[domain.Decision](s, ctx, KindDecision, id)
}

// ReadFinding reads one finding with its raw bytes.
func (s *Store) ReadFinding(ctx context.Context, id string) (*domain.Finding, []byte, error) {
	return readSnapshot[domain.Finding](s, ctx, KindFinding, id)
}

// ReadArtifact reads one artifact with its raw bytes.
func (s *Store) ReadArtifact(ctx context.Context, id string) (*domain.Artifact, []byte, error) {
	return readSnapshot[domain.Artifact](s, ctx, KindArtifact, id)
}

// readSnapshot reads one record plus its raw bytes.
func readSnapshot[T any](s *Store, ctx context.Context, kind Kind, id string) (*T, []byte, error) {
	if !kind.ValidID(id) {
		return nil, nil, fmt.Errorf("%w: %q", ErrBadID, id)
	}
	st, err := s.store()
	if err != nil {
		return nil, nil, err
	}
	var (
		out = new(T)
		raw []byte
	)
	err = st.Read(ctx, func(r *storage.Reader) error {
		data, exists, err := r.Read(kind.dir() + "/" + id + ".yaml")
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		if err := domain.DecodeYAML(data, out); err != nil {
			return err
		}
		raw = data
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return out, raw, nil
}

// UpdateDecision rewrites a decision under the version guard (the caller's
// expected bytes come from ReadDecision).
func (s *Store) UpdateDecision(ctx context.Context, d *domain.Decision, expected []byte) error {
	return s.update(ctx, KindDecision, d, d.ID, expected)
}

// UpdateFinding rewrites a finding under the version guard.
func (s *Store) UpdateFinding(ctx context.Context, f *domain.Finding, expected []byte) error {
	return s.update(ctx, KindFinding, f, f.ID, expected)
}

// ErrSuperseded reports that the artifact already has a newer version: the
// version chain is linear, so a second append from the same parent is
// refused instead of forking history. Re-read the head and append from it.
var ErrSuperseded = errors.New("record: artifact already has a newer version")

// AppendArtifactVersion appends a new artifact version: the previous version
// is read inside the write transaction, the caller's expected bytes (from
// ReadArtifact) must still match, mutate may patch the copy, and the result
// is written as a new file (previous_id links back; the old file is never
// rewritten). It returns the new artifact's ID.
//
// The append is refused with ErrSuperseded when a child of currentID already
// exists, so concurrent appends cannot fork the chain or duplicate version
// numbers.
func (s *Store) AppendArtifactVersion(ctx context.Context, currentID string, expected []byte, mutate func(*domain.Artifact)) (string, error) {
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
			if len(expected) > 0 && !bytes.Equal(data, expected) {
				return storage.ErrConflict
			}
			if err := domain.DecodeYAML(data, cur); err != nil {
				return err
			}
			child, err := hasSuccessor(st, currentID)
			if err != nil {
				return err
			}
			if child != "" {
				return ErrSuperseded
			}
			suffix, scanErr := nextNumber(st, KindArtifact)
			if scanErr != nil {
				return scanErr
			}
			newID = fmt.Sprintf("%s-%d", KindArtifact, suffix)
			next := *cur
			if mutate != nil {
				mutate(&next)
			}
			next.ID = newID
			next.SchemaVersion = domain.SchemaVersion
			next.Version = cur.Version + 1
			prev := currentID
			next.PreviousID = &prev
			return tx.PutYAML(KindArtifact.dir()+"/"+newID+".yaml", &next, storage.ExpectAbsent())
		})
		if err == nil {
			return newID, nil
		}
		if errors.Is(err, storage.ErrConflict) {
			// Either the caller's snapshot is stale or the new ID collided;
			// both need a fresh read, so surface the conflict.
			return "", err
		}
		return "", err
	}
	return "", fmt.Errorf("append artifact version: gave up after %d conflicts", maxAttempts)
}

// hasSuccessor reports the ID of an artifact whose previous_id points at id.
func hasSuccessor(st *storage.Store, id string) (string, error) {
	var child string
	ids, err := listIDs(st, KindArtifact)
	if err != nil {
		return "", err
	}
	for _, candidate := range ids {
		if candidate == id {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(st.DevsysDir(), KindArtifact.dir(), candidate+".yaml"))
		if err != nil {
			return "", err
		}
		var a domain.Artifact
		if err := domain.DecodeYAML(raw, &a); err != nil {
			return "", err
		}
		if a.PreviousID != nil && *a.PreviousID == id {
			child = candidate
			break
		}
	}
	return child, nil
}

func (s *Store) update(ctx context.Context, kind Kind, v any, id string, expected []byte) error {
	if !kind.ValidID(id) {
		return fmt.Errorf("%w: %q", ErrBadID, id)
	}
	st, err := s.store()
	if err != nil {
		return err
	}
	return st.Write(ctx, func(tx *storage.Tx) error {
		return tx.PutYAML(kind.dir()+"/"+id+".yaml", v, storage.ExpectHash(storage.HashBytes(expected)))
	})
}
