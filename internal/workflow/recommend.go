package workflow

import (
	"strings"

	"github.com/JAYY513/Workloom/internal/domain"
)

const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"

	RecommendationSourceExplicit = "explicit"
	RecommendationSourceRule     = "rule"
	RecommendationSourceDefault  = "default"
)

// Recommendation is the deterministic policy choice for a work item.
type Recommendation struct {
	PolicyID       string   `json:"policy_id"`
	Confidence     string   `json:"confidence"`
	Signals        []string `json:"signals,omitempty"`
	Reasons        []string `json:"reasons,omitempty"`
	Source         string   `json:"source"`
	RequiresTriage bool     `json:"requires_triage,omitempty"`
	LegacyDefault  bool     `json:"legacy_default,omitempty"`
}

// Recommend chooses a policy without invoking a model or generating policy data.
// An explicit instance is returned unchanged; otherwise strong task signals win
// over the project default, which is only a fallback for unknown work.
func Recommend(wi *domain.WorkItem, defaultPolicy string) Recommendation {
	if wi == nil {
		return Recommendation{PolicyID: strings.TrimSpace(defaultPolicy), Confidence: ConfidenceLow, Source: RecommendationSourceDefault}
	}
	if wi.Workflow != nil && strings.TrimSpace(wi.Workflow.ID) != "" {
		return Recommendation{PolicyID: strings.TrimSpace(wi.Workflow.ID), Confidence: ConfidenceHigh, Source: RecommendationSourceExplicit, Reasons: []string{"work item already declares a workflow instance"}}
	}

	kind := strings.ToLower(strings.TrimSpace(wi.Type))
	text := strings.ToLower(strings.Join(append([]string{wi.Title, wi.Description}, wi.AcceptanceCriteria...), " "))
	if architectureSignal(kind, text) {
		return Recommendation{
			PolicyID: "architecture-change", Confidence: ConfidenceHigh, Source: RecommendationSourceRule,
			Signals: []string{"architecture_or_boundary_change"},
			Reasons: []string{"the work item changes an architecture, protocol, data model, or module boundary"},
		}
	}
	if kind == "feature" {
		return Recommendation{
			PolicyID: "feature-development", Confidence: ConfidenceHigh, Source: RecommendationSourceRule,
			Signals: []string{"workitem.type=feature"},
			Reasons: []string{"feature work requires planning before implementation"},
		}
	}
	if quickFixType(kind) {
		return Recommendation{
			PolicyID: "quick-fix", Confidence: ConfidenceHigh, Source: RecommendationSourceRule,
			Signals: []string{"workitem.type=" + kind},
			Reasons: []string{"small corrective or maintenance work fits the quick-fix flow"},
		}
	}
	legacy := strings.TrimSpace(defaultPolicy) == "quick-fix"
	reasons := []string{"work item has no high-confidence workflow signal; classify it before execution"}
	if legacy {
		reasons = append(reasons, "project default_policy=quick-fix is a legacy execution fallback; migrate to intake")
	}
	return Recommendation{PolicyID: "intake", Confidence: ConfidenceLow, Source: RecommendationSourceDefault, Reasons: reasons, RequiresTriage: true, LegacyDefault: legacy}
}

func architectureSignal(kind, text string) bool {
	for _, value := range []string{"architecture", "architectural", "protocol", "data model", "database schema", "migration", "module boundary", "cross-module", "api contract"} {
		if strings.Contains(text, value) {
			return true
		}
	}
	return kind == "architecture" || kind == "architectural-change" || kind == "protocol" || kind == "data-model"
}

func quickFixType(kind string) bool {
	switch kind {
	case "bug", "chore", "docs", "documentation", "config", "configuration", "maintenance":
		return true
	default:
		return false
	}
}
