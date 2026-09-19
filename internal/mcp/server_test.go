package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// session connects an SDK client to a fresh server over the in-memory
// transport: the tests exercise the real protocol path, not internals.
func session(t *testing.T, cfg Config) *mcpsdk.ClientSession {
	t.Helper()
	if cfg.Profiles == nil {
		cfg.Profiles = DefaultProfiles()
	}
	server := NewServer(cfg)
	st, ct := mcpsdk.NewInMemoryTransports()
	ctx := context.Background()
	go func() { _ = server.Run(ctx, st) }()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "devsys-test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func toolNames(t *testing.T, cs *mcpsdk.ClientSession) []string {
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

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// payload decodes a tool result's structured content. Error results carry
// the structured failure in their text content instead (the SDK still
// serializes the handler's zero output value alongside), so IsError results
// are read from text.
func payload[T any](t *testing.T, res *mcpsdk.CallToolResult) T {
	t.Helper()
	var out T
	if !res.IsError && res.StructuredContent != nil {
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatalf("structured content: %v", err)
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("structured content is not the expected shape: %v (%s)", err, raw)
		}
		return out
	}
	for _, c := range res.Content {
		if text, ok := c.(*mcpsdk.TextContent); ok {
			if err := json.Unmarshal([]byte(text.Text), &out); err != nil {
				t.Fatalf("text content is not the expected shape: %v (%s)", err, text.Text)
			}
			return out
		}
	}
	t.Fatalf("tool result carries no structured content: %+v", res)
	return out
}

func TestServerAdvertisesIdentityAndInstructions(t *testing.T) {
	cs := session(t, Config{Root: t.TempDir(), ServerVersion: "test-1", Instructions: "discipline"})
	init := cs.InitializeResult()
	if init == nil {
		t.Fatal("no initialize result")
	}
	if init.ServerInfo.Name != ServerName || init.ServerInfo.Version != "test-1" {
		t.Fatalf("server info = %+v", init.ServerInfo)
	}
	if !strings.Contains(init.Instructions, "discipline") {
		t.Fatalf("instructions = %q", init.Instructions)
	}
	if init.Capabilities == nil || init.Capabilities.Tools == nil {
		t.Fatalf("tools capability missing: %+v", init.Capabilities)
	}
}

func TestToolsListFiltersByProfile(t *testing.T) {
	def := toolNames(t, session(t, Config{Root: t.TempDir(), ServerVersion: "test"}))
	if !contains(def, "health") || !contains(def, "workitem_list") || !contains(def, "agent_session_start") {
		t.Fatalf("default tier lacks expected core tools: %v", def)
	}
	if contains(def, "workflow_step_complete") || contains(def, "project_update") || contains(def, "approval_decide") || contains(def, "project_create") {
		t.Fatalf("default tier exposes non-core tools: %v", def)
	}
	admin := toolNames(t, session(t, Config{Root: t.TempDir(), ServerVersion: "test", Profiles: []string{ProfileAdmin}, Tier: TierStandard}))
	if !contains(admin, "project_update") || !contains(admin, "project_create") || !contains(admin, "project_state_update") {
		t.Fatalf("admin profile lacks admin tools: %v", admin)
	}
	if contains(admin, "health") || contains(admin, "workitem_get") {
		t.Fatalf("admin profile leaks session tools: %v", admin)
	}
	reviewer := toolNames(t, session(t, Config{Root: t.TempDir(), ServerVersion: "test", Profiles: []string{ProfileReviewer}, Tier: TierStandard}))
	if !contains(reviewer, "approval_decide") {
		t.Fatalf("reviewer profile lacks approval_decide: %v", reviewer)
	}
}

// TestTierStandardRestoresFullSessionExecutor pins that --tier standard shows
// everything the selected profiles expose (the pre-tier default surface).
func TestTierStandardRestoresFullSessionExecutor(t *testing.T) {
	names := toolNames(t, session(t, Config{Root: t.TempDir(), ServerVersion: "test", Tier: TierStandard}))
	for _, want := range []string{"health", "workitem_list", "workflow_step_complete", "workitem_update", "context_get", "knowledge_status"} {
		if !contains(names, want) {
			t.Fatalf("standard tier lacks %s: %v", want, names)
		}
	}
}

