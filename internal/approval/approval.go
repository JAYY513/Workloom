// Package approval persists Approval records as one file per approval under
// .devsys/approvals/<id>.yaml (方案 §4.9/§5.8, 实施计划 M3.7) and implements
// the request → decide → consume lifecycle.
//
// Every write goes through the storage transaction path with an in-tx read
// and ExpectHash guard, so no caller can bypass version checking.
// Consumption happens inside the advancing work item's transaction via
// ConsumeTx: the approval flips to consumed only when the status change that
// needed it commits.
package approval

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"workloom/internal/domain"
	"workloom/internal/events"
	"workloom/internal/storage"
)

const dirRel = "approvals"

// Lifecycle states and scopes.
const (
	ScopeStageGate = "stage_gate"
	ScopeAction    = "action"

	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"

	eventRequested   = "approval_requested"
	eventApproved    = "approval_approved"
	eventRejected    = "approval_rejected"
	eventConsumed    = "approval_consumed"
	eventInvalidated = "approval_invalidated"
)

// Errors reported to callers.
var (
	ErrNotFound        = errors.New("approval: not found")
	ErrBadID           = errors.New("approval: bad id")
	ErrInvalidInput    = errors.New("approval: invalid input")
	ErrNotPending      = errors.New("approval: not pending")
	ErrNotApproved     = errors.New("approval: not approved")
	ErrAlreadyConsumed = errors.New("approval: already consumed")
	ErrInvalidated     = errors.New("approval: invalidated")
	ErrMismatch        = errors.New("approval: does not match the gate")
)

var idPattern = regexp.MustCompile(`^approval-[0-9]+$`)

// Store binds approval operations to a project root.
type Store struct {
	root string
}

// New binds the approval layer to a project root (the directory containing
// .devsys, which must exist).
func New(root string) *Store { return &Store{root: root} }

func (s *Store) store() (*storage.Store, error) { return storage.Open(s.root, storage.Options{}) }

// ValidID reports whether id is a well-formed approval identifier.
func ValidID(id string) bool { return idPattern.MatchString(id) }

func approvalRel(id string) string { return dirRel + "/" + id + ".yaml" }

func validStatus(status string) bool {
	for _, s := range domain.AllStatuses() {
		if s == status {
			return true
		}
	}
	return false
}

// RequestOptions is the input to Request.
type RequestOptions struct {
	Scope           string
	WorkItemID      string
	RunID           string
	Stage           string
	RequestedStatus string
	ProjectID       string
	RequestedBy     string
	Reason          string
	Now             time.Time
}

