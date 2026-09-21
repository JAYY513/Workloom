package knowledge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/storage"
)

// Checkpoints round-trip, and an unknown version is refused rather than guessed
// at (a resume that misreads its own progress would skip pages).
func TestCheckpointRoundTrip(t *testing.T) {
	root := t.TempDir()
	if _, err := LoadCheckpoint(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing checkpoint: %v", err)
	}
	checkpoint := &Checkpoint{
		SchemaVersion: RunSchemaVersion,
		PID:           4242,
		Host:          "host",
		Phase:         PhaseGenerating,
		Mode:          ScopeAffected,
		StartedAt:     time.Unix(0, 0).UTC(),
		UpdatedAt:     time.Unix(1, 0).UTC(),
		Pages:         []string{"a.md", "b.md"},
	}
	if _, err := WriteCheckpoint(root, checkpoint); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCheckpoint(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PID != 4242 || loaded.Phase != PhaseGenerating || len(loaded.Pages) != 2 {
		t.Fatalf("loaded = %+v", loaded)
	}
	if err := ClearCheckpoint(root); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCheckpoint(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleared checkpoint still readable: %v", err)
	}
	// Clearing twice is fine: the run that created it may be the one clearing.
	if err := ClearCheckpoint(root); err != nil {
		t.Fatal(err)
	}
	write(t, root, RunFile, `{"schema_version": 9}`)
	if _, err := LoadCheckpoint(root); err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("err = %v", err)
	}
}

// The mutex is an OS lock: it is exclusive, and it comes back when released.
func TestLockRefreshIsExclusive(t *testing.T) {
	root := t.TempDir()
	release, err := LockRefresh(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockRefresh(root); err == nil {
		t.Fatal("second lock succeeded while the first was held")
	} else if !errors.Is(err, storage.ErrLocked) {
		t.Fatalf("second lock error = %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	release2, err := LockRefresh(root)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	if err := release2(); err != nil {
		t.Fatal(err)
	}
}

// ResumePending keeps the pages that still differ from their record: those are
// the ones the interrupted run had not finished.
func TestResumePending(t *testing.T) {
	root := t.TempDir()
	clean := "---\nstatus: stable\ntype: module\ntriggers:\n  - x\ndescription: d\nsource_commit: abc1234\n---\n\n# 干净\n"
	edited := "---\nstatus: stable\ntype: module\ntriggers:\n  - x\ndescription: d\nsource_commit: abc1234\n---\n\n# 改过\n"
	write(t, root, "pages/clean.md", clean)
	write(t, root, "pages/edited.md", edited)
	write(t, root, "pages/gone.md", clean)

	cleanHash, err := HashFile(filepath.Join(root, "pages", "clean.md"))
	if err != nil {
		t.Fatal(err)
	}
	var pages []*Page
	for _, rel := range []string{"pages/clean.md", "pages/edited.md"} {
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
	state := &State{
		SchemaVersion: StateSchemaVersion,
		Pages: map[string]PageState{
			// clean.md was written before the interruption; edited.md was not
			// (its record still describes different content).
			"pages/clean.md":  {ContentHash: cleanHash},
			"pages/edited.md": {ContentHash: strings.Repeat("0", 64)},
		},
	}
	remaining, err := ResumePending(root, pages, state, []string{"pages/clean.md", "pages/edited.md", "pages/gone.md"})
	if err != nil {
		t.Fatal(err)
	}
	// gone.md is dropped: the page no longer exists, so there is nothing to
	// regenerate under that name.
	if len(remaining) != 1 || remaining[0] != "pages/edited.md" {
		t.Fatalf("remaining = %v", remaining)
	}
}
