package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// MCPSnippet returns the copy-paste MCP client configuration for one harness.
//
// File shapes written by `mcp install` were checked against the vendor docs
// (Codex command+args, Claude mcpServers command+args, OpenCode type local +
// command array, cwd not directory). JSONC and an inline Codex mcp_servers
// table are still refused, not rewritten. The snippet may include cwd; the
// installer does not write cwd.
//
// Snippets pin `--tier core` (the daily subset). Progress / failure / block
// tools (`run_update`, `run_fail`, `workitem_block`) live at `--tier standard`
// or the CLI — core stays an explicit opt-in surface.
func MCPSnippet(name, devsysBin, projectRoot string) (string, error) {
	const coreNote = `Default --tier core is the 19-tool daily subset. ` +
		`run_update / run_fail / workitem_block need the CLI or --tier standard.`
	const npxNote = "\n# Optional npx form (cold start, needs a network; not a replacement for the local binary above):\n# npx --yes @kaki317/workloom mcp serve --profile session,executor --tier core\n"
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
# File form (~/.codex/config.toml; command + args verified):
# [mcp_servers.devsys]
# command = ` + binJSON + `
# args = ` + args + `
# cwd = ` + cwdJSON + envTOML + npxNote, nil
	case "claude":
		return `# Claude Code (.mcp.json, project root). mcp install writes command, args, and type; cwd below is paste-only:
# ` + coreNote + `
{
  "mcpServers": {
    "devsys": {
      "command": ` + binJSON + `,
      "args": ` + args + `,
      "cwd": ` + cwdJSON + envJSON + `
    }
  }
}` + npxNote, nil
	case "opencode":
		envOpen := ""
		if envJSON != "" {
			envOpen = ",\n      \"environment\": {\n        \"DEVSYS_CONFIG_DIR\": " + jsonString(filepath.ToSlash(configDir)) + "\n      }"
		}
		return `# OpenCode (opencode.json). mcp install writes type local + command array; cwd below is paste-only:
# ` + coreNote + `
{
  "mcp": {
    "devsys": {
      "type": "local",
      "command": ` + commandArr + `,
      "cwd": ` + cwdJSON + envOpen + `
    }
  }
}` + npxNote, nil
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
