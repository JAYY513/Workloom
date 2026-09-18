// Command m4helper drives a scripted MCP session against a devsys binary and
// asserts the M4 acceptance: profile filtering, the health tool, a record
// round-trip through the tool surface, the session interface, and the error
// taxonomy. It uses the official Go SDK as the *client* — the same code the
// server speaks, exercised over the real stdio transport.
//
// usage: m4helper <devsys-binary> <project-dir>
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var failures int

func check(ok bool, format string, a ...any) {
	if ok {
		fmt.Printf("PASS: %s\n", fmt.Sprintf(format, a...))
		return
	}
	failures++
	fmt.Printf("FAIL: %s\n", fmt.Sprintf(format, a...))
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: m4helper <devsys-binary> <project-dir>")
		os.Exit(2)
	}
	binary, project := os.Args[1], os.Args[2]
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "mcp", "serve")
	cmd.Dir = project
	stdin, err := cmd.StdinPipe()
	if err != nil {
		fail("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fail("stdout pipe: %v", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		fail("start server: %v", err)
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "m4helper", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.IOTransport{Reader: stdout, Writer: stdin}, nil)
	if err != nil {
		fail("connect: %v (stderr: %s)", err, stderr.String())
	}
	defer func() {
		_ = session.Close()
		_ = cmd.Wait()
	}()

	// --- M4.1/M4.2: profile-filtered tool surface -------------------------
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		fail("tools/list: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range list.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"health", "workitem_list", "workitem_get", "decision_get", "agent_session_start", "knowledge_status", "run_heartbeat"} {
		check(names[want], "default profile exposes %s", want)
	}
	for _, forbidden := range []string{"project_update", "project_create", "approval_decide"} {
		check(!names[forbidden], "default profile hides %s", forbidden)
	}

	// --- health -----------------------------------------------------------
	health, err := call(ctx, session, "health", map[string]any{})
	if err != nil {
		fail("health: %v", err)
	}
	check(!health.IsError, "health call succeeds")
	check(strings.Contains(structured(health), `"present":true`) || strings.Contains(structured(health), `"present": true`),
		"health reports an initialized project")

	// --- M4.2/M4.3: a record round-trip over MCP --------------------------
	created, err := call(ctx, session, "decision_create", map[string]any{
		"title": "smoke decision", "decision": "recorded by the smoke script", "created_by": "smoke",
	})
	check(err == nil && !created.IsError, "decision_create succeeds (err=%v)", err)
	decisionID, version := decisionIdentity(created)
	check(decisionID != "" && version != "", "decision_create returns identity and version (got %q/%q)", decisionID, version)

	got, err := call(ctx, session, "decision_get", map[string]any{"id": decisionID})
	check(err == nil && !got.IsError, "decision_get succeeds (err=%v)", err)
	gotID, gotVersion := decisionIdentity(got)
	check(gotID == decisionID && gotVersion == version,
		"decision_get returns the same record and version (%s/%s)", gotID, gotVersion)

	// --- M4.4: session start ---------------------------------------------
	sessionRes, err := call(ctx, session, "agent_session_start", map[string]any{
		"harness": "m4helper", "agent_id": "smoke", "intent": "acceptance",
	})
	check(err == nil && !sessionRes.IsError, "agent_session_start succeeds (err=%v)", err)
	check(strings.Contains(structured(sessionRes), `"recommended_next_action"`),
		"agent_session_start returns a recommended action")

	// --- error taxonomy ---------------------------------------------------
	missing, err := call(ctx, session, "workitem_get", map[string]any{"id": "WLM-999"})
	check(err == nil && missing.IsError, "missing work item is an isError result (err=%v)", err)
	check(strings.Contains(text(missing), `"code": "precondition"`) || strings.Contains(text(missing), `"code":"precondition"`),
		"missing work item reports the precondition code")

	bogus, err := call(ctx, session, "workitem_get", map[string]any{"id": "WLM-1", "bogus": true})
	check(err == nil && bogus.IsError, "unknown argument is refused (err=%v)", err)
	check(strings.Contains(text(bogus), "bogus") || strings.Contains(text(bogus), "additional"),
		"refusal names the offending argument")

	if failures > 0 {
		fmt.Printf("%d checks failed\n", failures)
		os.Exit(1)
	}
	fmt.Println("PASS: M4 MCP surface (profiles, health, records, session, error taxonomy).")
}

func call(ctx context.Context, session *mcpsdk.ClientSession, name string, args map[string]any) (*mcpsdk.CallToolResult, error) {
	return session.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: args})
}

// structured renders a successful result's structured content.
func structured(res *mcpsdk.CallToolResult) string {
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return ""
	}
	return string(raw)
}

// text renders an error result's text payload.
func text(res *mcpsdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// decisionIdentity extracts the record identity and version hash.
func decisionIdentity(res *mcpsdk.CallToolResult) (string, string) {
	var payload struct {
		Decision struct {
			ID string `json:"id"`
		} `json:"decision"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(structured(res)), &payload); err != nil {
		return "", ""
	}
	return payload.Decision.ID, payload.Version
}

func fail(format string, a ...any) {
	failures++
	fmt.Printf("FAIL: %s\n", fmt.Sprintf(format, a...))
}
