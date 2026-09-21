package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/events"
	"github.com/JAYY513/Workloom/internal/record"
)

// RecordView is one record plus the version hash the surfaces report and the
// update guards consume.
type RecordView struct {
	Decision *domain.Decision `json:"decision,omitempty"`
	Finding  *domain.Finding  `json:"finding,omitempty"`
	Artifact *domain.Artifact `json:"artifact,omitempty"`
	Version  string           `json:"version,omitempty"`
}

// --- decisions -----------------------------------------------------------

// DecisionList lists every decision in ascending ID order.
func (s *Service) DecisionList(ctx context.Context) ([]*domain.Decision, error) {
	items, err := record.New(s.Root).ListDecisions(ctx)
	if err != nil {
		return nil, s.storeError(err)
	}
	if items == nil {
		items = []*domain.Decision{}
	}
	return items, nil
}

// DecisionGet reads one decision with its version hash.
func (s *Service) DecisionGet(ctx context.Context, id string) (RecordView, error) {
	d, raw, err := record.New(s.Root).ReadDecision(ctx, id)
	if err != nil {
		return RecordView{}, s.storeError(err)
	}
	return RecordView{Decision: d, Version: versionHash(raw)}, nil
}

// CreateDecisionRequest is the input to DecisionCreate.
type CreateDecisionRequest struct {
	Title            string
	Context          string
	Decision         string
	Reasoning        string
	Options          []string
	Consequences     []string
	RelatedWorkItems []string
	CreatedBy        string
}

// DecisionCreate stores a decision (一记录一文件, ID assigned by the store).
func (s *Service) DecisionCreate(ctx context.Context, req CreateDecisionRequest) (RecordView, error) {
	if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Decision) == "" || req.CreatedBy == "" {
		return RecordView{}, Usagef("decision create requires title, decision and created_by")
	}
	md, err := s.project()
	if err != nil {
		return RecordView{}, err
	}
	d := &domain.Decision{
		ProjectID:        md.Project.ID,
		Title:            req.Title,
		Context:          req.Context,
		Options:          req.Options,
		Decision:         req.Decision,
		Reasoning:        req.Reasoning,
		Consequences:     req.Consequences,
		Status:           "proposed",
		CreatedBy:        req.CreatedBy,
		RelatedWorkItems: req.RelatedWorkItems,
		CreatedAt:        s.now(),
	}
	id, err := record.New(s.Root).CreateDecision(ctx, d)
	if err != nil {
		return RecordView{}, s.storeError(err)
	}
	return s.DecisionGet(ctx, id)
}

// DecisionApprove marks a decision approved by the given decider, under the
// version guard.
func (s *Service) DecisionApprove(ctx context.Context, id, by, expect string) (RecordView, error) {
	if by == "" {
		return RecordView{}, Usagef("decision approve requires by")
	}
	store := record.New(s.Root)
	d, raw, err := store.ReadDecision(ctx, id)
	if err != nil {
		return RecordView{}, s.storeError(err)
	}
	if err := checkExpect(expect, raw, "decision"); err != nil {
		return RecordView{}, err
	}
	if d.Status == "approved" {
		return RecordView{}, Invalidf(KindInvalid, nil, "decision %s is already approved", id)
	}
	d.Status = "approved"
	d.ApprovedBy = by
	if err := store.UpdateDecision(ctx, d, raw); err != nil {
		return RecordView{}, s.storeError(err)
	}
	return s.DecisionGet(ctx, id)
}

// --- findings ------------------------------------------------------------

// FindingList lists every finding in ascending ID order.
func (s *Service) FindingList(ctx context.Context) ([]*domain.Finding, error) {
	items, err := record.New(s.Root).ListFindings(ctx)
	if err != nil {
		return nil, s.storeError(err)
	}
	if items == nil {
		items = []*domain.Finding{}
	}
	return items, nil
}

// FindingGet reads one finding with its version hash.
func (s *Service) FindingGet(ctx context.Context, id string) (RecordView, error) {
	f, raw, err := record.New(s.Root).ReadFinding(ctx, id)
	if err != nil {
		return RecordView{}, s.storeError(err)
	}
	return RecordView{Finding: f, Version: versionHash(raw)}, nil
}

// CreateFindingRequest is the input to FindingCreate.
type CreateFindingRequest struct {
	Type               string
	Title              string
	Description        string
	Evidence           []string
	Severity           string
	RelatedWorkItems   []string
	RecommendedActions []string
	DiscoveredByRunID  string
}

