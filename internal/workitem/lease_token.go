package workitem

import (
	"bytes"
	"context"
	"fmt"

	"workloom/internal/storage"
)

// tokenSidecarRel maps a work item id to its local-only fencing token
// sidecar. Since #342 the committed scheduling/<id>.yaml and
// workitems/<id>.yaml carry NO token: 方案 §16.3 keeps credentials out of
// the state tree, while §14.4 keeps the lease metadata (owner, claimed_at,
// lease_until, run) visible in git for cross-device drift detection. The
// sidecar lives under local/, which .gitignore already excludes.
func tokenSidecarRel(id string) string { return "local/leases/" + id + ".token" }

// stageToken writes the sidecar inside a claim/start transaction. ExpectAny
// tolerates a stale sidecar left behind by an interrupted claim.
func stageToken(tx *storage.Tx, id, token string) error {
	return tx.Put(tokenSidecarRel(id), []byte(token), storage.ExpectAny())
}

// dropToken removes the sidecar inside a release/retry transaction.
func dropToken(tx *storage.Tx, id string) error {
	return tx.Delete(tokenSidecarRel(id), storage.ExpectAny())
}

// tokenFromTx resolves the fencing token for id inside a transaction: the
// sidecar wins; when it is absent (a lease claimed before the sidecar
// existed) the legacy value persisted in the state file stands in, so old
// trees keep validating until the claim rotates.
func tokenFromTx(tx *storage.Tx, id, legacy string) (string, error) {
	b, ok, err := tx.ReadForExpect(tokenSidecarRel(id))
	if err != nil {
		return "", err
	}
	if ok && len(bytes.TrimSpace(b)) > 0 {
		return string(bytes.TrimSpace(b)), nil
	}
	return legacy, nil
}

// tokenFromReader resolves the fencing token outside a write transaction
// (early validations that run before the write tx opens).
func tokenFromReader(r *storage.Reader, id, legacy string) (string, error) {
	b, ok, err := r.Read(tokenSidecarRel(id))
	if err != nil {
		return "", err
	}
	if ok && len(bytes.TrimSpace(b)) > 0 {
		return string(bytes.TrimSpace(b)), nil
	}
	return legacy, nil
}

// validateToken checks a presented token against the sidecar (or the legacy
// persisted value) outside a write transaction, for the early validations
// that run before the write tx opens.
func (s *Store) validateToken(ctx context.Context, id, legacy, presented string) error {
	st, err := s.store()
	if err != nil {
		return err
	}
	return st.Read(ctx, func(r *storage.Reader) error {
		tok, err := tokenFromReader(r, id, legacy)
		if err != nil {
			return err
		}
		if tok == "" || tok != presented {
			return ErrLeaseTokenMismatch
		}
		return nil
	})
}

// LeaseToken returns the current fencing token for id from the local
// sidecar. It exists for local operators — the dispatch tick, `devsys
// recover` — that must act on a claim without its holder presenting the
// token. The token still never enters the committed state tree (#342).
func (s *Store) LeaseToken(ctx context.Context, id string) (string, error) {
	if !ValidID(id) {
		return "", fmt.Errorf("%w: %q", ErrBadID, id)
	}
	st, err := s.store()
	if err != nil {
		return "", err
	}
	var tok string
	err = st.Read(ctx, func(r *storage.Reader) error {
		var rerr error
		tok, rerr = tokenFromReader(r, id, "")
		return rerr
	})
	if err != nil {
		return "", err
	}
	return tok, nil
}
