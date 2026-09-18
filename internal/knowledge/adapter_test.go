package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The reference generator keeps its state outside the pages; importing it is
// what makes those pages attributable at all (方案 §12.6).
func TestRepowikiImport(t *testing.T) {
	root := t.TempDir()
	write(t, root, repowikiStateFile, `{
  "schema": 1,
  "git": {"commit": "abc1234", "branch": "main"},
  "pages": {
    "knowledge/存储/概述.md": {"content_hash": "`+strings.Repeat("ab", 32)+`", "sources": ["internal/storage/**"]},
    "content/总览.md": {"content_hash": "`+strings.Repeat("cd", 32)+`", "sources": ["README.md"]},
    "notes.txt": {"content_hash": "ignored"}
  }
}`)
	imported, err := Repowiki().Import(root)
	if err != nil {
		t.Fatal(err)
	}
	if imported == nil || imported.Baseline.Commit != "abc1234" || imported.Generator != "repowiki" {
		t.Fatalf("imported = %+v", imported)
	}
	entry, ok := imported.Mapping(repowikiBundleRoot + "/knowledge/存储/概述.md")
	if !ok || len(entry.Sources) != 1 || entry.ContentHash != strings.Repeat("ab", 32) {
		t.Fatalf("mapping = %+v", entry)
	}
	if _, ok := imported.Mapping(repowikiBundleRoot + "/notes.txt"); ok {
		t.Fatal("a non-page entry was imported")
	}
	// A generator that has not run yet has nothing to import, and that is fine.
	empty, err := Repowiki().Import(t.TempDir())
	if err != nil || empty != nil {
		t.Fatalf("empty import = %+v, %v", empty, err)
	}
}

// Importing must not overwrite what the layer itself recorded: it fills gaps.
func TestMergeStates(t *testing.T) {
	local := &State{
		SchemaVersion: StateSchemaVersion,
		Baseline:      Baseline{Commit: "local"},
		Pages: map[string]PageState{
			"docs/kb/a.md": {ContentHash: "local-hash"},
		},
		Generator: "stub",
	}
	imported := &State{
		SchemaVersion: StateSchemaVersion,
		Baseline:      Baseline{Commit: "imported", Branch: "main"},
		Pages: map[string]PageState{
			"docs/kb/a.md": {ContentHash: "imported-hash", Sources: []string{"internal/**"}},
			"docs/kb/b.md": {ContentHash: "imported-hash-b", Sources: []string{"cmd/**"}},
		},
		Generator: "repowiki",
	}
	merged := MergeStates(local, imported)
	if merged.Baseline.Commit != "local" || merged.Generator != "stub" {
		t.Fatalf("merged = %+v", merged)
	}
	if entry, _ := merged.Mapping("docs/kb/a.md"); entry.ContentHash != "local-hash" {
		t.Fatalf("local entry lost: %+v", entry)
	}
	if entry, ok := merged.Mapping("docs/kb/b.md"); !ok || len(entry.Sources) != 1 {
		t.Fatalf("imported entry missing: %+v", entry)
	}
	// A missing local state takes the imported one as is.
	if got := MergeStates(nil, imported); got != imported {
		t.Fatal("nil local state did not pass through")
	}
	if got := MergeStates(local, nil); got != local {
		t.Fatal("nil imported state did not pass through")
	}
}

func TestAdapterByName(t *testing.T) {
	if _, ok := AdapterByName("repowiki"); !ok {
		t.Fatal("repowiki adapter not found")
	}
	if _, ok := AdapterByName("REPOWIKI"); !ok {
		t.Fatal("adapter lookup is case sensitive")
	}
	if _, ok := AdapterByName("my-generator --flag"); ok {
		t.Fatal("a command string was treated as an adapter name")
	}
}

// pendingPages decides whether the generator may finalize: an unwritten page
// keeps the baseline where it is.
func TestRepowikiPendingPages(t *testing.T) {
	root := t.TempDir()
	write(t, root, repowikiBundleRoot+"/knowledge/写成.md", "内容\n")
	write(t, root, repowikiBundleRoot+"/knowledge/未写.md", "别的内容\n")
	written, err := HashFile(filepath.Join(root, filepath.FromSlash(repowikiBundleRoot+"/knowledge/写成.md")))
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, repowikiStateFile, `{
  "git": {"commit": "abc1234"},
  "pages": {"knowledge/写成.md": {"content_hash": "`+written+`"}}
}`)
	imported, err := Repowiki().Import(root)
	if err != nil {
		t.Fatal(err)
	}
	adapter := repowikiAdapter{}
	pending, err := adapter.pendingPages(GenerateRequest{
		Root:  root,
		Pages: []string{repowikiBundleRoot + "/knowledge/写成.md", repowikiBundleRoot + "/knowledge/未写.md"},
	}, imported)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || !strings.HasSuffix(pending[0], "未写.md") {
		t.Fatalf("pending = %v", pending)
	}
	// Nothing scoped means nothing to check.
	if pending, err := adapter.pendingPages(GenerateRequest{Root: root}, imported); err != nil || pending != nil {
		t.Fatalf("pending = %v, %v", pending, err)
	}
}