// FindingCreate stores a finding.
func (s *Service) FindingCreate(ctx context.Context, req CreateFindingRequest) (RecordView, error) {
	if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Description) == "" {
		return RecordView{}, Usagef("finding create requires title and description")
	}
	md, err := s.project()
	if err != nil {
		return RecordView{}, err
	}
	kind := req.Type
	if kind == "" {
		kind = "issue"
	}
	f := &domain.Finding{
		ProjectID:          md.Project.ID,
		Type:               kind,
		Title:              req.Title,
		Description:        req.Description,
		Evidence:           req.Evidence,
		Severity:           req.Severity,
		Status:             "open",
		DiscoveredByRunID:  req.DiscoveredByRunID,
		RelatedWorkItems:   req.RelatedWorkItems,
		RecommendedActions: req.RecommendedActions,
	}
	id, err := record.New(s.Root).CreateFinding(ctx, f)
	if err != nil {
		return RecordView{}, s.storeError(err)
	}
	return s.FindingGet(ctx, id)
}

// FindingResolve closes a finding under the version guard.
func (s *Service) FindingResolve(ctx context.Context, id, resolution, expect string) (RecordView, error) {
	store := record.New(s.Root)
	f, raw, err := store.ReadFinding(ctx, id)
	if err != nil {
		return RecordView{}, s.storeError(err)
	}
	if err := checkExpect(expect, raw, "finding"); err != nil {
		return RecordView{}, err
	}
	if f.Status == "resolved" {
		return RecordView{}, Invalidf(KindInvalid, nil, "finding %s is already resolved", id)
	}
	f.Status = "resolved"
	if strings.TrimSpace(resolution) != "" {
		f.RecommendedActions = append(f.RecommendedActions, "resolution: "+resolution)
	}
	if err := store.UpdateFinding(ctx, f, raw); err != nil {
		return RecordView{}, s.storeError(err)
	}
	return s.FindingGet(ctx, id)
}

// --- artifacts -----------------------------------------------------------

// ArtifactList lists every artifact in ascending ID order.
func (s *Service) ArtifactList(ctx context.Context) ([]*domain.Artifact, error) {
	items, err := record.New(s.Root).ListArtifacts(ctx)
	if err != nil {
		return nil, s.storeError(err)
	}
	if items == nil {
		items = []*domain.Artifact{}
	}
	return items, nil
}

// ArtifactGet reads one artifact with its version hash.
func (s *Service) ArtifactGet(ctx context.Context, id string) (RecordView, error) {
	a, raw, err := record.New(s.Root).ReadArtifact(ctx, id)
	if err != nil {
		return RecordView{}, s.storeError(err)
	}
	return RecordView{Artifact: a, Version: versionHash(raw)}, nil
}

// ArtifactHistory walks the previous_id chain (newest first).
func (s *Service) ArtifactHistory(ctx context.Context, id string) ([]*domain.Artifact, error) {
	items, err := record.New(s.Root).ArtifactHistory(ctx, id)
	if err != nil {
		return nil, s.storeError(err)
	}
	return items, nil
}

// RegisterArtifactRequest is the input to ArtifactRegister.
type RegisterArtifactRequest struct {
	Type             string
	Name             string
	Path             string
	Source           string
	CreatedByRunID   string
	Status           string
	RelatedWorkItems []string
	// Actor and Reason are the mandatory audit trail for the write.
	Actor  string
	Reason string
}