// TestParseTier rejects unknown tiers so a typo cannot silently widen or
// narrow the tool surface.
func TestParseTier(t *testing.T) {
	if got, err := ParseTier(""); err != nil || got != TierCore {
		t.Fatalf("default tier = %q, %v", got, err)
	}
	if got, err := ParseTier("standard"); err != nil || got != TierStandard {
		t.Fatalf("standard tier = %q, %v", got, err)
	}
	if _, err := ParseTier("root"); err == nil {
		t.Fatal("unknown tier accepted")
	}
}

func TestReadOnlyToolsAreAnnotated(t *testing.T) {
	cs := session(t, Config{Root: t.TempDir(), ServerVersion: "test"})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	for _, tool := range res.Tools {
		switch tool.Name {
		case "workitem_list", "project_status", "workflow_get", "approval_list", "health":
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("%s is not annotated read-only: %+v", tool.Name, tool.Annotations)
			}
		case "workitem_create", "workitem_claim":
			if tool.Annotations != nil && tool.Annotations.ReadOnlyHint {
				t.Fatalf("%s claims to be read-only", tool.Name)
			}
		}
	}
}

func TestHealthReportsProjectFacts(t *testing.T) {
	root := t.TempDir()
	cs := session(t, Config{Root: root, ServerVersion: "test-1"})
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "health", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if res.IsError {
		t.Fatalf("health reported an error: %+v", res)
	}
	got := payload[healthResult](t, res)
	if got.Server.Name != ServerName || got.Server.Version != "test-1" {
		t.Fatalf("server = %+v", got.Server)
	}
	if got.Root != root {
		t.Fatalf("root = %q, want %q", got.Root, root)
	}
	if got.Devsys.Present {
		t.Fatal("devsys.present = true for an uninitialized root")
	}
	if got.Project != nil {
		t.Fatalf("project = %+v, want null", got.Project)
	}
	if !contains(got.Profiles, ProfileSession) {
		t.Fatalf("profiles = %v", got.Profiles)
	}
}

func TestToolFailureCarriesStructuredError(t *testing.T) {
	cs := session(t, Config{Root: t.TempDir(), ServerVersion: "test"})
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "project_get", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if !res.IsError {
		t.Fatalf("project_get on an uninitialized root did not fail: %+v", res)
	}
	got := payload[toolError](t, res)
	if got.Code != "precondition" {
		t.Fatalf("code = %q, want precondition", got.Code)
	}
	if !strings.Contains(got.Message, "devsys init") {
		t.Fatalf("message = %q", got.Message)
	}
}

func TestUnknownToolIsRefused(t *testing.T) {
	cs := session(t, Config{Root: t.TempDir(), ServerVersion: "test"})
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "project_update", Arguments: map[string]any{}})
	if err == nil {
		t.Fatalf("a tool outside the profile was callable: %+v", res)
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("error = %v", err)
	}
}

func TestSchemaRejectsUnknownArguments(t *testing.T) {
	cs := session(t, Config{Root: t.TempDir(), ServerVersion: "test"})
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "workitem_get",
		Arguments: map[string]any{"id": "WLM-1", "bogus": true},
	})
	if err != nil {
		t.Fatalf("schema rejection surfaced as a protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("unknown argument accepted: %+v", res)
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			text += tc.Text
		}
	}
	if !strings.Contains(text, "bogus") && !strings.Contains(text, "additional") {
		t.Fatalf("rejection does not name the offending field: %q", text)
	}
}

func TestParseProfiles(t *testing.T) {
	got, err := ParseProfiles("")
	if err != nil || strings.Join(got, ",") != "session,executor" {
		t.Fatalf("defaults = %v, %v", got, err)
	}
	got, err = ParseProfiles(" admin , admin,session ")
	if err != nil || strings.Join(got, ",") != "admin,session" {
		t.Fatalf("explicit = %v, %v", got, err)
	}
	if _, err := ParseProfiles("root"); err == nil {
		t.Fatal("unknown profile accepted")
	}
	if _, err := ParseProfiles("session,,admin"); err == nil {
		t.Fatal("empty profile name accepted")
	}
}