// Request creates a pending approval for a gate stage or a dangerous action.
func (s *Store) Request(ctx context.Context, opts RequestOptions) (*domain.Approval, error) {
	if opts.Scope != ScopeStageGate && opts.Scope != ScopeAction {
		return nil, fmt.Errorf("%w: scope must be %s or %s", ErrInvalidInput, ScopeStageGate, ScopeAction)
	}
	if strings.TrimSpace(opts.WorkItemID) == "" || strings.TrimSpace(opts.RequestedBy) == "" {
		return nil, fmt.Errorf("%w: work item and requester are required", ErrInvalidInput)
	}
	if opts.Scope == ScopeStageGate {
		if !validStatus(opts.Stage) {
			return nil, fmt.Errorf("%w: stage_gate requires a known stage", ErrInvalidInput)
		}
		if opts.RequestedStatus != "" && !validStatus(opts.RequestedStatus) {
			return nil, fmt.Errorf("%w: unknown requested status %q", ErrInvalidInput, opts.RequestedStatus)
		}
	} else if opts.Stage != "" {
		return nil, fmt.Errorf("%w: action approvals carry no stage", ErrInvalidInput)
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	st, err := s.store()
	if err != nil {
		return nil, err
	}

	const maxAttempts = 5
	var created domain.Approval
	for range maxAttempts {
		err := st.Write(ctx, func(tx *storage.Tx) error {
			if opts.Scope == ScopeStageGate {
				dup, derr := findOpenApproval(tx, st, opts.WorkItemID, opts.Stage, opts.RequestedStatus)
				if derr != nil {
					return derr
				}
				if dup != "" {
					return fmt.Errorf("%w: %s is already open for stage %q; decide it or let the work item leave the status", ErrInvalidInput, dup, opts.Stage)
				}
			}
			next, err := nextNumber(st)
			if err != nil {
				return err
			}
			created = domain.Approval{
				SchemaVersion:   domain.SchemaVersion,
				ID:              fmt.Sprintf("approval-%d", next),
				ProjectID:       opts.ProjectID,
				Scope:           opts.Scope,
				Stage:           opts.Stage,
				RequestedStatus: opts.RequestedStatus,
				WorkItemID:      opts.WorkItemID,
				RequestedBy:     opts.RequestedBy,
				RequestedAt:     now.UTC(),
				Status:          StatusPending,
				CreatedAt:       now.UTC(),
				UpdatedAt:       now.UTC(),
			}
			if opts.RunID != "" {
				run := opts.RunID
				created.RunID = &run
			}
			if err := tx.PutYAML(approvalRel(created.ID), &created, storage.ExpectAbsent()); err != nil {
				return err
			}
			return events.AppendTx(tx, approvalEvent(&created, eventRequested, opts.RequestedBy, opts.Reason, now))
		})
		if err == nil {
			return &created, nil
		}
		if errors.Is(err, storage.ErrConflict) {
			continue
		}
		return nil, err
	}
	return nil, fmt.Errorf("%w: id allocation gave up after %d conflicts", ErrInvalidInput, maxAttempts)
}

// DecideOptions is the input to Decide.
type DecideOptions struct {
	Approve   bool
	DecidedBy string
	Comment   string
	Now       time.Time
}

// Decide approves or rejects a pending approval in its own transaction.
func (s *Store) Decide(ctx context.Context, id string, opts DecideOptions) (*domain.Approval, error) {
	st, err := s.store()
	if err != nil {
		return nil, err
	}
	var decided domain.Approval
	err = st.Write(ctx, func(tx *storage.Tx) error {
		a, ev, err := DecideTx(tx, id, opts)
		if err != nil {
			return err
		}
		decided = *a
		return events.AppendTx(tx, ev)
	})
	if err != nil {
		return nil, err
	}
	return &decided, nil
}

// DecideTx approves or rejects a pending approval inside an open transaction
// and returns the decision event for the caller to stage in the same batch.
// Invalidated approvals can never be decided into use.
func DecideTx(tx *storage.Tx, id string, opts DecideOptions) (*domain.Approval, *domain.Event, error) {
	if !ValidID(id) {
		return nil, nil, fmt.Errorf("%w: %q", ErrBadID, id)
	}
	if strings.TrimSpace(opts.DecidedBy) == "" {
		return nil, nil, fmt.Errorf("%w: decider is required", ErrInvalidInput)
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	raw, exists, err := tx.ReadForExpect(approvalRel(id))
	if err != nil {
		return nil, nil, err
	}
	if !exists {
		return nil, nil, ErrNotFound
	}
	var decided domain.Approval
	if err := domain.DecodeYAML(raw, &decided); err != nil {
		return nil, nil, err
	}
	switch {
	case decided.InvalidatedAt != nil:
		return nil, nil, fmt.Errorf("%w: %s was invalidated (the work item left %q)", ErrInvalidated, id, decided.RequestedStatus)
	case decided.Status != StatusPending:
		return nil, nil, fmt.Errorf("%w: %s is %s", ErrNotPending, id, decided.Status)
	}
	status := StatusRejected
	eventType := eventRejected
	if opts.Approve {
		status = StatusApproved
		eventType = eventApproved
	}
	decided.Status = status
	decided.DecidedBy = &opts.DecidedBy
	decidedAt := now.UTC()
	decided.DecidedAt = &decidedAt
	if opts.Comment != "" {
		comment := opts.Comment
		decided.Comment = &comment
	}
	decided.UpdatedAt = decidedAt
	if err := tx.PutYAML(approvalRel(id), &decided, storage.ExpectHash(storage.HashBytes(raw))); err != nil {
		return nil, nil, err
	}
	return &decided, approvalEvent(&decided, eventType, opts.DecidedBy, opts.Comment, now), nil
}

// ConsumeTx marks an approval consumed inside an open transaction and
// re-verifies every condition the gate relied on: the approval must be
// approved, unconsumed, for the same work item, for the same gate stage and
// still requested from the work item's current status. Any mismatch aborts
// the whole transaction (fail closed). The returned event is staged by the
// caller in the same batch as the status change, so a transaction keeps one
// append per JSONL shard.
func ConsumeTx(tx *storage.Tx, id, workItemID, stage, status string, now time.Time, actor, reason string) (*domain.Event, error) {
	if !ValidID(id) {
		return nil, fmt.Errorf("%w: %q", ErrBadID, id)
	}
	raw, exists, err := tx.ReadForExpect(approvalRel(id))
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	var a domain.Approval
	if err := domain.DecodeYAML(raw, &a); err != nil {
		return nil, err
	}
	switch {
	case a.Scope != ScopeStageGate:
		return nil, fmt.Errorf("%w: %s is a %s approval", ErrMismatch, id, a.Scope)
	case a.WorkItemID != workItemID:
		return nil, fmt.Errorf("%w: %s belongs to %s", ErrMismatch, id, a.WorkItemID)
	case a.Stage != stage:
		return nil, fmt.Errorf("%w: %s targets stage %q, not %q", ErrMismatch, id, a.Stage, stage)
	case a.InvalidatedAt != nil:
		return nil, fmt.Errorf("%w: %s was invalidated (the work item left %q)", ErrInvalidated, id, a.RequestedStatus)
	case a.Status != StatusApproved:
		return nil, fmt.Errorf("%w: %s is %s", ErrNotApproved, id, a.Status)
	case a.ConsumedAt != nil:
		return nil, ErrAlreadyConsumed
	case a.RequestedStatus != status:
		return nil, fmt.Errorf("%w: %s was requested from status %q; the work item left it", ErrMismatch, id, a.RequestedStatus)
	}
	consumedAt := now.UTC()
	a.ConsumedAt = &consumedAt
	a.UpdatedAt = consumedAt
	if err := tx.PutYAML(approvalRel(id), &a, storage.ExpectHash(storage.HashBytes(raw))); err != nil {
		return nil, err
	}
	return approvalEvent(&a, eventConsumed, actor, reason, now), nil
}

// Filter selects approvals for List; zero fields match everything.
type Filter struct {
	WorkItemID string
	Status     string
}

// List returns approvals ordered by numeric id.
func (s *Store) List(ctx context.Context, f Filter) ([]*domain.Approval, error) {
	st, err := s.store()
	if err != nil {
		return nil, err
	}
	var ids []string
	if err := st.Read(ctx, func(r *storage.Reader) error {
		var lerr error
		ids, lerr = listIDs(st)
		return lerr
	}); err != nil {
		return nil, err
	}
	out := make([]*domain.Approval, 0, len(ids))
	for _, id := range ids {
		a, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if f.WorkItemID != "" && a.WorkItemID != f.WorkItemID {
			continue
		}
		if f.Status != "" && a.Status != f.Status {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// Get decodes one approval.
func (s *Store) Get(ctx context.Context, id string) (*domain.Approval, error) {
	if !ValidID(id) {
		return nil, fmt.Errorf("%w: %q", ErrBadID, id)
	}
	st, err := s.store()
	if err != nil {
		return nil, err
	}
	var a domain.Approval
	err = st.Read(ctx, func(r *storage.Reader) error {
		data, exists, err := r.Read(approvalRel(id))
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		return domain.DecodeYAML(data, &a)
	})
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// MatchingGate returns the approved, unconsumed approval whose stage matches
// the gate being entered and whose request status matches the work item's
// current status (方案 §4.9 有效期).
func MatchingGate(ctx context.Context, root, workItemID, stage, status string) (*domain.Approval, error) {
	approvals, err := New(root).List(ctx, Filter{WorkItemID: workItemID, Status: StatusApproved})
	if err != nil {
		return nil, err
	}
	for _, a := range approvals {
		if a.ConsumedAt != nil || a.InvalidatedAt != nil || a.Scope != ScopeStageGate {
			continue
		}
		if a.Stage == stage && a.RequestedStatus == status {
			return a, nil
		}
	}
	return nil, ErrNotFound
}

// InvalidatedFor returns the most recently invalidated stage-gate approval
// requested for (workItemID, stage), or ErrNotFound. It explains a
// require_approval gate that an earlier approval used to satisfy: 方案 §4.9
// voids an approval the moment the work item leaves the status it was
// requested from, and the gate error should say so instead of only "an
// approved, unconsumed approval is required".
func InvalidatedFor(ctx context.Context, root, workItemID, stage string) (*domain.Approval, error) {
	approvals, err := New(root).List(ctx, Filter{WorkItemID: workItemID})
	if err != nil {
		return nil, err
	}
	var newest *domain.Approval
	for _, a := range approvals {
		if a.Scope != ScopeStageGate || a.Stage != stage || a.InvalidatedAt == nil {
			continue
		}
		if newest == nil || a.InvalidatedAt.After(*newest.InvalidatedAt) {
			newest = a
		}
	}
	if newest == nil {
		return nil, ErrNotFound
	}
	return newest, nil
}

func approvalEvent(a *domain.Approval, eventType, actor, content string, now time.Time) *domain.Event {
	ev := &domain.Event{
		SchemaVersion: domain.SchemaVersion,
		ProjectID:     a.ProjectID,
		Type:          eventType,
		Subject:       domain.Reference{Type: "approval", ID: a.ID},
		Time:          now.UTC(),
		Actor:         actor,
		Content:       content,
		Related:       []domain.Reference{{Type: "workitem", ID: a.WorkItemID}},
	}
	if a.RunID != nil && *a.RunID != "" {
		ev.Related = append(ev.Related, domain.Reference{Type: "run", ID: *a.RunID})
	}
	return ev
}

func nextNumber(st *storage.Store) (int, error) {
	ids, err := listIDs(st)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, id := range ids {
		n, err := strconv.Atoi(strings.TrimPrefix(id, "approval-"))
		if err != nil {
			return 0, fmt.Errorf("approval: bad id %q in approvals directory", id)
		}
		if n > max {
			max = n
		}
	}
	return max + 1, nil
}

// listIDs enumerates approvals/*.yaml with well-formed names, ordered by
// numeric suffix. A missing directory is an empty list.
func listIDs(st *storage.Store) ([]string, error) {
	return listIDsIn(filepath.Join(st.DevsysDir(), dirRel))
}

func listIDsIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var ids []string
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".yaml")
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") || !ValidID(name) {
			continue
		}
		ids = append(ids, name)
	}
	sort.Slice(ids, func(i, j int) bool {
		ni, _ := strconv.Atoi(strings.TrimPrefix(ids[i], "approval-"))
		nj, _ := strconv.Atoi(strings.TrimPrefix(ids[j], "approval-"))
		return ni < nj
	})
	return ids, nil
}

// findOpenApproval returns the id of an unconsumed, non-invalidated approval
// already open for the same (work item, stage, request status). Duplicate
// requests would otherwise linger forever because only the first can ever be
// consumed.
func findOpenApproval(tx *storage.Tx, st *storage.Store, workItemID, stage, requestedStatus string) (string, error) {
	ids, err := listIDs(st)
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		raw, exists, err := tx.ReadForExpect(approvalRel(id))
		if err != nil {
			return "", err
		}
		if !exists {
			continue
		}
		var a domain.Approval
		if err := domain.DecodeYAML(raw, &a); err != nil {
			return "", err
		}
		if a.Scope != ScopeStageGate || a.WorkItemID != workItemID || a.Stage != stage || a.RequestedStatus != requestedStatus {
			continue
		}
		if a.ConsumedAt != nil || a.InvalidatedAt != nil || a.Status == StatusRejected {
			continue
		}
		return id, nil
	}
	return "", nil
}

// InvalidateForStatusTx marks every unconsumed approval requested from a work
// item status as invalidated once the work item leaves that status
// (方案 §4.9 离开阶段即作废): even a later return to the same status needs a
// fresh request. The returned events join the status-change batch so the
// invalidation is atomic with the transition that caused it.
func InvalidateForStatusTx(tx *storage.Tx, devsysDir, workItemID, status string, now time.Time, reason string) ([]*domain.Event, error) {
	if workItemID == "" || status == "" {
		return nil, nil
	}
	ids, err := listIDsIn(filepath.Join(devsysDir, dirRel))
	if err != nil {
		return nil, err
	}
	var out []*domain.Event
	for _, id := range ids {
		if tx.Staged(approvalRel(id)) {
			continue // already rewritten in this transaction (e.g. consumed by the gate)
		}
		raw, exists, err := tx.ReadForExpect(approvalRel(id))
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		var a domain.Approval
		if err := domain.DecodeYAML(raw, &a); err != nil {
			return nil, err
		}
		if a.Scope != ScopeStageGate || a.WorkItemID != workItemID || a.RequestedStatus != status {
			continue
		}
		if a.ConsumedAt != nil || a.InvalidatedAt != nil || a.Status == StatusRejected {
			continue
		}
		invalidatedAt := now.UTC()
		a.InvalidatedAt = &invalidatedAt
		a.UpdatedAt = invalidatedAt
		if err := tx.PutYAML(approvalRel(id), &a, storage.ExpectHash(storage.HashBytes(raw))); err != nil {
			return nil, err
		}
		content := fmt.Sprintf("invalidated: work item left status %q", status)
		if reason != "" {
			content += ": " + reason
		}
		out = append(out, approvalEvent(&a, eventInvalidated, "system", content, now))
	}
	return out, nil
}
