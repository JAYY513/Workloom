package next

import (
	"strings"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
)

func TestEvaluateDraftWorkitemIsConcernAndPromoteRecommendation(t *testing.T) {
	rep := Evaluate(Input{
		Now:          time.Now().UTC(),
		InspectionOK: true,
		HasBlueprint: true,
		WorkItems:    []*domain.WorkItem{{ID: "WLM-draft", Status: domain.StatusDraft}},
	})
	if rep.Verdict != VerdictConcerns {
		t.Fatalf("verdict = %s, want %s", rep.Verdict, VerdictConcerns)
	}
	if len(rep.Risks) != 1 || rep.Risks[0].Kind != RiskDraftWorkitem {
		t.Fatalf("risks = %+v, want one draft risk", rep.Risks)
	}
	if rep.Next.Action != ActionPromoteToReady || rep.Next.WorkitemID != "WLM-draft" || !strings.Contains(rep.Next.Reason, "--to ready") {
		t.Fatalf("next = %+v, want promote-to-ready recommendation", rep.Next)
	}
}
