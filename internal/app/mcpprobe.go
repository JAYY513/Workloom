package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/JAYY513/Workloom/internal/version"
)

// MCPProbeTimeout bounds the startup probe. A client that connects to a
// registered server must see an answer inside this window, otherwise the
// registration is reported as not working.
const MCPProbeTimeout = 10 * time.Second

// MCPProbeResult is the outcome of one `mcp serve` startup probe. It is data,
// not an error: the caller decides whether a failed probe fails the command.
// Skipped marks a probe that was not run (DEVSYS_MCP_PROBE=0) — that is "not
// checked", not "working".
type MCPProbeResult struct {
	OK         bool   `json:"ok"`
	Skipped    bool   `json:"skipped,omitempty"`
	Tools      int    `json:"tools"`
	DurationMS int64  `json:"duration_ms"`
	Detail     string `json:"detail,omitempty"`
}

// probeDisabled reports whether the operator turned the startup check off. The
// check spawns a server, which some environments cannot do (a sandbox that
// forbids child processes); skipping is explicit so it can never be mistaken
// for a passing check.
func probeDisabled() bool {
	v, ok := os.LookupEnv("DEVSYS_MCP_PROBE")
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "off", "false", "no":
		return true
	default:
		return false
	}
}

// ProbeMCPServer starts the registered command, completes the MCP initialize
// handshake and lists the tool surface — the same three steps the first client
// to connect will take. Writing a config file is not evidence that the server
// starts, so the caller reports both.
func ProbeMCPServer(ctx context.Context, cmd MCPCommand, serve []string, cwd string) MCPProbeResult {
	if probeDisabled() {
		return MCPProbeResult{Skipped: true, Detail: "skipped (DEVSYS_MCP_PROBE=0)"}
	}
	started := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, MCPProbeTimeout)
	defer cancel()

	line := exec.CommandContext(runCtx, cmd.Command, cmd.ServeArgs(serve)...)
	if strings.TrimSpace(cwd) != "" {
		line.Dir = cwd
	}
	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "workloom-probe", Version: version.Version},
		&mcpsdk.ClientOptions{Logger: discardLogger()},
	)
	session, err := client.Connect(runCtx, &mcpsdk.CommandTransport{Command: line}, nil)
	if err != nil {
		return failedProbe(started, err)
	}
	defer session.Close()

	tools, err := session.ListTools(runCtx, nil)
	if err != nil {
		return failedProbe(started, err)
	}
	return MCPProbeResult{OK: true, Tools: len(tools.Tools), DurationMS: time.Since(started).Milliseconds()}
}

func failedProbe(started time.Time, err error) MCPProbeResult {
	return MCPProbeResult{Detail: err.Error(), DurationMS: time.Since(started).Milliseconds()}
}

// discardLogger keeps the SDK's diagnostics out of the operator's report:
// stdout carries the report, and a probe is expected to fail sometimes.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
