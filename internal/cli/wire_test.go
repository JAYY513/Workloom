package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	wireMarkerBegin = "<!-- devsys:begin | managed by `devsys wire`; edits inside this block are overwritten -->"
	wireMarkerEnd   = "<!-- devsys:end -->"
)

func readAgents(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	return string(data)
}

// TestWireIsIdempotentAndPreservesHandwritten pins the M4.6 acceptance:
// three runs leave the file byte-identical, and hand-written content —
// including another tool's managed block — survives verbatim.
func TestWireIsIdempotentAndPreservesHandwritten(t *testing.T) {
	repo, _ := gatedProject(t)
	handwritten := "# 项目说明（手写）\n\n这段内容不属于任何管理块，必须原样保留。\n\n" +
		"<!-- repowiki:begin | 由 repowiki 管理 -->\n## repowiki\n\n- docs/repowiki/ — 自动生成的项目知识\n\n<!-- repowiki:end -->\n\n## 手写小节\n\n- 保留我\n"
	path := filepath.Join(repo, "AGENTS.md")
	if err := os.WriteFile(path, []byte(handwritten), 0o644); err != nil {
		t.Fatal(err)
	}

	if code, _, errOut := run(t, "wire"); code != CodeOK {
		t.Fatalf("wire: code=%d stderr=%q", code, errOut)
	}
	first := readAgents(t, repo)
	if !strings.Contains(first, "这段内容不属于任何管理块，必须原样保留。") ||
		!strings.Contains(first, "<!-- repowiki:begin") || !strings.Contains(first, "- 保留我") {
		t.Fatalf("hand-written content was not preserved:\n%s", first)
	}
	if strings.Count(first, "<!-- devsys:begin") != 1 || strings.Count(first, "<!-- devsys:end -->") != 1 {
		t.Fatalf("expected exactly one devsys block:\n%s", first)
	}

	for i := 0; i < 2; i++ {
		code, out, errOut := run(t, "wire")
		if code != CodeOK {
			t.Fatalf("wire run %d: code=%d stderr=%q", i+2, code, errOut)
		}
		if !strings.Contains(out, "already wired") {
			t.Fatalf("wire run %d did not report a no-op: %q", i+2, out)
		}
		if got := readAgents(t, repo); got != first {
			t.Fatalf("wire run %d changed the file:\n--- before\n%s\n--- after\n%s", i+2, first, got)
		}
	}
}

// TestWireDryRunPreviewsWithoutWriting checks the diff preview.
func TestWireDryRunPreviewsWithoutWriting(t *testing.T) {
	repo, _ := gatedProject(t)
	code, out, errOut := run(t, "wire", "--dry-run")
	if code != CodeOK {
		t.Fatalf("wire --dry-run: code=%d stderr=%q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(repo, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("dry run created the file (err=%v)", err)
	}
	if !strings.Contains(out, "+++ AGENTS.md") || !strings.Contains(out, "+<!-- devsys:begin") {
		t.Fatalf("dry run lacks a diff preview: %q", out)
	}
}

// TestWireRejectsMalformedBlock refuses to guess when the block is broken.
func TestWireRejectsMalformedBlock(t *testing.T) {
	repo, _ := gatedProject(t)
	broken := "# 手写\n\n<!-- devsys:begin | managed by `devsys wire`; edits inside this block are overwritten -->\n没有结束标记\n"
	path := filepath.Join(repo, "AGENTS.md")
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "wire")
	if code != CodeInvalid || !strings.Contains(errOut, "malformed devsys block") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if got := readAgents(t, repo); got != broken {
		t.Fatalf("refusal modified the file:\n%s", got)
	}
}

// TestWireIgnoresQuotedMarkers checks that a fenced example quoting the
// markers is not mistaken for the real block.
func TestWireIgnoresQuotedMarkers(t *testing.T) {
	repo, _ := gatedProject(t)
	quoted := "# 手写\n\n```markdown\n" + wireMarkerBegin + "\n示例正文\n" + wireMarkerEnd + "\n```\n\n结尾段落。\n"
	path := filepath.Join(repo, "AGENTS.md")
	if err := os.WriteFile(path, []byte(quoted), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := run(t, "wire"); code != CodeOK {
		t.Fatalf("wire: code=%d stderr=%q", code, errOut)
	}
	got := readAgents(t, repo)
	if !strings.Contains(got, "示例正文") || !strings.Contains(got, "结尾段落。") {
		t.Fatalf("quoted example was destroyed:\n%s", got)
	}
	if strings.Count(got, wireMarkerBegin) != 2 { // the quoted one plus ours
		t.Fatalf("expected the quoted marker and exactly one managed block:\n%s", got)
	}
}

// TestWireKeepsLineEndingsAndMode checks CRLF files stay CRLF-consistent and
// the file mode is preserved.
func TestWireKeepsLineEndingsAndMode(t *testing.T) {
	repo, _ := gatedProject(t)
	path := filepath.Join(repo, "AGENTS.md")
	if err := os.WriteFile(path, []byte("# 手写\r\n\r\n段落。\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := run(t, "wire"); code != CodeOK {
		t.Fatalf("wire: code=%d stderr=%q", code, errOut)
	}
	got := readAgents(t, repo)
	if strings.Contains(got, "\n\n") && !strings.Contains(got, "\r\n\r\n") {
		t.Fatalf("line endings were mixed:\n%q", got)
	}
	if !strings.Contains(got, "段落。\r\n") {
		t.Fatalf("hand-written CRLF content changed:\n%q", got)
	}
	// Windows has no Unix permission bits: os.Chmod only toggles the
	// read-only attribute there, so the mode assertion is POSIX-only.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o, want 600", info.Mode().Perm())
		}
	}
	// Second run is a no-op (byte-identical).
	before := readAgents(t, repo)
	if code, _, _ := run(t, "wire"); code != CodeOK {
		t.Fatal("second wire failed")
	}
	if after := readAgents(t, repo); after != before {
		t.Fatalf("second wire changed the file:\n--- before\n%q\n--- after\n%q", before, after)
	}
}
