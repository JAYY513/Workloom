package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// staleViewFixture builds a project whose page covers internal/store/**,
// committed at base — every M7.4 freshness test starts from here.
func staleViewFixture(t *testing.T) (root, pageRel string) {
	t.Helper()
	root, _ = gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	pageRel = "docs/repowiki/knowledge/存储.md"
	writePage(t, root, pageRel, knowledgePage(base, "internal/store/**"))
	gitCommitAll(t, root, "page")
	return root, pageRel
}

// TestWorkspaceViewStaleHint is the M7.4 acceptance: touching a file a page
// covers makes the view say stale, name the page, and point at refresh —
// while the code stays 0 (10/11 belong to `knowledge status`, not the view).
func TestWorkspaceViewStaleHint(t *testing.T) {
	root, pageRel := staleViewFixture(t)
	writePage(t, root, "internal/store/a.go", "package store // edited\n")

	code, out, errOut := run(t, "workspace", "view")
	if code != CodeOK {
		t.Fatalf("workspace view: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{
		"knowledge: stale",
		"affected: " + pageRel,
		"hint: run `devsys knowledge refresh` to regenerate affected pages",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout misses %q:\n%s", want, out)
		}
	}

	// Same state through `knowledge status` exits 10: one verdict, two faces.
	if code, _, _ := run(t, "knowledge", "status"); code != CodeStale {
		t.Errorf("knowledge status: code=%d, want %d", code, CodeStale)
	}
}

// TestWorkspaceViewFreshHasNoHint: a fresh layer gets no hint line.
func TestWorkspaceViewFreshHasNoHint(t *testing.T) {
	staleViewFixture(t)

	code, out, errOut := run(t, "workspace", "view")
	if code != CodeOK {
		t.Fatalf("workspace view: code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "knowledge: fresh") {
		t.Fatalf("stdout = %q, want fresh", out)
	}
	if strings.Contains(out, "hint:") || strings.Contains(out, "affected:") {
		t.Errorf("fresh view should carry no hint:\n%s", out)
	}
}

// TestWorkspaceViewUnverifiableHint: a page nobody can attribute is stale
// ("cannot tell" is not fresh) and points at refresh too — refresh
// regenerates affected + unverifiable pages (app/knowledge.go candidates).
func TestWorkspaceViewUnverifiableHint(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	pageRel := "docs/repowiki/knowledge/无来源.md"
	writePage(t, root, pageRel, knowledgePage(base))
	gitCommitAll(t, root, "page")

	code, out, errOut := run(t, "workspace", "view")
	if code != CodeOK {
		t.Fatalf("workspace view: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{
		"knowledge: stale",
		"unverifiable: " + pageRel,
		"hint: run `devsys knowledge refresh` to regenerate affected pages",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout misses %q:\n%s", want, out)
		}
	}
}

// TestWorkspaceViewMissingHint: no page layer names the supported
// degradation and the --full command that builds it.
func TestWorkspaceViewMissingHint(t *testing.T) {
	root, _ := staleViewFixture(t)
	if err := os.RemoveAll(filepath.Join(root, "docs", "repowiki")); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := run(t, "workspace", "view")
	if code != CodeOK {
		t.Fatalf("workspace view: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{
		"knowledge: missing",
		"hint: configure `knowledge_generator`, then run `devsys knowledge refresh --full`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout misses %q:\n%s", want, out)
		}
	}
}

// TestWorkspaceViewPendingDoctorHint: pending transactions keep the
// knowledge section quiet about refresh (nothing trustworthy to
// regenerate from) and point at doctor before recover instead.
func TestWorkspaceViewPendingDoctorHint(t *testing.T) {
	root, _ := staleViewFixture(t)
	if err := os.MkdirAll(filepath.Join(root, ".devsys", "local", "txn", "txn-1"), 0o755); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := run(t, "workspace", "view")
	if code != CodeOK {
		t.Fatalf("workspace view: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{
		"pending: txn-1",
		"hint: run `devsys doctor` to inspect before `devsys recover`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hint: run `devsys knowledge refresh`") {
		t.Errorf("pending view must not suggest refresh:\n%s", out)
	}
	if strings.Contains(out, "hint: run `devsys knowledge status`") {
		t.Errorf("pending view needs no status hint (trust already says recover):\n%s", out)
	}
}
