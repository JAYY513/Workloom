package app

import (
	"encoding/json"
	"os"
	"path/filepath"
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
func MCPSnippet(name, devsysBin, projectRoot string) (string, error) {
	const coreNote = `Default --tier core is the 19-tool daily subset. ` +
		`run_update / run_fail / workitem_block need the CLI or --tier standard.`
	bin := filepath.ToSlash(strings.TrimSpace(devsysBin))
	cwd := strings.TrimSpace(projectRoot)
	if cwd == "" {
		cwd = "<project-root>"
	} else {
		cwd = filepath.ToSlash(cwd)
	}
	binJSON := jsonString(bin)
	cwdJSON := jsonString(cwd)
	configDir := os.Getenv("DEVSYS_CONFIG_DIR")
	envJSON := ""
	envTOML := ""
	envCodex := ""
	if strings.TrimSpace(configDir) != "" {
		dir := filepath.ToSlash(configDir)
		envJSON = ",\n      \"env\": {\n        \"DEVSYS_CONFIG_DIR\": " + jsonString(dir) + "\n      }"
		envTOML = "\n# [mcp_servers.devsys.env]\n# DEVSYS_CONFIG_DIR = " + jsonString(dir)
		envCodex = " -c mcp_servers.devsys.env.DEVSYS_CONFIG_DIR=" + jsonString(dir)
	}
	args := `["mcp", "serve", "--profile", "session,executor", "--tier", "core"]`
	commandArr := jsonStringList(bin, "mcp", "serve", "--profile", "session,executor", "--tier", "core")
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "codex":
		return `# Codex: ephemeral injection (verified with codex-cli 0.144.1, no user config touched):
# ` + coreNote + `
codex -c mcp_servers.devsys.command=` + binJSON + ` -c mcp_servers.devsys.args='` + args + `' -c mcp_servers.devsys.cwd=` + cwdJSON + envCodex + `
# File form [UNVERIFIED] (~/.codex/config.toml):
# [mcp_servers.devsys]
# command = ` + binJSON + `
# args = ` + args + `
# cwd = ` + cwdJSON + envTOML, nil
	case "claude":
		return `# Claude Code (.mcp.json, project root) [UNVERIFIED file shape]:
# ` + coreNote + `
{
  "mcpServers": {
    "devsys": {
      "command": ` + binJSON + `,
      "args": ` + args + `,
      "cwd": ` + cwdJSON + envJSON + `
    }
  }
}`, nil
	case "opencode":
		envOpen := ""
		if envJSON != "" {
			envOpen = ",\n      \"environment\": {\n        \"DEVSYS_CONFIG_DIR\": " + jsonString(filepath.ToSlash(configDir)) + "\n      }"
		}
		return `# OpenCode (opencode.json) [UNVERIFIED file shape]:
# ` + coreNote + `
{
  "mcp": {
    "devsys": {
      "type": "local",
      "command": ` + commandArr + `,
      "directory": ` + cwdJSON + envOpen + `
    }
  }
}`, nil
	default:
		return "", Usagef("unknown harness %q (expected codex, claude or opencode)", name)
	}
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

func jsonStringList(parts ...string) string {
	b, err := json.Marshal(parts)
	if err != nil {
		return "[]"
	}
	return string(b)
}
