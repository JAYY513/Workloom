package workitem

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"workloom/internal/domain"
	"workloom/internal/events"
	"workloom/internal/storage"
)

// TransitionRequest carries mutation authority; repair additionally requires a
// confirmed evidence digest and a guard revalidated inside the same transaction.
type TransitionRequest struct {
	TargetStatus   string
	Actor          string
	Reason         string
	RunID          string
	Token          string
	PreviousStatus string
	Confirmation   []byte
	AllowRepair    bool
	Now            time.Time
	Guard          func(*storage.Tx) error
}

func (r TransitionRequest) valid() error {
	if strings.TrimSpace(r.TargetStatus) == "" || strings.TrimSpace(r.Actor) == "" || strings.TrimSpace(r.Reason) == "" {
		return fmt.Errorf("%w: target, actor and reason required", ErrInvalidInput)
	}
	return nil
}

func newTransitionEvent(now time.Time, projectID, id, from, to string, req TransitionRequest) *domain.Event {
	ev := &domain.Event{SchemaVersion: domain.SchemaVersion, ProjectID: projectID, Type: "status_changed", Subject: domain.Reference{Type: "workitem", ID: id}, Time: now.UTC(), Actor: req.Actor, Content: fmt.Sprintf("%s -> %s: %s", from, to, req.Reason), Related: []domain.Reference{{Type: "workitem_status", ID: from + "->" + to}}}
	if req.RunID != "" {
		ev.Related = append(ev.Related, domain.Reference{Type: "run", ID: req.RunID})
	}
	return ev
}

func (s *Store) Transition(ctx context.Context, id string, req TransitionRequest, expected []byte) (*domain.WorkItem, error) {
	if req.AllowRepair {
		return nil, fmt.Errorf("%w: use confirmed repair", ErrInvalidInput)
	}
	return s.changeStatus(ctx, id, req, expected, false)
}

func (s *Store) ApplyRepair(ctx context.Context, id string, req TransitionRequest, expected []byte) error {
	if !req.AllowRepair || len(req.Confirmation) == 0 || req.Guard == nil {
		return fmt.Errorf("%w: confirmed evidence and guard required", ErrInvalidInput)
	}
	_, err := s.changeStatus(ctx, id, req, expected, true)
	return err
}

func (s *Store) changeStatus(ctx context.Context, id string, req TransitionRequest, expected []byte, repair bool) (*domain.WorkItem, error) {
	if err := req.valid(); err != nil {
		return nil, err
	}
	if !ValidID(id) || len(expected) == 0 {
		return nil, fmt.Errorf("%w: valid id and expected snapshot required", ErrInvalidInput)
	}
	st, err := s.store()
	if err != nil {
		return nil, err
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var wi domain.WorkItem
	err = st.Write(ctx, func(tx *storage.Tx) error {
		raw, exists, err := tx.ReadForExpect(workitemRel(id))
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		if !bytes.Equal(raw, expected) {
			return fmt.Errorf("%w: %s", storage.ErrConflict, id)
		}
		if err := domain.DecodeYAML(raw, &wi); err != nil {
			return err
		}
		allowed := domain.AllowedFrom(wi.Status)
		if wi.Status == domain.StatusBlocked && wi.PreviousStatus != "" {
			allowed = []string{wi.PreviousStatus, domain.StatusCancelled}
		}
		legal := legalTransition(wi.Status, req.TargetStatus, wi.PreviousStatus)
		if repair {
			legal = legalRepairTransition(wi.Status, req.TargetStatus)
		}
		if !legal {
			return &domain.TransitionError{From: wi.Status, To: req.TargetStatus, Allowed: allowed, Reason: "target not allowed; regression requires confirmed repair"}
		}
		leaseRaw, leased, err := tx.ReadForExpect(leaseRel(id))
		if err != nil {
			return err
		}
		if leased {
			var lease domain.SchedulingLease
			if err := domain.DecodeYAML(leaseRaw, &lease); err != nil {
				return err
			}
			if repair {
				return fmt.Errorf("%w: release claim before repairing status", ErrAlreadyClaimed)
			}
			if req.Token == "" || req.Token != lease.Token || req.Actor != lease.Owner || (req.RunID != "" && req.RunID != lease.RunID) {
				return ErrLeaseTokenMismatch
			}
			if !now.Before(lease.LeaseUntil) {
				return ErrLeaseExpired
			}
		} else if req.Token != "" {
			return ErrLeaseTokenMismatch
		}
		if repair {
			if err := req.Guard(tx); err != nil {
				return err
			}
		}
		from := wi.Status
		wi.Status = req.TargetStatus
		wi.UpdatedAt = now
		if req.TargetStatus == domain.StatusBlocked {
			wi.PreviousStatus = from
		} else if from == domain.StatusBlocked {
			wi.PreviousStatus = ""
		}
		// An active lease must be released separately before leaving execution.
		// This prevents business status from contradicting the retained claim.
		if leased && req.TargetStatus != domain.StatusInProgress {
			return fmt.Errorf("%w: release claim before advancing status", ErrAlreadyClaimed)
		}
		switch req.TargetStatus {
		case domain.StatusDraft, domain.StatusBacklog, domain.StatusReady, domain.StatusBlocked:
			wi.SchedulingState = domain.SchedulingUnclaimed
		case domain.StatusReview, domain.StatusVerification, domain.StatusDone, domain.StatusCancelled:
			wi.SchedulingState = domain.SchedulingReleased
		}
		ev := newTransitionEvent(now, wi.ProjectID, id, from, wi.Status, req)
		if repair {
			ev.Type = "repair_applied"
			ev.Content += " confirmation=" + string(req.Confirmation)
			ev.Related = append(ev.Related, domain.Reference{Type: "repair_digest", ID: string(req.Confirmation)})
		}
		if err := tx.PutYAML(workitemRel(id), &wi, storage.ExpectHash(storage.HashBytes(raw))); err != nil {
			return err
		}
		return events.AppendTx(tx, ev)
	})
	if err != nil {
		return nil, err
	}
	return &wi, nil
}

func legalTransition(from, to, previous string) bool {
	if from == to {
		return false
	}
	if from == domain.StatusBlocked {
		return to == domain.StatusCancelled || previous != "" && to == previous
	}
	return domain.IsTransitionLegal(from, to)
}
func legalRepairTransition(from, to string) bool {
	if from == to {
		return false
	}
	switch from {
	case domain.StatusDone:
		return to == domain.StatusInProgress || to == domain.StatusReview || to == domain.StatusVerification || to == domain.StatusBlocked
	case domain.StatusCancelled:
		return to == domain.StatusDraft || to == domain.StatusBacklog || to == domain.StatusReady || to == domain.StatusBlocked
	}
	return legalTransition(from, to, "")
}

// Shared optimistic byte comparison for claim operations.
func bytesEqual(a, b []byte) bool { return bytes.Equal(a, b) }

var ErrInvalidInput = errors.New("workitem: invalid input")
var ErrLeaseTokenMismatch = errors.New("workitem: lease token mismatch")
var ErrAlreadyClaimed = errors.New("workitem: already claimed")
var ErrNotClaimed = errors.New("workitem: no active claim")
var ErrLeaseExpired = errors.New("workitem: lease expired")

func workitemRel(id string) string { return dirRel + "/" + id + ".yaml" }
