package app

import (
	"strings"
)

// MCPSnippet returns the copy-paste MCP client configuration for one harness.

// File-based shapes are marked [UNVERIFIED]: no live client of that kind has
// been connected from this machine. The stdio command itself is verified:
// `devsys mcp serve` over IOTransport (smoke-m4 via scripts/m4helper).
func MCPSnippet(name, devsysBin string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "codex":
		return `# Codex: ephemeral injection (verified with codex-cli 0.144.1, no user config touched):
codex -c mcp_servers.devsys.command="` + devsysBin + `" -c mcp_servers.devsys.args='["mcp","serve","--profile","session,executor"]' -c mcp_servers.devsys.cwd="<project-root>"
# File form [UNVERIFIED] (~/.codex/config.toml):
# [mcp_servers.devsys]
# command = "` + devsysBin + `"
# args = ["mcp", "serve", "--profile", "session,executor"]
# cwd = "<project-root>"`, nil
	case "claude":
		return `# Claude Code (.mcp.json, project root) [UNVERIFIED file shape]:
{
  "mcpServers": {
    "devsys": {
      "command": "` + devsysBin + `",
      "args": ["mcp", "serve", "--profile", "session,executor"],
      "cwd": "<project-root>"
    }
  }
}`, nil
	case "opencode":
		return `# OpenCode (opencode.json) [UNVERIFIED file shape]:
{
  "mcp": {
    "devsys": {
      "type": "local",
      "command": ["` + devsysBin + `", "mcp", "serve", "--profile", "session,executor"],
      "directory": "<project-root>"
    }
  }
}`, nil
	default:
		return "", Usagef("unknown harness %q (expected codex, claude or opencode)", name)
	}
}
