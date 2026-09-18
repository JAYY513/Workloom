package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// newWorkitem creates a work item through the CLI and returns its id.
func newWorkitem(t *testing.T, title string) string {
	t.Helper()
	code, out, errOut := run(t, "--json", "workitem", "create", "--title", title, "--actor", "operator", "--reason", "fixture")
	if code != CodeOK {
		t.Fatalf("workitem create: code=%d stderr=%q", code, errOut)
	}
	var payload struct {
		Item struct {
			ID string `json:"id"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("create output: %v (%s)", err, out)
	}
	return payload.Item.ID
}

// recordVersion reads a decision through the CLI and returns its version hash.
func cliRecordVersion(t *testing.T, kind, id string) string {
	t.Helper()
	code, out, errOut := run(t, "--json", kind, "get", id)
	if code != CodeOK {
		t.Fatalf("%s get %s: code=%d stderr=%q", kind, id, code, errOut)
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("cli output: %v (%s)", err, out)
	}
	return payload.Version
}

// TestRecordCLIAndMCPAgree pins the M4.3 acceptance: one record, two entry
// points, identical identity and version hash.
func TestRecordCLIAndMCPAgree(t *testing.T) {
	gatedProject(t)
	cs, _ := serveSession(t)

	// CLI creates, MCP reads.
	wi := newWorkitem(t, "sdk adoption task")
	code, out, errOut := run(t, "--json", "decision", "create",
		"--title", "adopt the sdk", "--decision", "use the official MCP Go SDK",
		"--by", "operator", "--related", wi)
	if code != CodeOK {
		t.Fatalf("decision create: code=%d stderr=%q", code, errOut)
	}
	var created struct {
		Decision struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"decision"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("create output: %v (%s)", err, out)
	}
	if created.Decision.ID == "" || created.Version == "" {
		t.Fatalf("create output lacks identity: %s", out)
	}

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "decision_get", Arguments: map[string]any{"id": created.Decision.ID},
	})
	if err != nil {
		t.Fatalf("decision_get: %v", err)
	}
	if res.IsError {
		t.Fatalf("decision_get failed: %+v", res)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var overMCP struct {
		Decision struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"decision"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &overMCP); err != nil {
		t.Fatalf("mcp payload: %v (%s)", err, raw)
	}
	if overMCP.Version != created.Version || overMCP.Decision.Title != created.Decision.Title {
		t.Fatalf("MCP view %+v differs from CLI view %+v", overMCP, created)
	}

	// MCP creates, CLI reads — same record identity space.
	res, err = cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "finding_create",
		Arguments: map[string]any{
			"title": "gate evidence window", "description": "evidence is collected outside the transition transaction",
			"severity": "low", "related_workitems": []string{wi},
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("finding_create: err=%v res=%+v", err, res)
	}
	raw, _ = json.Marshal(res.StructuredContent)
	var finding struct {
		Finding struct {
			ID string `json:"id"`
		} `json:"finding"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &finding); err != nil {
		t.Fatalf("finding payload: %v (%s)", err, raw)
	}
	if got := cliRecordVersion(t, "finding", finding.Finding.ID); got != finding.Version {
		t.Fatalf("CLI version %s != MCP version %s", got, finding.Version)
	}
}

// TestContextLocatesRecords proves an agent can find tasks, progress and
// decisions without reading source.
func TestContextLocatesRecords(t *testing.T) {
	gatedProject(t)
	cs, _ := serveSession(t)
	wi := newWorkitem(t, "context probe task")

	if code, _, errOut := run(t, "decision", "create", "--title", "context probe",
		"--decision", "recorded for context", "--by", "operator", "--related", wi); code != CodeOK {
		t.Fatalf("decision create: %d %s", code, errOut)
	}

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "context_get", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("context_get: err=%v res=%+v", err, res)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var view struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
		Verdict         string `json:"verdict"`
		RecentDecisions []struct {
			Ref string `json:"ref"`
		} `json:"recent_decisions"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("context payload: %v (%s)", err, raw)
	}
	if view.Project.ID != "proj" || view.Verdict == "" {
		t.Fatalf("context = %+v", view)
	}
	found := false
	for _, ref := range view.RecentDecisions {
		if strings.HasPrefix(ref.Ref, "decision://") {
			found = true
		}
	}
	if !found {
		t.Fatalf("context does not reference the decision: %s", raw)
	}

	// context_for_workitem surfaces records that reference the work item.
	res, err = cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "context_for_workitem", Arguments: map[string]any{"id": wi},
	})
	if err != nil {
		t.Fatalf("context_for_workitem: %v", err)
	}
	if res.IsError {
		t.Fatalf("context_for_workitem failed: %+v", res)
	}
	raw, _ = json.Marshal(res.StructuredContent)
	if !strings.Contains(string(raw), "decision://") {
		t.Fatalf("work item context lacks the referencing decision: %s", raw)
	}
}

// TestRunLifecycleEvidence walks the run tools the milestone owns (create,
// read, guarded update, log, heartbeat credentials).
func TestRunLifecycleEvidence(t *testing.T) {
	gatedProject(t)
	wi := newWorkitem(t, "run evidence task")
	if code, _, errOut := run(t, "run", "create", "--workitem", wi, "--actor", "operator", "--reason", "evidence"); code != CodeOK {
		t.Fatalf("run create: %d %s", code, errOut)
	}
	code, out, errOut := run(t, "--json", "run", "list", "--workitem", wi)
	if code != CodeOK {
		t.Fatalf("run list: %d %s", code, errOut)
	}
	var listed struct {
		Runs []struct {
			ID string `json:"id"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("run list output: %v (%s)", err, out)
	}
	if len(listed.Runs) != 1 {
		t.Fatalf("runs = %+v", listed.Runs)
	}
	id := listed.Runs[0].ID
	version := cliRecordVersion(t, "run", id)

	if code, _, errOut := run(t, "run", "update", "--id", id, "--status", "completed",
		"--log", "did the thing", "--expect", version); code != CodeOK {
		t.Fatalf("run update: %d %s", code, errOut)
	}
	code, out, _ = run(t, "--json", "run", "log", "--id", id)
	if code != CodeOK {
		t.Fatalf("run log: %d", code)
	}
	if !strings.Contains(out, "did the thing") {
		t.Fatalf("run log lacks the appended line: %s", out)
	}

	// Heartbeat needs a real lease: an unclaimed work item refuses it.
	if code, _, _ := run(t, "run", "heartbeat", "--id", id, "--owner", "nobody",
		"--token", "nope", "--actor", "operator", "--reason", "try"); code == CodeOK {
		t.Fatal("heartbeat without a lease succeeded")
	}
}

