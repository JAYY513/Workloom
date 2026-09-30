package workflow

import (
	"testing"

	"github.com/JAYY513/Workloom/internal/domain"
)

func TestRecommendFeatureOverridesDefault(t *testing.T) {
	r := Recommend(&domain.WorkItem{Type: "feature", Title: "桌面工作台", AcceptanceCriteria: []string{"刷新后数据保留"}}, "quick-fix")
	if r.PolicyID != "feature-development" || r.Confidence != ConfidenceHigh || r.Source != RecommendationSourceRule {
		t.Fatalf("recommendation = %+v", r)
	}
}

func TestRecommendArchitectureSignalWins(t *testing.T) {
	r := Recommend(&domain.WorkItem{Type: "feature", Description: "修改 API contract 和跨模块边界"}, "quick-fix")
	if r.PolicyID != "architecture-change" || r.Confidence != ConfidenceHigh {
		t.Fatalf("recommendation = %+v", r)
	}
}

func TestRecommendQuickFixTypes(t *testing.T) {
	for _, kind := range []string{"bug", "docs", "config", "chore"} {
		r := Recommend(&domain.WorkItem{Type: kind}, "feature-development")
		if r.PolicyID != "quick-fix" || r.Confidence != ConfidenceHigh {
			t.Errorf("type %q: recommendation = %+v", kind, r)
		}
	}
}

func TestRecommendUnknownUsesIntake(t *testing.T) {
	r := Recommend(&domain.WorkItem{Type: "research"}, "quick-fix")
	if r.PolicyID != "intake" || r.Confidence != ConfidenceLow || r.Source != RecommendationSourceDefault || !r.RequiresTriage || !r.LegacyDefault {
		t.Fatalf("recommendation = %+v", r)
	}
}

func TestRecommendExplicitInstanceWins(t *testing.T) {
	r := Recommend(&domain.WorkItem{Type: "feature", Workflow: &domain.WorkflowInstance{ID: "quick-fix"}}, "feature-development")
	if r.PolicyID != "quick-fix" || r.Source != RecommendationSourceExplicit {
		t.Fatalf("recommendation = %+v", r)
	}
}
