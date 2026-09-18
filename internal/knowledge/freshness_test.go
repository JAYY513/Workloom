package knowledge

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitRepo initializes a repository with one commit and returns its head commit.
func gitRepo(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", "base")
	return root, run("rev-parse", "HEAD")
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// renderedPage renders the page fixture the evaluation tests parse.
func renderedPage(commit string, sources ...string) string {
	front := "status: current\ntype: module\ntriggers:\n  - x\ndescription: d\nsource_commit: " + commit + "\n"
	if len(sources) > 0 {
		front += "sources:\n"
		for _, source := range sources {
			front += "  - " + source + "\n"
		}
	}
	return page(front, "\n# 标题\n")
}

// commitAll commits the working tree of an existing fixture repository.
func commitAll(root, message string) (string, error) {
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "--allow-empty", "-m", message}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git %v: %v: %s", args, err, out)
		}
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	return strings.TrimSpace(string(out)), err
}

// mustRead reads a file the fixture wrote.
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestHeadAndChanges(t *testing.T) {
	root, commit := gitRepo(t, map[string]string{"a.go": "package a\n", "docs/x.md": "# x\n"})
	head, branch, err := Head(root)
	if err != nil {
		t.Fatal(err)
	}
	if head != commit || branch != "main" {
		t.Fatalf("head=%s branch=%s, want %s/main", head, branch, commit)
	}
	// A clean tree has no changes; a modification, a deletion and an untracked
	// file all count.
	if changed, err := Changes(root, commit); err != nil || len(changed) != 0 {
		t.Fatalf("clean: %v %v", changed, err)
	}
	write(t, root, "a.go", "package a // edited\n")
	write(t, root, "new.md", "# new\n")
	if err := os.Remove(filepath.Join(root, "docs", "x.md")); err != nil {
		t.Fatal(err)
	}
	changed, err := Changes(root, commit)
	if err != nil {
		t.Fatal(err)
	}
	assertSet(t, changed, []string{"a.go", "docs/x.md", "new.md"})

	// An unknown baseline is an error, not an empty change set.
	if _, err := Changes(root, "0000000000000000000000000000000000000000"); err == nil {
		t.Fatal("unknown baseline did not fail")
	}
	if _, err := Changes(root, ""); err == nil {
		t.Fatal("empty baseline did not fail")
	}
}