// Probe reports an absent generator as absent; when RepoWiki is installed it
// reports the banner version line instead of failing.
func TestRepowikiProbe(t *testing.T) {
	availability := Repowiki().Probe(t.Context())
	if availability.Installed && !strings.Contains(availability.Detail, "repowiki") {
		t.Fatalf("detail = %q", availability.Detail)
	}
	if !availability.Installed && !strings.Contains(availability.Detail, "PATH") {
		t.Fatalf("detail = %q", availability.Detail)
	}
}

// The adapter is an interop surface: an unreadable or malformed state file is
// an error, not a silent "nothing to import".
func TestRepowikiImportRejectsBrokenState(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".repowiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, root, repowikiStateFile, "{not json")
	if _, err := Repowiki().Import(root); err == nil {
		t.Fatal("broken state was accepted")
	}
}

// The adapter's import is what makes a bundle whose pages keep their sources in
// the generator's state attributable: without it every page is unverifiable.
func TestRepowikiImportMakesPagesAttributable(t *testing.T) {
	root, commit := gitRepo(t, map[string]string{"internal/storage/a.go": "package storage\n"})
	write(t, root, repowikiBundleRoot+"/knowledge/存储/概述.md",
		"---\nstatus: stable\ntype: module\ntriggers:\n  - 存储\ndescription: d\nsource_commit: "+commit+"\n---\n\n# 概述\n")
	pagePath := filepath.Join(root, filepath.FromSlash(repowikiBundleRoot+"/knowledge/存储/概述.md"))
	hash, err := HashFile(pagePath)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, repowikiStateFile, `{
  "git": {"commit": "`+commit+`", "branch": "main"},
  "pages": {"knowledge/存储/概述.md": {"content_hash": "`+hash+`", "sources": ["internal/storage/**"]}}
}`)
	report, err := Scan(root, []string{repowikiBundleRoot + "/knowledge"})
	if err != nil {
		t.Fatal(err)
	}
	imported, err := Repowiki().Import(root)
	if err != nil {
		t.Fatal(err)
	}
	// Without the import the page has no sources: the layer cannot attribute
	// anything to it, and says so instead of calling it fresh.
	plain, err := Evaluate(root, report.Pages, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Unverifiable()) != 1 {
		t.Fatalf("without import: %+v", plain.Pages)
	}
	// With it the page is judged — and it matches its record, so it is fresh.
	merged := MergeStates(nil, imported)
	freshness, err := Evaluate(root, report.Pages, merged)
	if err != nil {
		t.Fatal(err)
	}
	if len(freshness.Unverifiable()) != 0 || len(freshness.Affected()) != 0 {
		t.Fatalf("with import: %+v", freshness.Pages)
	}
	if freshness.Pages[0].Baseline != commit {
		t.Fatalf("baseline = %q, want %q", freshness.Pages[0].Baseline, commit)
	}

	// And the page layer's own state wins where both know a page: an imported
	// mapping must not overwrite what the project recorded.
	local := &State{SchemaVersion: StateSchemaVersion, Pages: map[string]PageState{
		repowikiBundleRoot + "/knowledge/存储/概述.md": {ContentHash: strings.Repeat("ab", 32)},
	}}
	preferred := MergeStates(local, imported)
	if entry, _ := preferred.Mapping(repowikiBundleRoot + "/knowledge/存储/概述.md"); entry.ContentHash != strings.Repeat("ab", 32) {
		t.Fatalf("imported mapping overwrote the project's own: %+v", entry)
	}
}

// Merging fills gaps field by field: a local state that knows a commit but no
// branch still learns the branch, and an entry that knows a hash but no sources
// still learns the sources.
func TestMergeStatesFillsGapsPerField(t *testing.T) {
	local := &State{
		SchemaVersion: StateSchemaVersion,
		Baseline:      Baseline{Commit: "local"},
		Pages:         map[string]PageState{"docs/kb/a.md": {ContentHash: "local-hash"}},
	}
	imported := &State{
		SchemaVersion: StateSchemaVersion,
		Baseline:      Baseline{Commit: "imported", Branch: "main"},
		Pages: map[string]PageState{
			"docs/kb/a.md": {ContentHash: "imported-hash", Sources: []string{"internal/**"}, SourceCommit: "imported"},
		},
	}
	merged := MergeStates(local, imported)
	if merged.Baseline.Commit != "local" || merged.Baseline.Branch != "main" {
		t.Fatalf("baseline = %+v", merged.Baseline)
	}
	entry, _ := merged.Mapping("docs/kb/a.md")
	if entry.ContentHash != "local-hash" || len(entry.Sources) != 1 || entry.SourceCommit != "imported" {
		t.Fatalf("entry = %+v", entry)
	}
}
