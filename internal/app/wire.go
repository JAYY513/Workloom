package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// The managed block markers. Only the text between them is ours; everything
// else in the file — including other tools' managed blocks — is preserved
// byte for byte (方案 §12.5).
const (
	wireBegin = "<!-- devsys:begin | managed by `devsys wire`; edits inside this block are overwritten -->"
	wireEnd   = "<!-- devsys:end -->"
)

// wireBlock is the discipline the operator publishes into AGENTS.md: where
// project state lives and how to reach it. It is deliberately factual — the
// same description the MCP server gives of itself, plus the entry points an
// agent needs.
const wireBlock = wireBegin + `
## devsys（项目状态与读取纪律）

- 项目状态保存在仓库内 ` + "`.devsys/`" + `，它是唯一事实来源；不要直接编辑受管文件（修复请用 ` + "`devsys repair`" + `）。
- 查询与变更通过 CLI（` + "`devsys …`" + `）或 MCP（` + "`devsys mcp serve`" + `，工作目录 = 项目根）；两者共用同一应用服务，约束一致。
- 先读后写：写操作携带版本哈希（` + "`--expect`" + ` / 工具的 ` + "`expect`" + `），过期哈希一律拒绝。
- 入口：` + "`devsys session start`" + ` 一次给出项目状态与下一步；` + "`devsys next`" + ` 给出就绪判定与推荐动作；` + "`devsys project status`" + ` 给出计数与风险。
- 结构化输出：` + "`--json`" + `（单文档信封）或 ` + "`--jsonl`" + `（列表一行一条记录）；退出码 0/1/2/3/4（10/11 预留给知识层）。
` + wireEnd

// WireView reports what `devsys wire` did (or would do).
type WireView struct {
	Path    string `json:"path"`
	Changed bool   `json:"changed"`
	Created bool   `json:"created"`
	Diff    string `json:"diff,omitempty"`
}

// Wire injects the managed block into the repository's AGENTS.md,
// idempotently: running it three times leaves the file byte-identical, and
// content outside the block is never touched. With dryRun it reports the
// diff instead of writing.
func (s *Service) Wire(ctx context.Context, dryRun bool) (WireView, error) {
	path := filepath.Join(s.Root, "AGENTS.md")
	view := WireView{Path: path}

	existing, mode, err := readAgentsFile(path)
	switch {
	case err == nil:
	case os.IsNotExist(err):
		view.Created = true
		mode = 0o644
	default:
		return WireView{}, Internalf("read %s: %v", path, err)
	}

	updated, changed, err := spliceWireBlock(string(existing))
	if err != nil {
		return WireView{}, err
	}
	view.Changed = changed
	if !changed {
		return view, nil
	}
	view.Diff = unifiedDiff(string(existing), updated)
	if dryRun {
		return view, nil
	}
	if err := writeFileAtomic(path, []byte(updated), mode); err != nil {
		return WireView{}, Internalf("write %s: %v", path, err)
	}
	return view, nil
}

// readAgentsFile reads AGENTS.md together with its mode, so a rewrite never
// widens or narrows the permissions the operator set.
func readAgentsFile(path string) ([]byte, os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	return data, info.Mode().Perm(), nil
}

// spliceWireBlock returns the file content with exactly one managed block.
// Markers are only recognised as whole lines outside fenced code blocks, so
// an example that quotes them is never mistaken for the real block; a
// malformed or ambiguous layout is a located refusal, because guessing which
// half is ours could destroy hand-written content.
func spliceWireBlock(content string) (string, bool, error) {
	eol := "\n"
	if strings.Contains(content, "\r\n") {
		eol = "\r\n"
	}
	lines := splitLines(content)
	beginAt, endAt := -1, -1
	fence := false
	for i, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		if strings.HasPrefix(strings.TrimSpace(trimmed), "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		switch trimmed {
		case wireBegin:
			if beginAt != -1 {
				return "", false, Invalidf(KindInvalid, nil,
					"AGENTS.md carries more than one devsys begin marker; fix or remove the extra block and rerun `devsys wire`")
			}
			beginAt = i
		case wireEnd:
			if endAt != -1 {
				return "", false, Invalidf(KindInvalid, nil,
					"AGENTS.md carries more than one devsys end marker; fix or remove the extra block and rerun `devsys wire`")
			}
			endAt = i
		}
	}
	blockLines := splitLines(wireBlock)

	switch {
	case beginAt == -1 && endAt == -1:
		out := trimTrailingBlank(lines)
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, blockLines...)
		return joinLines(out, eol), true, nil
	case beginAt == -1 || endAt == -1:
		return "", false, Invalidf(KindInvalid, nil,
			"AGENTS.md carries a malformed devsys block (begin marker without end, or the reverse); fix or remove it and rerun `devsys wire`")
	case endAt < beginAt:
		return "", false, Invalidf(KindInvalid, nil,
			"AGENTS.md has the devsys end marker before its begin marker; fix or remove it and rerun `devsys wire`")
	}
	if equalLines(lines[beginAt:endAt+1], blockLines) {
		return content, false, nil
	}
	out := append([]string{}, lines[:beginAt]...)
	out = append(out, blockLines...)
	out = append(out, lines[endAt+1:]...)
	return joinLines(trimTrailingBlank(out), eol), true, nil
}

// splitLines splits on either line ending and drops the terminators.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// joinLines writes the lines back with one consistent line ending and a
// single trailing newline.
func joinLines(lines []string, eol string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, eol) + eol
}

func trimTrailingBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// unifiedDiff renders a preview of the change: the common prefix and suffix
// are trimmed, so only the region that actually changes is shown.
func unifiedDiff(before, after string) string {
	if before == after {
		return ""
	}
	var b strings.Builder
	b.WriteString("--- AGENTS.md (current)\n+++ AGENTS.md (wired)\n")
	beforeLines := splitLines(before)
	afterLines := splitLines(after)
	prefix := 0
	for prefix < len(beforeLines) && prefix < len(afterLines) && beforeLines[prefix] == afterLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(beforeLines)-prefix && suffix < len(afterLines)-prefix &&
		beforeLines[len(beforeLines)-1-suffix] == afterLines[len(afterLines)-1-suffix] {
		suffix++
	}
	for _, line := range beforeLines[prefix : len(beforeLines)-suffix] {
		b.WriteString("-" + line + "\n")
	}
	for _, line := range afterLines[prefix : len(afterLines)-suffix] {
		b.WriteString("+" + line + "\n")
	}
	return b.String()
}

// writeFileAtomic replaces a repository file without ever leaving a partial
// write behind, keeping the mode the file had.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".devsys-wire-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