func assertSet(t *testing.T, got, want []string) {
	t.Helper()
	have := map[string]bool{}
	for _, item := range got {
		have[item] = true
	}
	for _, item := range want {
		if !have[item] {
			t.Errorf("missing %q in %v", item, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// evaluate builds pages with in-page front matter and runs the verdict.
func pageFor(path string, sources []string, commit string) *Page {
	front := "status: current\ntype: module\ntriggers:\n  - x\ndescription: d\n"
	if commit != "" {
		front += "source_commit: " + commit + "\n"
	} else {
		front += "source_commit: \"\"\n"
	}
	if len(sources) > 0 {
		front += "sources:\n"
		for _, source := range sources {
			front += "  - " + source + "\n"
		}
	}
	page, problems := Parse(path, []byte(page(front, "\n# 标题\n")))
	for _, problem := range problems {
		if problem.Severity != "warning" {
			panic("fixture page has errors: " + problem.String())
		}
	}
	return page
}

// TestEvaluateAffectsOnlyMatchingPages is the M5.3 acceptance: changing one
// source file marks exactly the pages whose sources cover it.
func TestEvaluateAffectsOnlyMatchingPages(t *testing.T) {
	root, commit := gitRepo(t, map[string]string{
		"internal/store/a.go": "package store\n",
		"internal/cli/b.go":   "package cli\n",
		"docs/x.md":           "# x\n",
	})
	pages := []*Page{
		pageFor("docs/repowiki/knowledge/存储/概述.md", []string{"internal/store/**"}, commit),
		pageFor("docs/repowiki/knowledge/CLI/概述.md", []string{"internal/cli/**"}, commit),
	}
	write(t, root, "internal/store/a.go", "package store // edited\n")

	freshness, err := Evaluate(root, pages, nil)
	if err != nil {
		t.Fatal(err)
	}
	affected := freshness.Affected()
	if len(affected) != 1 || !strings.HasSuffix(affected[0], "存储/概述.md") {
		t.Fatalf("affected = %v", affected)
	}
	for _, page := range freshness.Pages {
		if strings.HasSuffix(page.Path, "CLI/概述.md") && page.Stale {
			t.Fatalf("unrelated page is stale: %+v", page)
		}
	}
	if freshness.ChangedFiles != 1 {
		t.Fatalf("changed files = %d", freshness.ChangedFiles)
	}
}

func TestEvaluateUsesMappingAndLayerBaseline(t *testing.T) {
	root, commit := gitRepo(t, map[string]string{"internal/store/a.go": "package store\n"})
	// The page declares neither sources nor a commit: both come from the
	// generator's mapping / layer baseline (方案 §12.5).
	pageUnderTest := pageFor("docs/repowiki/knowledge/存储/概述.md", nil, "")
	state := &State{
		SchemaVersion: StateSchemaVersion,
		Baseline:      Baseline{Commit: commit, Branch: "main"},
		Pages: map[string]PageState{
			"docs/repowiki/knowledge/存储/概述.md": {Sources: []string{"internal/store/**"}},
		},
	}
	write(t, root, "internal/store/a.go", "package store // edited\n")
	freshness, err := Evaluate(root, []*Page{pageUnderTest}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(freshness.Affected()) != 1 {
		t.Fatalf("affected = %v (pages %+v)", freshness.Affected(), freshness.Pages)
	}
	if freshness.Pages[0].Baseline != commit {
		t.Fatalf("baseline = %q, want %q", freshness.Pages[0].Baseline, commit)
	}
}

// A page the layer cannot attribute or date is reported as unverifiable, never
// as fresh.
func TestEvaluateUnverifiable(t *testing.T) {
	root, commit := gitRepo(t, map[string]string{"a.go": "package a\n"})
	pages := []*Page{
		pageFor("docs/kb/no-sources.md", nil, commit),
		pageFor("docs/kb/no-baseline.md", []string{"a.go"}, ""),
		pageFor("docs/kb/fine.md", []string{"a.go"}, commit),
	}
	freshness, err := Evaluate(root, pages, nil)
	if err != nil {
		t.Fatal(err)
	}
	unverifiable := freshness.Unverifiable()
	if len(unverifiable) != 2 {
		t.Fatalf("unverifiable = %v (pages %+v)", unverifiable, freshness.Pages)
	}
	if len(freshness.Affected()) != 0 {
		t.Fatalf("affected = %v", freshness.Affected())
	}
}

// Git failures must surface: no repository means no verdict.
func TestEvaluateWithoutGitFails(t *testing.T) {
	root := t.TempDir()
	pages := []*Page{pageFor("docs/kb/x.md", []string{"a.go"}, "abc1234")}
	if _, err := Evaluate(root, pages, nil); err == nil || !strings.Contains(err.Error(), ErrNoGit.Error()) {
		t.Fatalf("err = %v", err)
	}
}

func TestStateRoundTrip(t *testing.T) {
	root := t.TempDir()
	if _, err := LoadState(root); !MissingState(err) {
		t.Fatalf("missing state: %v", err)
	}
	state := &State{
		SchemaVersion: StateSchemaVersion,
		Baseline:      Baseline{Commit: "abc1234", Branch: "main"},
		Pages:         map[string]PageState{"a.md": {Sources: []string{"internal/**"}, ContentHash: strings.Repeat("ab", 32)}},
		Generator:     "repowiki-gen",
		UpdatedAt:     time.Unix(0, 0).UTC(),
	}
	if _, err := WriteState(root, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := loaded.Mapping("a.md")
	if !ok || len(entry.Sources) != 1 || loaded.Baseline.Commit != "abc1234" || loaded.Generator != "repowiki-gen" {
		t.Fatalf("loaded = %+v", loaded)
	}
	if _, ok := loaded.Mapping("missing.md"); ok {
		t.Fatal("unexpected mapping entry")
	}
}

func TestLoadStateRejectsUnknownVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(Dir)), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, root, StateFile, `{"schema_version": 7, "baseline": {"commit": "x"}}`)
	if _, err := LoadState(root); err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("err = %v", err)
	}
}

// A page whose content matches its record is current as of the layer baseline:
// the in-page source_commit a generator writes lags behind the baseline it
// finalizes at, and judging the page by the older commit would call a page that
// was just regenerated stale.
func TestEvaluatePrefersLayerBaselineForMatchingContent(t *testing.T) {
	root, older := gitRepo(t, map[string]string{"internal/store/a.go": "package store\n"})
	pagePath := "docs/repowiki/knowledge/存储/概述.md"
	write(t, root, pagePath, renderedPage(older, "internal/store/**"))
	// The generator regenerated the page after a later commit and recorded the
	// content it wrote; the page's own source_commit still names the older one.
	write(t, root, "internal/store/a.go", "package store // newer\n")
	committed, err := commitAll(root, "newer")
	if err != nil {
		t.Fatal(err)
	}
	current, err := HashFile(filepath.Join(root, filepath.FromSlash(pagePath)))
	if err != nil {
		t.Fatal(err)
	}
	state := &State{
		SchemaVersion: StateSchemaVersion,
		Baseline:      Baseline{Commit: committed},
		Pages:         map[string]PageState{pagePath: {ContentHash: current}},
	}
	parsed, problems := Parse(pagePath, mustRead(t, filepath.Join(root, filepath.FromSlash(pagePath))))
	for _, problem := range problems {
		if problem.Severity != "warning" {
			t.Fatalf("fixture page: %s", problem.String())
		}
	}
	freshness, err := Evaluate(root, []*Page{parsed}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(freshness.Affected()) != 0 || freshness.Pages[0].Baseline != committed {
		t.Fatalf("verdict = %+v, want fresh against the layer baseline", freshness.Pages[0])
	}
}
