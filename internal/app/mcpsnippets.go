package app

import (
	"strings"
)

// MCPSnippet returns the copy-paste MCP client configuration for one harness.
//
// File-based shapes are marked [UNVERIFIED]: no live client of that kind has
// been connected from this machine. The stdio command itself is verified:
// `devsys mcp serve` over IOTransport (smoke-m4 via scripts/m4helper).
//
// Snippets pin `--tier core` (the 19-tool daily subset). Progress / failure /
// block tools (`run_update`, `run_fail`, `workitem_block`) live at
// `--tier standard` or the CLI — core stays an explicit opt-in surface.
func MCPSnippet(name, devsysBin string) (string, error) {
	const coreNote = `Default --tier core is the 19-tool daily subset. ` +
		`run_update / run_fail / workitem_block need the CLI or --tier standard.`
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "codex":
		return `# Codex: ephemeral injection (verified with codex-cli 0.144.1, no user config touched):
# ` + coreNote + `
codex -c mcp_servers.devsys.command="` + devsysBin + `" -c mcp_servers.devsys.args='["mcp","serve","--profile","session,executor","--tier","core"]' -c mcp_servers.devsys.cwd="<project-root>"
# File form [UNVERIFIED] (~/.codex/config.toml):
# [mcp_servers.devsys]
# command = "` + devsysBin + `"
# args = ["mcp", "serve", "--profile", "session,executor", "--tier", "core"]
# cwd = "<project-root>"`, nil
	case "claude":
		return `# Claude Code (.mcp.json, project root) [UNVERIFIED file shape]:
# ` + coreNote + `
{
  "mcpServers": {
    "devsys": {
      "command": "` + devsysBin + `",
      "args": ["mcp", "serve", "--profile", "session,executor", "--tier", "core"],
      "cwd": "<project-root>"
    }
  }
}`, nil
	case "opencode":
		return `# OpenCode (opencode.json) [UNVERIFIED file shape]:
# ` + coreNote + `
{
  "mcp": {
    "devsys": {
      "type": "local",
      "command": ["` + devsysBin + `", "mcp", "serve", "--profile", "session,executor", "--tier", "core"],
      "directory": "<project-root>"
    }
  }
}`, nil
	default:
		return "", Usagef("unknown harness %q (expected codex, claude or opencode)", name)
	}
}
