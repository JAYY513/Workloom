package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// protectionFixture writes three pages: one locked, one hand-edited, one as
// the generator left it.
func protectionFixture(t *testing.T) (string, []*Page, *State) {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	clean := "---\nstatus: stable\ntype: module\ntriggers:\n  - x\ndescription: d\nsource_commit: abc1234\nsources:\n  - internal/**\n---\n\n# 干净页\n"
	locked := "---\nstatus: stable\ntype: module\ntriggers:\n  - x\ndescription: d\nsource_commit: abc1234\nsources:\n  - internal/**\nprotected: true\n---\n\n# 锁定页\n"
	edited := clean + "\n人工补充的一段。\n"
	write("pages/clean.md", clean)
	write("pages/locked.md", locked)
	write("pages/edited.md", edited)

	var pages []*Page
	for _, rel := range []string{"pages/clean.md", "pages/locked.md", "pages/edited.md"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		page, problems := Parse(rel, data)
		for _, problem := range problems {
			if problem.Severity != "warning" {
				t.Fatalf("%s: %s", rel, problem.String())
			}
		}
		pages = append(pages, page)
	}
	// The clean page's hash is recorded as it is; the edited page's record is
	// what it looked like before the hand edit.
	cleanHash, err := HashFile(filepath.Join(root, "pages", "clean.md"))
	if err != nil {
		t.Fatal(err)
	}
	editedHash, err := HashFile(filepath.Join(root, "pages", "edited.md"))
	if err != nil {
		t.Fatal(err)
	}
	state := &State{
		SchemaVersion: StateSchemaVersion,
		Baseline:      Baseline{Commit: "abc1234"},
		Pages: map[string]PageState{
			"pages/clean.md":  {ContentHash: cleanHash},
			"pages/edited.md": {ContentHash: strings.Repeat("0", 64)},
		},
	}
	if state.Pages["pages/edited.md"].ContentHash == editedHash {
		t.Fatal("fixture is not actually edited")
	}
	return root, pages, state
}

func TestSkipsReportsProtectionAndHandEdits(t *testing.T) {
	root, pages, state := protectionFixture(t)
	skips, err := Skips(root, pages, state, false)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]Skip{}
	for _, skip := range skips {
		byPath[skip.Path] = skip
	}
	if len(skips) != 2 {
		t.Fatalf("skips = %+v", skips)
	}
	if !strings.Contains(byPath["pages/locked.md"].Reason, "protected") {
		t.Errorf("locked reason = %q", byPath["pages/locked.md"].Reason)
	}
	edited, ok := byPath["pages/edited.md"]
	if !ok || !strings.Contains(edited.Reason, "手工修改") {
		t.Fatalf("edited skip = %+v", edited)
	}
	if edited.Recorded == "" || edited.Actual == "" || edited.Recorded == edited.Actual {
		t.Errorf("edited hashes = %q / %q", edited.Recorded, edited.Actual)
	}
	if _, ok := byPath["pages/clean.md"]; ok {
		t.Error("a page that matches its record was skipped")
	}

	// --force regenerates them, and still says which pages it overrode.
	forced, err := Skips(root, pages, state, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(forced) != 2 {
		t.Fatalf("forced = %+v", forced)
	}
	for _, skip := range forced {
		if !skip.Overridden {
			t.Errorf("%s was not marked as overridden", skip.Path)
		}
	}
}

// An in-page hash wins over the mapping, and a page with no recorded hash is
// simply not protected: there is no claim to compare.
func TestSkipsSources(t *testing.T) {
	root, pages, _ := protectionFixture(t)
	cleanHash, err := HashFile(filepath.Join(root, "pages", "clean.md"))
	if err != nil {
		t.Fatal(err)
	}
	state := &State{
		SchemaVersion: StateSchemaVersion,
		Pages: map[string]PageState{
			// The mapping disagrees with the file; the page itself records the
			// correct value, and the page is what the contract says wins.
			"pages/clean.md": {ContentHash: strings.Repeat("f", 64)},
		},
	}
	pages[0].ContentHash = cleanHash
	skips, err := Skips(root, pages[:1], state, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(skips) != 0 {
		// The mapping's stale hash must not win over the page's own record.
		t.Fatalf("skips = %+v", skips)
	}

	pages[0].ContentHash = ""
	skips, err = Skips(root, pages[:1], nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(skips) != 0 {
		t.Fatalf("skips = %+v", skips)
	}
}
