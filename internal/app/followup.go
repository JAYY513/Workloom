package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/events"
)

var dynamicFollowUpTypes = map[string]bool{
	"research":  true,
	"decision":  true,
	"feature":   true,
	"bug":       true,
	"follow-up": true,
}

// CreateFollowUpRequest describes a controlled task proposed from an existing
// work item. It deliberately has no workflow YAML or executable step payload.
type CreateFollowUpRequest struct {
	ParentID           string
	Type               string
	Title              string
	Description        string
	AcceptanceCriteria []string
	Actor              string
	Reason             string
}

// CreateFollowUpWorkitem creates an auditable, controlled follow-up task.
// The new item starts as draft and never mutates the parent or its active run.
func (s *Service) CreateFollowUpWorkitem(ctx context.Context, req CreateFollowUpRequest) (WorkItemView, error) {
	parentID := strings.TrimSpace(req.ParentID)
	kind := strings.ToLower(strings.TrimSpace(req.Type))
	if parentID == "" || req.Actor == "" || req.Reason == "" {
		return WorkItemView{}, Usagef("follow-up requires parent, actor and reason")
	}
	if strings.TrimSpace(req.Title) == "" {
		return WorkItemView{}, Usagef("follow-up requires title")
	}
	if !dynamicFollowUpTypes[kind] {
		return WorkItemView{}, Usagef("unsupported follow-up type %q (allowed: research, decision, feature, bug, follow-up)", req.Type)
	}
	parent, err := s.WorkitemGet(ctx, parentID)
	if err != nil {
		return WorkItemView{}, err
	}
	parentRef := parentID
	item := &domain.WorkItem{
		ProjectID:          parent.Item.ProjectID,
		ParentID:           &parentRef,
		Type:               kind,
		Title:              strings.TrimSpace(req.Title),
		Description:        req.Description,
		Status:             domain.StatusDraft,
		AcceptanceCriteria: req.AcceptanceCriteria,
		CreatedFrom:        &domain.Reference{Type: "workitem", ID: parentID},
		Reason:             req.Reason,
		ProposedBy:         req.Actor,
		ApprovalRequired:   kind == "decision",
		SchedulingState:    "pending",
	}
	id, err := s.items().Create(ctx, item, "WLM")
	if err != nil {
		return WorkItemView{}, s.storeError(err)
	}
	ev := &domain.Event{
		Type:      "follow_up_workitem_created",
		Subject:   domain.Reference{Type: "workitem", ID: id},
		ProjectID: item.ProjectID,
		Actor:     req.Actor,
		Content:   fmt.Sprintf("from %s: %s", parentID, req.Reason),
		Time:      s.now(),
	}
	if err := events.New(s.Root).Append(ctx, ev); err != nil {
		return WorkItemView{}, s.storeError(err)
	}
	return s.WorkitemGet(ctx, id)
}
