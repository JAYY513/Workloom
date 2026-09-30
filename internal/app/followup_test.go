package app

import (
	"context"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/events"
	"github.com/JAYY513/Workloom/internal/project"
)

func followUpFixture(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	if _, err := project.Init(root, project.Options{Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	svc := New(root)
	parent, err := svc.WorkitemCreate(context.Background(), CreateWorkitemRequest{
		Title: "主任务", Type: "feature", Actor: "operator", Reason: "create parent",
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, parent.Item.ID
}

func TestCreateFollowUpWorkitemRecordsProvenance(t *testing.T) {
	svc, parentID := followUpFixture(t)
	view, err := svc.CreateFollowUpWorkitem(context.Background(), CreateFollowUpRequest{
		ParentID: parentID, Type: "research", Title: "调查现有边界", Actor: "agent", Reason: "缺少架构信息",
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Item.ParentID == nil || *view.Item.ParentID != parentID {
		t.Fatalf("parent_id = %v, want %s", view.Item.ParentID, parentID)
	}
	if view.Item.CreatedFrom == nil || view.Item.CreatedFrom.Type != "workitem" || view.Item.CreatedFrom.ID != parentID {
		t.Fatalf("created_from = %+v", view.Item.CreatedFrom)
	}
	if view.Item.ProposedBy != "agent" || view.Item.Reason != "缺少架构信息" || view.Item.Status != domain.StatusDraft {
		t.Fatalf("provenance/status = %+v", view.Item)
	}
	records, err := events.New(svc.Root).Read(context.Background(), events.Filter{Type: "follow_up_workitem_created"})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Subject.ID != view.Item.ID {
		t.Fatalf("events = %+v", records)
	}
}

func TestCreateFollowUpDecisionRequiresApproval(t *testing.T) {
	svc, parentID := followUpFixture(t)
	view, err := svc.CreateFollowUpWorkitem(context.Background(), CreateFollowUpRequest{
		ParentID: parentID, Type: "decision", Title: "确认边界", Actor: "agent", Reason: "架构选择需要确认",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !view.Item.ApprovalRequired {
		t.Fatal("decision follow-up is not marked approval_required")
	}
}

func TestCreateFollowUpRejectsUnknownType(t *testing.T) {
	svc, parentID := followUpFixture(t)
	if _, err := svc.CreateFollowUpWorkitem(context.Background(), CreateFollowUpRequest{
		ParentID: parentID, Type: "arbitrary-yaml", Title: "不应创建", Actor: "agent", Reason: "test",
	}); err == nil {
		t.Fatal("unknown follow-up type accepted")
	}
}
