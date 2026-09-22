package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// The npm wrapper package (see npm/workloom). An npm-installed CLI runs from a
// nested node_modules tree whose layout belongs to the package manager, so a
// config that points at the platform binary embeds that layout — it breaks
// when the tree moves (hoisting changes, pnpm store pruning, a different
// package manager). Pointing at the wrapper script instead keeps the
// registration identical for every install of the same package name: node
// starts the wrapper, the wrapper execs the platform binary.
const (
	npmWrapperName    = "@kaki317/workloom"
	npmWrapperScript  = "bin/workloom.js"
	mcpCommandWalkMax = 6
)

// MCPCommand is the executable form written into MCP client configuration.
// Args precede the serve arguments (Codex and Claude spawn Command with Args;
// OpenCode uses one command array).
type MCPCommand struct {
	Command string
	Args    []string
}

// ServeArgs returns the full argv handed to the MCP client.
func (c MCPCommand) ServeArgs(serve []string) []string {
	out := make([]string, 0, len(c.Args)+len(serve))
	out = append(out, c.Args...)
	return append(out, serve...)
}

// MCPCommandFor resolves the running executable (os.Args[0] in the CLI).
func MCPCommandFor(exe string) MCPCommand {
	return ResolveMCPCommand(exe, exec.LookPath)
}

// ResolveMCPCommand maps an executable path onto the form written into client
// configs. Paths outside node_modules are returned unchanged; an npm-managed
// path is replaced by node + the wrapper script when the wrapper package is
// found next to it and node is available.
//
// Two npm layouts have to be covered: the platform package nested inside the
// wrapper (what a global install produces today) and hoisted next to it under
// the same node_modules (what a project-local install produces).
func ResolveMCPCommand(exe string, lookPath func(string) (string, error)) MCPCommand {
	exe = filepath.ToSlash(strings.TrimSpace(exe))
	if exe == "" || !strings.Contains(exe, "node_modules/") {
		return MCPCommand{Command: exe}
	}
	dir := path.Dir(exe)
	for range mcpCommandWalkMax {
		if dir == "" || dir == "." || dir == "/" {
			break
		}
		if isNPMWrapperDir(dir) {
			return wrapperCommand(dir, exe, lookPath)
		}
		if path.Base(dir) == "node_modules" {
			if hoisted := path.Join(dir, npmWrapperName); isNPMWrapperDir(hoisted) {
				return wrapperCommand(hoisted, exe, lookPath)
			}
		}
		dir = path.Dir(dir)
	}
	return MCPCommand{Command: exe}
}

// wrapperCommand is the npm form: node starts the wrapper script, which execs
// the platform binary. Without node the wrapper cannot run, so the binary path
// stays the honest fallback.
func wrapperCommand(wrapperDir, exe string, lookPath func(string) (string, error)) MCPCommand {
	if node, err := lookPath("node"); err == nil && strings.TrimSpace(node) != "" {
		return MCPCommand{
			Command: filepath.ToSlash(node),
			Args:    []string{filepath.ToSlash(path.Join(wrapperDir, npmWrapperScript))},
		}
	}
	return MCPCommand{Command: exe}
}

// isNPMWrapperDir reports whether dir holds the npm wrapper package: its
// manifest names the wrapper and the bin script it registers is present.
func isNPMWrapperDir(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(npmWrapperScript))); err != nil {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return false
	}
	var doc struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false
	}
	return doc.Name == npmWrapperName
}
