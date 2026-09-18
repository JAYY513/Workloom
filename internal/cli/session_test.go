package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// advanceTo walks a freshly created work item to a target status through the
// CLI (no workflow instance is declared, so no gates apply).
func advanceTo(t *testing.T, id, target string) {
	t.Helper()
	steps := map[string][]string{
		"ready":   {"backlog", "ready"},
		"blocked": {"backlog", "ready", "in_progress", "blocked"},
	}[target]
	if steps == nil {
		t.Fatalf("advanceTo: unsupported target %q", target)
	}
	for _, step := range steps {
		version := cliRecordVersion(t, "workitem", id)
		if code, _, errOut := run(t, "workitem", "transition", "--id", id, "--to", step,
			"--actor", "operator", "--reason", "fixture", "--expect", version); code != CodeOK {
			t.Fatalf("transition to %s: code=%d stderr=%q", step, code, errOut)
		}
	}
}

// TestSessionStartOrientation pins the M4.4 acceptance: one call gives a new
// session everything it needs to begin, including the recommended action.
func TestSessionStartOrientation(t *testing.T) {
	gatedProject(t)
	wi := newWorkitem(t, "session orientation task")
	advanceTo(t, wi, "ready")

	code, out, errOut := run(t, "--json", "session", "start",
		"--harness", "codex", "--agent", "codex-local", "--intent", "continue development")
	if code != CodeOK {
		t.Fatalf("session start: code=%d stderr=%q", code, errOut)
	}
	var view struct {
		Project struct {
			ID           string `json:"id"`
			CurrentPhase string `json:"current_phase"`
		} `json:"project"`
		CurrentWorkitems []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"current_workitems"`
		RecommendedNextAction struct {
			Type    string `json:"type"`
			ID      string `json:"id"`
			Reason  string `json:"reason"`
			Command string `json:"command"`
		} `json:"recommended_next_action"`
		Context struct {
			ProjectSummary string `json:"project_summary"`
		} `json:"context"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("session output: %v (%s)", err, out)
	}
	if view.Project.ID != "proj" {
		t.Fatalf("project = %+v", view.Project)
	}
	if len(view.CurrentWorkitems) != 1 || view.CurrentWorkitems[0].ID != wi {
		t.Fatalf("current work items = %+v, want %s", view.CurrentWorkitems, wi)
	}
	action := view.RecommendedNextAction
	if action.Type != "start" || action.ID != wi || action.Reason == "" {
		t.Fatalf("recommended action = %+v", action)
	}
	if !strings.Contains(action.Command, wi) {
		t.Fatalf("action command = %q, want it to name %s", action.Command, wi)
	}
}

// TestSessionStartCompactTrims checks the compact mode keeps the essentials.
func TestSessionStartCompactTrims(t *testing.T) {
	gatedProject(t)
	code, out, errOut := run(t, "--json", "session", "start", "--compact")
	if code != CodeOK {
		t.Fatalf("session start --compact: %d %s", code, errOut)
	}
	var view struct {
		RecommendedNextAction struct {
			Type string `json:"type"`
		} `json:"recommended_next_action"`
		Context struct {
			RecentEvents []any `json:"recent_events"`
		} `json:"context"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("session output: %v (%s)", err, out)
	}
	if view.RecommendedNextAction.Type == "" {
		t.Fatalf("compact session lacks a recommendation: %s", out)
	}
	if len(view.Context.RecentEvents) != 0 {
		t.Fatalf("compact session should omit the event list: %s", out)
	}
}

// TestAgentSessionStartOverMCP checks the same orientation through the MCP
// tool, with the recommendation matching the CLI.
func TestAgentSessionStartOverMCP(t *testing.T) {
	gatedProject(t)
	wi := newWorkitem(t, "mcp session task")
	advanceTo(t, wi, "ready")
	cs, _ := serveSession(t)

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "agent_session_start",
		Arguments: map[string]any{
			"harness": "codex", "agent_id": "codex-local", "intent": "continue development",
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("agent_session_start: err=%v res=%+v", err, res)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var view struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
		RecommendedNextAction struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"recommended_next_action"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("session payload: %v (%s)", err, raw)
	}
	if view.Project.ID != "proj" || view.RecommendedNextAction.Type != "start" || view.RecommendedNextAction.ID != wi {
		t.Fatalf("mcp session view = %+v", view)
	}
}
