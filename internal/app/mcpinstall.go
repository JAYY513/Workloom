package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// MCPInstallRequest describes one `workloom mcp install` run. Clients empty
// means every known client; Bin and HomeDir are test hooks (empty = the
// running devsys binary / the operator's home directory).
//
// Writing happens only when Apply is set and DryRun is not. The default
// (neither flag) is a dry-run. Force replaces an existing devsys entry;
// without it an existing entry is left untouched. Announce, when set, is
// called with the target path before any write.
type MCPInstallRequest struct {
	Scope    string
	Clients  []string
	Apply    bool
	Force    bool
	DryRun   bool
	Bin      string
	HomeDir  string
	Announce func(client, path string)

	// probe is a test hook: nil runs the real startup probe.
	probe func(context.Context, MCPCommand, []string, string) MCPProbeResult
}

// MCPInstallResult is one client's outcome. Status is one of installed,
// planned (dry-run), skipped (already registered), failed, not-detected.
type MCPInstallResult struct {
	Client string `json:"client"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Path   string `json:"path"`
}

// MCPInstallView is the `workloom mcp install` report. Probe is the result of
// starting the registered command once: writing a config is not evidence that
// the server runs, so both are reported.
type MCPInstallView struct {
	Scope   string             `json:"scope"`
	Results []MCPInstallResult `json:"results"`
	Probe   *MCPProbeResult    `json:"probe,omitempty"`
}

// mcpClientOrder is the registration order shown to the operator.
var mcpClientOrder = []string{"codex", "claude", "opencode"}

// mcpServeArgs is the invocation every client registers for devsys.
func mcpServeArgs() []string {
	return []string{"mcp", "serve", "--profile", "session,executor", "--tier", "core"}
}

// MCPInstall registers the devsys MCP server with Codex / Claude Code /
// OpenCode config files. Existing files are preserved byte-for-byte outside
// the inserted entry; anything unreadable or non-strict is refused (fail
// closed) with a `wire --print-mcp` pointer instead of a guessed rewrite.
func (s *Service) MCPInstall(ctx context.Context, req MCPInstallRequest) (*MCPInstallView, error) {
	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = "user"
	}
	if scope != "user" && scope != "project" {
		return nil, Usagef("unknown scope %q (expected user or project)", req.Scope)
	}
	exe := strings.TrimSpace(req.Bin)
	if exe == "" {
		resolved, err := os.Executable()
		if err != nil {
			return nil, Internalf("locate workloom binary: %v", err)
		}
		exe = filepath.ToSlash(resolved)
	}
	cmd := ResolveMCPCommand(exe, exec.LookPath)
	home := strings.TrimSpace(req.HomeDir)
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, Internalf("locate home directory: %v", err)
		}
		home = h
	}
	want := map[string]bool{}
	for _, c := range req.Clients {
		want[strings.ToLower(strings.TrimSpace(c))] = true
	}
	// An explicitly named client is an explicit intent: register even when
	// the client is not detectable here yet. Auto mode (no --client) only
	// touches clients that look installed (binary on PATH or config file).
	explicit := len(want) > 0
	if !explicit {
		for _, c := range mcpClientOrder {
			want[c] = true
		}
	}
	for name := range want {
		if !slices.Contains(mcpClientOrder, name) {
			return nil, Usagef("unknown client %q (expected codex, claude or opencode)", name)
		}
	}

	if req.Apply && req.DryRun {
		return nil, Usagef("`--apply` and `--dry-run` are mutually exclusive")
	}
	writing := req.Apply && !req.DryRun

	view := &MCPInstallView{Scope: scope}
	touched := false
	for _, name := range mcpClientOrder {
		if !want[name] {
			continue
		}
		res := MCPInstallResult{Client: name, Path: filepath.ToSlash(mcpClientPath(name, scope, home, s.Root))}
		detected := fileExists(res.Path)
		if !detected {
			if _, err := exec.LookPath(name); err == nil {
				detected = true
			}
		}
		if !explicit && !detected {
			res.Status = "not-detected"
			res.Detail = "client binary not on PATH and no config file yet; skipped"
			view.Results = append(view.Results, res)
			continue
		}
		if req.Announce != nil {
			req.Announce(name, res.Path)
		}
		var (
			changed bool
			detail  string
			err     error
		)
		switch name {
		case "codex":
			changed, detail, err = installCodexTOML(res.Path, cmd, !writing, req.Force)
		case "claude":
			changed, detail, err = installClientJSON(res.Path, "mcpServers", claudeMCPInstallEntry(cmd), !writing, req.Force)
		case "opencode":
			changed, detail, err = installClientJSON(res.Path, "mcp", opencodeMCPInstallEntry(cmd), !writing, req.Force)
		}
		switch {
		case err != nil:
			res.Status = "failed"
			res.Detail = err.Error()
		case changed && !writing:
			res.Status = "planned"
			res.Detail = detail
		case changed:
			res.Status = "installed"
			res.Detail = detail
		default:
			res.Status = "skipped"
			res.Detail = detail
		}
		view.Results = append(view.Results, res)
		touched = true
	}
	if touched {
		probe := ProbeMCPServer
		if req.probe != nil {
			probe = req.probe
		}
		view.Probe = new(probe(ctx, cmd, mcpServeArgs(), s.Root))
	}
	// A registration that cannot start is not a success. Dry runs report the
	// probe and stay read-only; `--apply` that wrote a config nobody can use
	// has to say so out loud (the config is kept — the operator decides).
	if writing && view.Probe != nil && !view.Probe.OK && !view.Probe.Skipped {
		return view, Preconditionf("registration written, but `workloom mcp serve` did not start: %s", view.Probe.Detail)
	}
	return view, nil
}

// mcpClientPath resolves the client's config file for the scope. User-scope
// paths match the clients' documented defaults; project-scope paths are the
// files `wire --check` already scans for.
func mcpClientPath(name, scope, home, root string) string {
	user := map[string]string{
		"codex":    filepath.Join(home, ".codex", "config.toml"),
		"claude":   filepath.Join(home, ".claude.json"),
		"opencode": filepath.Join(home, ".config", "opencode", "opencode.json"),
	}
	project := map[string]string{
		"codex":    filepath.Join(root, ".codex", "config.toml"),
		"claude":   filepath.Join(root, ".mcp.json"),
		"opencode": filepath.Join(root, "opencode.json"),
	}
	if scope == "project" {
		return project[name]
	}
	return user[name]
}

// installCodexTOML appends the [mcp_servers.devsys] table to the Codex
// config, creating the file when absent. An inline `mcp_servers = {...}`
// table is refused even with force: sub-table syntax cannot merge into it.
// force replaces an existing [mcp_servers.devsys] table and its
// [mcp_servers.devsys.*] subtables, leaving every other byte in place.
func installCodexTOML(path string, cmd MCPCommand, dryRun, force bool) (bool, string, error) {
	data, err := os.ReadFile(path)
	exists := true
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		exists, data = false, nil
	default:
		return false, "", err
	}
	block := "[mcp_servers.devsys]\ncommand = " + jsonString(cmd.Command) +
		"\nargs = " + jsonStringList(cmd.ServeArgs(mcpServeArgs())...) + "\n"
	if exists {
		for _, line := range bytes.Split(data, []byte("\n")) {
			t := bytes.TrimSpace(line)
			if bytes.HasPrefix(t, []byte("mcp_servers =")) {
				return false, "", fmt.Errorf("config uses an inline mcp_servers table; add the [mcp_servers.devsys] block manually, or use `workloom wire --print-mcp codex`")
			}
		}
		if bytes.Contains(data, []byte("[mcp_servers.devsys]")) || bytes.Contains(data, []byte("[mcp_servers.devsys.")) {
			if !force {
				return false, "already registered", nil
			}
			replaced, ok := replaceCodexDevsys(data, block)
			if !ok {
				return false, "already registered", nil
			}
			if dryRun {
				return true, "dry-run: would replace [mcp_servers.devsys]", nil
			}
			if err := os.WriteFile(path, replaced, 0o644); err != nil {
				return false, "", err
			}
			return true, "replaced devsys", nil
		}
	}
	var out []byte
	if !exists {
		out = []byte(block)
	} else {
		out = append([]byte{}, data...)
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}
		out = append(out, block...)
	}
	if dryRun {
		return true, "dry-run: would append [mcp_servers.devsys]", nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, "", err
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return false, "", err
	}
	return true, "registered devsys", nil
}

// replaceCodexDevsys swaps [mcp_servers.devsys] and [mcp_servers.devsys.*]
// tables for block. Later devsys tables are removed. Every other byte stays.
func replaceCodexDevsys(data []byte, block string) ([]byte, bool) {
	type span struct{ start, end int }
	var regions []span
	i := 0
	for i < len(data) {
		lineEnd := bytes.IndexByte(data[i:], '\n')
		var line []byte
		next := len(data)
		if lineEnd < 0 {
			line = data[i:]
		} else {
			line = data[i : i+lineEnd]
			next = i + lineEnd + 1
		}
		if isCodexDevsysHeader(line) {
			start := i
			j := next
			for j < len(data) {
				le := bytes.IndexByte(data[j:], '\n')
				var ln []byte
				n2 := len(data)
				if le < 0 {
					ln = data[j:]
				} else {
					ln = data[j : j+le]
					n2 = j + le + 1
				}
				if isTOMLTableHeader(ln) && !isCodexDevsysHeader(ln) {
					break
				}
				j = n2
				if le < 0 {
					break
				}
			}
			regions = append(regions, span{start, j})
			i = j
			continue
		}
		if lineEnd < 0 {
			break
		}
		i = next
	}
	if len(regions) == 0 {
		return data, false
	}
	out := append([]byte{}, data...)
	for n := len(regions) - 1; n >= 0; n-- {
		r := regions[n]
		if n == 0 {
			out = append(append(append([]byte{}, out[:r.start]...), block...), out[r.end:]...)
			continue
		}
		out = append(append([]byte{}, out[:r.start]...), out[r.end:]...)
	}
	return out, true
}

func isCodexDevsysHeader(line []byte) bool {
	t := bytes.TrimSpace(line)
	return bytes.Equal(t, []byte("[mcp_servers.devsys]")) || bytes.HasPrefix(t, []byte("[mcp_servers.devsys."))
}

func isTOMLTableHeader(line []byte) bool {
	t := bytes.TrimSpace(line)
	return len(t) >= 2 && t[0] == '[' && t[len(t)-1] == ']'
}

// installClientJSON inserts the devsys entry as the first member of the
// client's MCP object, preserving every other byte of the document. force
// replaces an existing devsys value and leaves sibling keys untouched.
func installClientJSON(path, objectKey string, entry []byte, dryRun, force bool) (bool, string, error) {
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		data = []byte("{}")
	default:
		return false, "", err
	}
	if !json.Valid(data) {
		return false, "", fmt.Errorf("not valid strict JSON (comments or trailing commas are refused, never rewritten): edit manually, or use `workloom wire --print-mcp`")
	}
	if force {
		_, wouldInsert, err := jsonSetEntry(data, objectKey, "devsys", entry, false)
		if err != nil {
			return false, "", err
		}
		if !wouldInsert {
			out, _, err := jsonSetEntry(data, objectKey, "devsys", entry, true)
			if err != nil {
				return false, "", err
			}
			if dryRun {
				return true, "dry-run: would replace mcp entry", nil
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return false, "", err
			}
			if err := os.WriteFile(path, out, 0o644); err != nil {
				return false, "", err
			}
			return true, "replaced devsys", nil
		}
	}
	out, inserted, err := jsonSetEntry(data, objectKey, "devsys", entry, false)
	if err != nil {
		return false, "", err
	}
	if !inserted {
		return false, "already registered", nil
	}
	if dryRun {
		return true, "dry-run: would insert mcp entry", nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, "", err
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return false, "", err
	}
	return true, "registered devsys", nil
}

// claudeMCPInstallEntry is the ~/.claude.json / .mcp.json member shape.
func claudeMCPInstallEntry(cmd MCPCommand) []byte {
	b, err := json.Marshal(map[string]any{
		"type":    "stdio",
		"command": cmd.Command,
		"args":    cmd.ServeArgs(mcpServeArgs()),
	})
	if err != nil {
		return []byte(`{"type":"stdio"}`)
	}
	return b
}

// opencodeMCPInstallEntry is the opencode.json member shape (command array,
// type local).
func opencodeMCPInstallEntry(cmd MCPCommand) []byte {
	b, err := json.Marshal(map[string]any{
		"type":    "local",
		"command": append([]string{cmd.Command}, cmd.ServeArgs(mcpServeArgs())...),
	})
	if err != nil {
		return []byte(`{"type":"local","command":[]}`)
	}
	return b
}

// jsonSetEntry inserts `"entryKey": entry` as the first member of the object
// stored at top-level property objectKey. The document must be strict JSON
// (callers validate first); every byte outside the inserted or replaced span
// is preserved. inserted=false when entryKey already exists and force is false.
func jsonSetEntry(doc []byte, objectKey, entryKey string, entry []byte, force bool) ([]byte, bool, error) {
	i := skipJSONSpace(doc, 0)
	if i >= len(doc) || doc[i] != '{' {
		return nil, false, errors.New("top-level JSON value is not an object")
	}
	i = skipJSONSpace(doc, i+1)
	if i < len(doc) && doc[i] == '}' {
		repl := []byte("\n  " + jsonString(objectKey) + ": {\n    " + jsonString(entryKey) + ": " + string(entry) + "\n  }\n")
		out := append(append([]byte{}, doc[:i]...), repl...)
		return append(out, doc[i:]...), true, nil
	}
	for {
		if i >= len(doc) || doc[i] != '"' {
			return nil, false, errors.New("malformed object member")
		}
		j, err := scanJSONString(doc, i)
		if err != nil {
			return nil, false, err
		}
		key, err := strconv.Unquote(string(doc[i:j]))
		if err != nil {
			return nil, false, err
		}
		i = skipJSONSpace(doc, j)
		if i >= len(doc) || doc[i] != ':' {
			return nil, false, errors.New("expected ':' after object key")
		}
		valStart := skipJSONSpace(doc, i+1)
		valEnd, err := skipJSONValue(doc, valStart)
		if err != nil {
			return nil, false, err
		}
		if key == objectKey {
			return insertIntoJSONObject(doc, valStart, entryKey, entry, force)
		}
		i = skipJSONSpace(doc, valEnd)
		if i < len(doc) && doc[i] == ',' {
			i = skipJSONSpace(doc, i+1)
			continue
		}
		if i < len(doc) && doc[i] == '}' {
			repl := []byte(",\n  " + jsonString(objectKey) + ": {\n    " + jsonString(entryKey) + ": " + string(entry) + "\n  }")
			out := append(append([]byte{}, doc[:i]...), repl...)
			return append(out, doc[i:]...), true, nil
		}
		return nil, false, errors.New("malformed object")
	}
}

// insertIntoJSONObject inserts the entry as the first member of the object
// starting at objStart (doc[objStart] == '{'). An existing entryKey is left
// untouched unless force is set, in which case only that value span is replaced.
func insertIntoJSONObject(doc []byte, objStart int, entryKey string, entry []byte, force bool) ([]byte, bool, error) {
	if objStart >= len(doc) || doc[objStart] != '{' {
		return nil, false, fmt.Errorf("existing %q is not an object; refusing to overwrite", entryKey)
	}
	j := skipJSONSpace(doc, objStart+1)
	if j < len(doc) && doc[j] == '}' {
		repl := []byte(jsonString(entryKey) + ": " + string(entry))
		out := append(append([]byte{}, doc[:j]...), repl...)
		return append(out, doc[j:]...), true, nil
	}
	firstMember := j
	for j < len(doc) && doc[j] == '"' {
		k, err := scanJSONString(doc, j)
		if err != nil {
			return nil, false, err
		}
		key, err := strconv.Unquote(string(doc[j:k]))
		if err != nil {
			return nil, false, err
		}
		if key == entryKey {
			if !force {
				return doc, false, nil
			}
			j = skipJSONSpace(doc, k)
			if j >= len(doc) || doc[j] != ':' {
				return nil, false, errors.New("expected ':' after object key")
			}
			valStart := skipJSONSpace(doc, j+1)
			valEnd, err := skipJSONValue(doc, valStart)
			if err != nil {
				return nil, false, err
			}
			out := append(append([]byte{}, doc[:valStart]...), entry...)
			return append(out, doc[valEnd:]...), true, nil
		}
		j = skipJSONSpace(doc, k)
		if j >= len(doc) || doc[j] != ':' {
			return nil, false, errors.New("expected ':' after object key")
		}
		vEnd, err := skipJSONValue(doc, skipJSONSpace(doc, j+1))
		if err != nil {
			return nil, false, err
		}
		j = skipJSONSpace(doc, vEnd)
		if j < len(doc) && doc[j] == ',' {
			j = skipJSONSpace(doc, j+1)
			continue
		}
		break
	}
	// Pretty form: the first member starts a line; reuse its indentation so
	// the inserted entry matches the file's style.
	lineStart := bytes.LastIndexByte(doc[:firstMember], '\n') + 1
	indent := string(doc[lineStart:firstMember])
	if strings.TrimSpace(indent) != "" {
		// Compact single-line object: `{ "a": 1, ... }`.
		repl := []byte(jsonString(entryKey) + ": " + string(entry) + ", ")
		out := append(append([]byte{}, doc[:objStart+1]...), repl...)
		return append(out, doc[objStart+1:]...), true, nil
	}
	repl := []byte(indent + jsonString(entryKey) + ": " + string(entry) + ",\n")
	out := append(append([]byte{}, doc[:lineStart]...), repl...)
	return append(out, doc[lineStart:]...), true, nil
}

func skipJSONSpace(b []byte, i int) int {
	for i < len(b) {
		switch b[i] {
		case ' ', '\t', '\r', '\n':
			i++
		default:
			return i
		}
	}
	return i
}

// scanJSONString assumes b[i] == '"' and returns the index just past the
// closing quote.
func scanJSONString(b []byte, i int) (int, error) {
	if i >= len(b) || b[i] != '"' {
		return 0, fmt.Errorf("expected string at offset %d", i)
	}
	i++
	for i < len(b) {
		switch b[i] {
		case '\\':
			i += 2
		case '"':
			return i + 1, nil
		default:
			i++
		}
	}
	return 0, errors.New("unterminated string")
}

// skipJSONValue assumes b[i] starts a JSON value and returns the index just
// past its end.
func skipJSONValue(b []byte, i int) (int, error) {
	i = skipJSONSpace(b, i)
	if i >= len(b) {
		return 0, errors.New("unexpected end of document")
	}
	switch b[i] {
	case '"':
		return scanJSONString(b, i)
	case '{', '[':
		open, close := b[i], byte('}')
		if open == '[' {
			close = ']'
		}
		depth := 0
		for i < len(b) {
			switch b[i] {
			case '"':
				j, err := scanJSONString(b, i)
				if err != nil {
					return 0, err
				}
				i = j
				continue
			case open:
				depth++
			case close:
				depth--
				if depth == 0 {
					return i + 1, nil
				}
			}
			i++
		}
		return 0, errors.New("unterminated container")
	default:
		for i < len(b) {
			switch b[i] {
			case ',', '}', ']', ' ', '\t', '\r', '\n':
				return i, nil
			}
			i++
		}
		return i, nil
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
