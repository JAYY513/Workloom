package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPProbeHelperServer is not a test of its own: the probe test starts it
// as a real stdio MCP server in a child process of this test binary. It exits
// through os.Exit so the test framework never writes to stdout — stdout is the
// protocol channel.
func TestMCPProbeHelperServer(t *testing.T) {
	if os.Getenv("WORKLOOM_PROBE_HELPER") != "1" {
		t.Skip("helper process for TestProbeMCPServerReportsAWorkingServer")
	}
	srv := mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: "fake-workloom", Version: "test"},
		&mcpsdk.ServerOptions{Logger: discardLogger()},
	)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "health", Description: "fake liveness tool"},
		func(context.Context, *mcpsdk.CallToolRequest, probeHelperInput) (*mcpsdk.CallToolResult, struct{}, error) {
			return nil, struct{}{}, nil
		})
	if err := srv.Run(context.Background(), &mcpsdk.StdioTransport{}); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

type probeHelperInput struct {
	Ping string `json:"ping,omitempty"`
}

// stubProbe replaces the startup probe in tests that are about the writer: the
// probe itself is exercised against a real stdio server below.
func stubProbe(res MCPProbeResult) func(context.Context, MCPCommand, []string, string) MCPProbeResult {
	return func(context.Context, MCPCommand, []string, string) MCPProbeResult { return res }
}

// The probe answers the question the operator actually has: does a client that
// connects to this registration get a working server?
func TestProbeMCPServerReportsAWorkingServer(t *testing.T) {
	t.Setenv("WORKLOOM_PROBE_HELPER", "1")
	cmd := MCPCommand{Command: os.Args[0], Args: []string{"-test.run=TestMCPProbeHelperServer"}}
	res := ProbeMCPServer(context.Background(), cmd, []string{"mcp", "serve", "--tier", "core"}, "")
	if !res.OK {
		t.Fatalf("probe failed: %s", res.Detail)
	}
	if res.Tools != 1 {
		t.Fatalf("tools = %d, want the helper's single tool", res.Tools)
	}
}

// A command that starts and exits without speaking MCP is exactly the failure
// a config file cannot show: the probe must report it, not hang.
func TestProbeMCPServerReportsACommandThatNeverSpeaksMCP(t *testing.T) {
	cmd := MCPCommand{Command: os.Args[0], Args: []string{"-test.run=^$"}}
	res := ProbeMCPServer(context.Background(), cmd, []string{"mcp", "serve"}, "")
	if res.OK {
		t.Fatal("probe reported success for a command that never speaks MCP")
	}
	if res.Detail == "" {
		t.Fatal("failure carries no detail for the operator")
	}
}

func TestProbeMCPServerReportsAMissingExecutable(t *testing.T) {
	cmd := MCPCommand{Command: filepath.Join(t.TempDir(), "no-such-workloom")}
	res := ProbeMCPServer(context.Background(), cmd, []string{"mcp", "serve"}, "")
	if res.OK || res.Detail == "" {
		t.Fatalf("probe = %+v, want a reported failure", res)
	}
}

// The escape hatch has to be visible in the result: "not checked" must never
// be read as "working". The command here does not exist, so a probe that ran
// could not report skipped.
func TestProbeMCPServerHonoursTheSkipSwitch(t *testing.T) {
	t.Setenv("DEVSYS_MCP_PROBE", "0")
	res := ProbeMCPServer(context.Background(), MCPCommand{Command: "no-such-workloom-cli"}, []string{"mcp", "serve"}, "")
	if !res.Skipped || res.OK {
		t.Fatalf("probe = %+v, want a skipped result", res)
	}
	if res.Detail == "" {
		t.Fatal("skipped probe carries no reason for the operator")
	}
}
