package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/project"
	"github.com/JAYY513/Workloom/internal/workitem"
)

func recommendationFixture(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	if _, err := project.Init(root, project.Options{Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	policy := "---\nid: quick-fix\nname: Quick fix\nversion: 1\nsteps:\n  - id: implement\n    type: execute\n    required: true\n---\nImplement {{workitem.title}}\n"
	if err := os.WriteFile(filepath.Join(root, ".devsys", "workflows", "quick-fix.md"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	config := "schema_version: 1\ndefault_policy: quick-fix\n"
	if err := os.WriteFile(filepath.Join(root, ".devsys", "config.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	item := &domain.WorkItem{ProjectID: "demo", Type: "feature", Title: "桌面工作台", Description: "交付一个可用切片", Status: domain.StatusReady, AcceptanceCriteria: []string{"刷新后数据保留"}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	id, err := workitem.New(root).Create(context.Background(), item, "WLM")
	if err != nil {
		t.Fatal(err)
	}
	return New(root), id
}

func TestWorkflowRecommendationReportsFeatureMismatch(t *testing.T) {
	svc, id := recommendationFixture(t)
	view, err := svc.WorkflowRecommendation(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Recommendation.PolicyID != "feature-development" || !view.Mismatch || view.SelectedPolicy != "quick-fix" {
		t.Fatalf("recommendation = %+v", view)
	}
}

func TestClaimRejectsHighConfidencePolicyMismatch(t *testing.T) {
	svc, id := recommendationFixture(t)
	_, err := svc.WorkitemClaim(context.Background(), id, "agent", "start feature", "")
	if err == nil || !strings.Contains(err.Error(), "recommended \"feature-development\"") || !strings.Contains(err.Error(), "workflow start") {
		t.Fatalf("claim error = %v", err)
	}
}