// TestArtifactVersionChain walks register → update → history.
func TestArtifactVersionChain(t *testing.T) {
	gatedProject(t)
	wi := newWorkitem(t, "artifact chain task")
	if code, _, errOut := run(t, "artifact", "register", "--name", "design.md",
		"--path", "docs/design.md", "--related", wi); code != CodeOK {
		t.Fatalf("artifact register: %d %s", code, errOut)
	}
	code, out, _ := run(t, "--json", "artifact", "list")
	if code != CodeOK {
		t.Fatal("artifact list failed")
	}
	var listed struct {
		Artifacts []struct {
			ID string `json:"id"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil || len(listed.Artifacts) != 1 {
		t.Fatalf("artifact list: %v (%s)", err, out)
	}
	id := listed.Artifacts[0].ID
	version := cliRecordVersion(t, "artifact", id)

	code, out, errOut := run(t, "--json", "artifact", "update", "--id", id, "--status", "approved", "--expect", version)
	if code != CodeOK {
		t.Fatalf("artifact update: %d %s", code, errOut)
	}
	var updated struct {
		Artifact struct {
			ID      string `json:"id"`
			Status  string `json:"status"`
			Version int    `json:"version"`
		} `json:"artifact"`
	}
	if err := json.Unmarshal([]byte(out), &updated); err != nil {
		t.Fatalf("update output: %v (%s)", err, out)
	}
	if updated.Artifact.Version != 2 || updated.Artifact.Status != "approved" || updated.Artifact.ID == id {
		t.Fatalf("update did not append a new version: %s", out)
	}
	code, out, _ = run(t, "--json", "artifact", "history", updated.Artifact.ID)
	if code != CodeOK {
		t.Fatal("artifact history failed")
	}
	var history struct {
		Artifacts []struct {
			Version    int     `json:"version"`
			PreviousID *string `json:"previous_id"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal([]byte(out), &history); err != nil {
		t.Fatalf("history output: %v (%s)", err, out)
	}
	if len(history.Artifacts) != 2 || history.Artifacts[0].Version != 2 || history.Artifacts[0].PreviousID == nil {
		t.Fatalf("history = %s", out)
	}
}

// TestKnowledgeStatusDegrades checks the documented M5 degradation: the tool
// answers honestly instead of failing.
func TestKnowledgeStatusDegrades(t *testing.T) {
	gatedProject(t)
	code, out, errOut := run(t, "--json", "knowledge", "status")
	if code != CodeOK {
		t.Fatalf("knowledge status: %d %s", code, errOut)
	}
	if !strings.Contains(out, "unavailable") || !strings.Contains(out, "M5") {
		t.Fatalf("knowledge status = %s", out)
	}
}
