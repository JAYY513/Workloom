package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// serveSession starts `devsys mcp serve` in-process over pipes and connects
// a real SDK client to it: the test exercises the CLI wiring and the whole
// protocol path, not internals. Extra args are appended to the serve command
// (e.g. "--tier", "standard" for tests that need the full surface).
func serveSession(t *testing.T, args ...string) (*mcpsdk.ClientSession, func() int) {
	t.Helper()
	toServerR, toServerW := io.Pipe()
	fromServerR, fromServerW := io.Pipe()
	done := make(chan int, 1)
	go func() {
		var errBuf bytes.Buffer
		code := RunWithIO(append([]string{"mcp", "serve"}, args...), toServerR, fromServerW, &errBuf)
		if code != CodeOK {
			t.Logf("mcp serve exited with %d: %s", code, errBuf.String())
		}
		_ = fromServerW.Close()
		done <- code
	}()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "cli-test", Version: "0"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	cs, err := client.Connect(ctx, &mcpsdk.IOTransport{Reader: fromServerR, Writer: toServerW}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, func() int { return <-done }
}

func toolNamesOf(t *testing.T, cs *mcpsdk.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func hasTool(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestMCPServeEndToEnd(t *testing.T) {
	repo, _ := gatedProject(t)
	cs, wait := serveSession(t)

	names := toolNamesOf(t, cs)
	for _, want := range []string{"health", "workitem_list", "workitem_get", "agent_session_start"} {
		if !hasTool(names, want) {
			t.Fatalf("default tier lacks %s: %v", want, names)
		}
	}
	for _, forbidden := range []string{"project_update", "approval_decide", "project_create", "workflow_step_complete", "run_fail", "run_cancel"} {
		if hasTool(names, forbidden) {
			t.Fatalf("default tier exposes %s: %v", forbidden, names)
		}
	}

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "health", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if res.IsError {
		t.Fatalf("health failed: %+v", res)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var health struct {
		Root    string `json:"root"`
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
		Devsys struct {
			Present bool `json:"present"`
		} `json:"devsys"`
	}
	if err := json.Unmarshal(raw, &health); err != nil {
		t.Fatalf("health payload: %v (%s)", err, raw)
	}
	if health.Root != repo || health.Project.ID != "proj" || !health.Devsys.Present {
		t.Fatalf("health = %+v, want root %s project proj", health, repo)
	}

	_ = cs.Close()
	if code := wait(); code != CodeOK {
		t.Fatalf("serve exit code = %d", code)
	}
}

// TestMCPServeWritesShareTheCLIPath proves the acceptance criterion that both
// entry points consume one implementation: a work item created through MCP is
// readable through the CLI with the very same version hash.
func TestMCPServeWritesShareTheCLIPath(t *testing.T) {
	gatedProject(t)
	cs, _ := serveSession(t)

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "workitem_create",
		Arguments: map[string]any{
			"title": "created over mcp", "actor": "codex", "reason": "m4.2 acceptance",
		},
	})
	if err != nil {
		t.Fatalf("workitem_create: %v", err)
	}
	if res.IsError {
		t.Fatalf("workitem_create failed: %+v", res)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var view struct {
		Item struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Title  string `json:"title"`
		} `json:"item"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("create payload: %v (%s)", err, raw)
	}
	if view.Item.ID == "" || view.Version == "" {
		t.Fatalf("create payload lacks identity: %s", raw)
	}

	code, out, errOut := run(t, "--json", "workitem", "get", view.Item.ID)
	if code != CodeOK {
		t.Fatalf("cli get: code=%d stderr=%q", code, errOut)
	}
	var cliView struct {
		Item struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"item"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(out), &cliView); err != nil {
		t.Fatalf("cli output: %v (%s)", err, out)
	}
	if cliView.Version != view.Version || cliView.Item.Title != "created over mcp" {
		t.Fatalf("CLI view %+v does not match MCP view %+v", cliView, view)
	}
}

func TestMCPServeRefusalsMatchCLITaxonomy(t *testing.T) {
	gatedProject(t)
	cs, _ := serveSession(t)

	// A work item that does not exist is a precondition on both surfaces.
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "workitem_get", Arguments: map[string]any{"id": "WLM-999"},
	})
	if err != nil {
		t.Fatalf("workitem_get: %v", err)
	}
	if !res.IsError {
		t.Fatalf("missing work item did not fail: %+v", res)
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			text += tc.Text
		}
	}
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("refusal payload: %v (%s)", err, text)
	}
	if payload.Code != "precondition" {
		t.Fatalf("code = %q, want precondition (%s)", payload.Code, text)
	}

	// The same operation through the CLI lands on the same exit code class.
	code, _, errOut := run(t, "workitem", "get", "WLM-999")
	if code != CodePrecondition {
		t.Fatalf("cli code = %d, want %d (%s)", code, CodePrecondition, errOut)
	}
}

func TestMCPServeRejectsUnknownProfile(t *testing.T) {
	gatedProject(t)
	code, _, stderr := runMCPArgs(t, "--profile", "root")
	if code != CodeUsage || !strings.Contains(stderr, "unknown profile") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	code, _, stderr = runMCPArgs(t, "--tier", "root")
	if code != CodeUsage || !strings.Contains(stderr, "unknown tier") {
		t.Fatalf("tier code=%d stderr=%q", code, stderr)
	}
}

func TestMCPServeRejectsOutputFlags(t *testing.T) {
	gatedProject(t)
	for _, flag := range []string{"--json", "--quiet"} {
		code, _, errOut := runMCPArgs(t, flag)
		if code != CodeUsage || !strings.Contains(errOut, flag) {
			t.Fatalf("serve %s: code=%d stderr=%q", flag, code, errOut)
		}
	}
	var out, errBuf bytes.Buffer
	code := RunWithIO([]string{"--json", "mcp", "serve"}, strings.NewReader(""), &out, &errBuf)
	if code != CodeUsage || !strings.Contains(errBuf.String(), "--json") {
		t.Fatalf("global --json before mcp: code=%d stderr=%q", code, errBuf.String())
	}
}

// runMCPArgs runs `devsys mcp serve` with no client attached (usage paths).
func runMCPArgs(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code := RunWithIO(append([]string{"mcp", "serve"}, args...), strings.NewReader(""), &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestMCPServeRequiresInitializedProject(t *testing.T) {
	t.Chdir(t.TempDir())
	code, _, stderr := runMCPArgs(t)
	if code != CodePrecondition || !strings.Contains(stderr, "devsys init") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestMCPUsage(t *testing.T) {
	for _, args := range [][]string{{"mcp"}, {"mcp", "bogus"}} {
		code, _, _ := run(t, args...)
		if code != CodeUsage {
			t.Fatalf("%v: code=%d, want %d", args, code, CodeUsage)
		}
	}
}
