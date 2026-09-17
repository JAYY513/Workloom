package project

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}

func insideRepo(t *testing.T, dir string) bool {
	t.Helper()
	_, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	return err == nil
}

func snapshot(t *testing.T, root string) map[string][]byte {
	t.Helper()
	snap := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		snap[filepath.ToSlash(rel)] = b
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func assertSameSnapshot(t *testing.T, before, after map[string][]byte) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("file count changed: %d -> %d", len(before), len(after))
	}
	for rel, want := range before {
		got, ok := after[rel]
		if !ok {
			t.Errorf("missing file after second run: %s", rel)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("file changed by second run: %s", rel)
		}
	}
}

func TestInitCreatesLayoutIdempotently(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "TempMonitorSystem")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	devsys := filepath.Join(repo, DevsysDirName)

	res1, err := Init(repo, Options{Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	if res1.ID != "tempmonitorsystem" || res1.Name != "TempMonitorSystem" {
		t.Errorf("id/name = %q/%q", res1.ID, res1.Name)
	}
	if len(res1.Created) == 0 {
		t.Fatal("expected created entries on first run")
	}

	for _, rel := range []string{
		"project.yaml", "config.yaml", "state/current.yaml", "state/milestones.yaml", ".gitignore",
	} {
		if _, err := os.Stat(filepath.Join(devsys, filepath.FromSlash(rel))); err != nil {
			t.Errorf("missing file %s: %v", rel, err)
		}
	}
	for _, d := range layoutDirs {
		fi, err := os.Stat(filepath.Join(devsys, d))
		if err != nil || !fi.IsDir() {
			t.Errorf("missing directory %s: %v", d, err)
		}
	}

	gi, err := os.ReadFile(filepath.Join(devsys, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ignoreEntries {
		if !strings.Contains(string(gi), e) {
			t.Errorf(".gitignore missing %q: %s", e, gi)
		}
	}
	pj, err := os.ReadFile(filepath.Join(devsys, "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"schema_version: 1", "id: 'tempmonitorsystem'", "name: 'TempMonitorSystem'", "created_at: 2026-09-17T09:00:00Z"} {
		if !strings.Contains(string(pj), want) {
			t.Errorf("project.yaml missing %q:\n%s", want, pj)
		}
	}

	before := snapshot(t, devsys)
	res2, err := Init(repo, Options{Now: fixedNow.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Created) != 0 {
		t.Errorf("second run created %v", res2.Created)
	}
	assertSameSnapshot(t, before, snapshot(t, devsys))
}

func TestInitPreservesUserEdits(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	if _, err := Init(repo, Options{Now: fixedNow}); err != nil {
		t.Fatal(err)
	}
	devsys := filepath.Join(repo, DevsysDirName)

	custom := "schema_version: 1\nsummary: 'hand edited'\n"
	current := filepath.Join(devsys, "state", "current.yaml")
	if err := os.WriteFile(current, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	giPath := filepath.Join(devsys, ".gitignore")
	if err := os.WriteFile(giPath, []byte("# user line\nkeep-me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Init(repo, Options{Now: fixedNow}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(current); string(got) != custom {
		t.Errorf("user edit overwritten:\n%s", got)
	}
	gi, _ := os.ReadFile(giPath)
	if !strings.Contains(string(gi), "keep-me") {
		t.Errorf("user .gitignore line lost: %s", gi)
	}
	for _, e := range ignoreEntries {
		if !strings.Contains(string(gi), e) {
			t.Errorf(".gitignore missing %q after top-up: %s", e, gi)
		}
	}
	if strings.Count(string(gi), "local/") != 1 || strings.Count(string(gi), ".cache/") != 1 {
		t.Errorf(".gitignore entries duplicated:\n%s", gi)
	}
}

func TestInitRejectsNonRepository(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	if insideRepo(t, dir) {
		t.Skipf("temp dir %s is inside a repository", dir)
	}
	_, err := Init(dir, Options{})
	var pe *PreconditionError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want PreconditionError", err)
	}
	if !strings.Contains(pe.Msg, "git init") {
		t.Errorf("message should mention git init: %s", pe.Msg)
	}
	if _, statErr := os.Stat(filepath.Join(dir, DevsysDirName)); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatal("precondition failure must not create state")
	}
}

func TestInitRejectsRepositorySubdirectory(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Init(sub, Options{})
	var pe *PreconditionError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want PreconditionError", err)
	}
	if !strings.Contains(pe.Msg, "repository root") {
		t.Errorf("message = %s", pe.Msg)
	}
	if _, statErr := os.Stat(filepath.Join(sub, DevsysDirName)); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatal("precondition failure must not create state in the subdirectory")
	}
	if _, statErr := os.Stat(filepath.Join(repo, DevsysDirName)); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatal("precondition failure must not create state in the repository root either")
	}
}
