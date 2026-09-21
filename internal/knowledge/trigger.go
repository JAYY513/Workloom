package knowledge

import (
	"strings"

	"github.com/JAYY513/Workloom/internal/domain"
)

// TriggerHit reports the first trigger that occurs in lowerText, the relevance
// rule the context assembly and the workspace view share (M5.6, 方案 §10.2):
// the trimmed trigger, lowercased, must appear as a substring, and the first
// match in trigger order wins. Callers pass text they have lowercased once, so
// a scan over many pages pays for the lowercasing once, not per page.
func TriggerHit(triggers []string, lowerText string) (string, bool) {
	for _, trigger := range triggers {
		trimmed := strings.TrimSpace(trigger)
		if trimmed == "" {
			continue
		}
		if strings.Contains(lowerText, strings.ToLower(trimmed)) {
			return trimmed, true
		}
	}
	return "", false
}

// WorkItemText is the text knowledge triggers are matched against (M5.6): the
// work item's title, description and type plus its workflow instance's id and
// step, newline-joined so one field cannot fake another, then lowercased once.
func WorkItemText(item *domain.WorkItem) string {
	if item == nil {
		return ""
	}
	workflowID, workflowStep := "", ""
	if item.Workflow != nil {
		workflowID, workflowStep = item.Workflow.ID, item.Workflow.Step
	}
	return strings.ToLower(strings.Join([]string{
		item.Title, item.Description, item.Type, workflowID, workflowStep,
	}, "\n"))
}