// ArtifactRegister stores a new artifact record (version 1).
func (s *Service) ArtifactRegister(ctx context.Context, req RegisterArtifactRequest) (RecordView, error) {
	if strings.TrimSpace(req.Name) == "" {
		return RecordView{}, Usagef("artifact register requires a name")
	}
	if strings.TrimSpace(req.Actor) == "" || strings.TrimSpace(req.Reason) == "" {
		return RecordView{}, Usagef("artifact register requires --actor and --reason (audit trail)")
	}
	md, err := s.project()
	if err != nil {
		return RecordView{}, err
	}
	kind := req.Type
	if kind == "" {
		kind = "document"
	}
	status := req.Status
	if status == "" {
		status = "draft"
	}
	now := s.now()
	a := &domain.Artifact{
		ProjectID:        md.Project.ID,
		Type:             kind,
		Name:             req.Name,
		Path:             req.Path,
		Source:           req.Source,
		CreatedByRunID:   req.CreatedByRunID,
		Status:           status,
		Version:          1,
		RelatedWorkItems: req.RelatedWorkItems,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	id, err := record.New(s.Root).CreateArtifact(ctx, a)
	if err != nil {
		return RecordView{}, s.storeError(err)
	}
	ev := &domain.Event{
		Type:      "artifact_registered",
		Subject:   domain.Reference{Type: "artifact", ID: id},
		ProjectID: md.Project.ID,
		Actor:     req.Actor,
		Content:   fmt.Sprintf("artifact registered: %s — %s", req.Name, req.Reason),
		Time:      s.now(),
	}
	if err := events.New(s.Root).Append(ctx, ev); err != nil {
		return RecordView{}, s.storeError(err)
	}
	return s.ArtifactGet(ctx, id)
}

// UpdateArtifactRequest appends a new artifact version.
type UpdateArtifactRequest struct {
	ID               string
	Expect           string
	Status           string
	Path             string
	Source           string
	RelatedWorkItems []string
}

// ArtifactUpdate appends a new version linked to the previous one (records
// are immutable: the old file is never rewritten).
func (s *Service) ArtifactUpdate(ctx context.Context, req UpdateArtifactRequest) (RecordView, error) {
	store := record.New(s.Root)
	current, raw, err := store.ReadArtifact(ctx, req.ID)
	if err != nil {
		return RecordView{}, s.storeError(err)
	}
	if err := checkExpect(req.Expect, raw, "artifact"); err != nil {
		return RecordView{}, err
	}
	// The new version is built inside the write transaction (the store copies
	// the previous version and links it), so the patch cannot race a
	// concurrent append.
	now := s.now()
	id, err := store.AppendArtifactVersion(ctx, current.ID, raw, func(next *domain.Artifact) {
		if req.Status != "" {
			next.Status = req.Status
		}
		if req.Path != "" {
			next.Path = req.Path
		}
		if req.Source != "" {
			next.Source = req.Source
		}
		if req.RelatedWorkItems != nil {
			next.RelatedWorkItems = req.RelatedWorkItems
		}
		next.UpdatedAt = now
	})
	if err != nil {
		return RecordView{}, s.storeError(err)
	}
	return s.ArtifactGet(ctx, id)
}

// --- events --------------------------------------------------------------

// EventListRequest filters the event stream.
type EventListRequest struct {
	SubjectType string
	SubjectID   string
	Type        string
	Since       time.Time
	Until       time.Time
	Limit       int
}

// EventList reads events (ascending by time; Limit keeps the newest N).
func (s *Service) EventList(ctx context.Context, req EventListRequest) ([]*domain.Event, error) {
	if req.Limit < 0 {
		return nil, Usagef("limit must not be negative (got %d)", req.Limit)
	}
	if req.SubjectID != "" && req.SubjectType == "" {
		return nil, Usagef("subject_type is required when subject_id is set")
	}
	filter := events.Filter{Type: req.Type}
	if req.SubjectID != "" {
		filter.Subject = &domain.Reference{Type: req.SubjectType, ID: req.SubjectID}
	}
	if !req.Since.IsZero() {
		filter.From = req.Since
	}
	if !req.Until.IsZero() {
		filter.To = req.Until
	}
	items, err := events.New(s.Root).Read(ctx, filter)
	if err != nil {
		return nil, s.storeError(err)
	}
	if req.Limit > 0 && len(items) > req.Limit {
		items = items[len(items)-req.Limit:]
	}
	if items == nil {
		items = []*domain.Event{}
	}
	return items, nil
}

// RecordEventRequest is the input to EventRecord.
type RecordEventRequest struct {
	Type    string
	Subject domain.Reference
	Actor   string
	Content string
	Related []domain.Reference
	ReplyTo string
}

// EventRecord appends one event (append-only; no version guard).
func (s *Service) EventRecord(ctx context.Context, req RecordEventRequest) (*domain.Event, error) {
	if req.Type == "" || req.Subject.Type == "" || req.Subject.ID == "" || req.Actor == "" {
		return nil, Usagef("event record requires type, subject and actor")
	}
	for i, ref := range req.Related {
		if ref.Type == "" || ref.ID == "" {
			return nil, Usagef("related[%d] must carry both type and id", i)
		}
	}
	ev := &domain.Event{
		Type:    req.Type,
		Subject: req.Subject,
		Actor:   req.Actor,
		Content: req.Content,
		Related: req.Related,
		Time:    s.now(),
	}
	if req.ReplyTo != "" {
		reply := req.ReplyTo
		ev.ReplyTo = &reply
	}
	if err := events.New(s.Root).Append(ctx, ev); err != nil {
		return nil, s.storeError(err)
	}
	return ev, nil
}
