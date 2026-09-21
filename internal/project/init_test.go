package project

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/config"
	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/storage"
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
	var parsed domain.Project
	if err := storage.DecodeYAML(pj, &parsed); err != nil {
		t.Fatalf("project.yaml does not parse: %v\n%s", err, pj)
	}
	if parsed.SchemaVersion != 1 || parsed.ID != "tempmonitorsystem" || parsed.Name != "TempMonitorSystem" {
		t.Errorf("project.yaml = %+v", parsed)
	}
	if !parsed.CreatedAt.Equal(fixedNow) {
		t.Errorf("created_at = %q", parsed.CreatedAt)
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

func TestInitCreatesLayoutWithoutRepository(t *testing.T) {
	dir := t.TempDir()
	if parent, ok := nestedUnderDevsys(dir); ok {
		t.Skipf("temp dir nested under project %s", parent)
	}
	res, err := Init(dir, Options{Now: fixedNow})
	if err != nil {
		t.Fatalf("init without git: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, DevsysDirName, "project.yaml")); err != nil {
		t.Fatalf("layout missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("init must not run git init")
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitattributes")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("non-git init must not write .gitattributes")
	}
	if res.Root == "" || res.ID == "" {
		t.Fatalf("result = %+v", res)
	}
}

func TestInitAllowsRepositorySubdirectory(t *testing.T) {
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

	res, err := Init(sub, Options{Now: fixedNow})
	if err != nil {
		t.Fatalf("init in git subdirectory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sub, DevsysDirName, "project.yaml")); err != nil {
		t.Fatalf("subdirectory layout missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, DevsysDirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("parent git root must not receive .devsys/")
	}
	if _, err := os.Stat(filepath.Join(repo, ".gitattributes")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("parent git root must not receive .gitattributes")
	}
	if _, err := os.Stat(filepath.Join(sub, ".gitattributes")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("git subdirectory must not write .gitattributes")
	}
	if res.Name != "sub" {
		t.Errorf("name = %q, want sub", res.Name)
	}
}

func TestInitRejectsNestedDevsys(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "parent")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if parentRoot, ok := nestedUnderDevsys(parent); ok {
		t.Skipf("temp dir nested under project %s", parentRoot)
	}
	if _, err := Init(parent, Options{Now: fixedNow}); err != nil {
		t.Fatalf("parent init: %v", err)
	}
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, child)
	parentBefore := snapshot(t, filepath.Join(parent, DevsysDirName))
	_, err := Init(child, Options{})
	var pe *PreconditionError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want PreconditionError", err)
	}
	if !strings.Contains(pe.Msg, parent) || !strings.Contains(pe.Msg, DevsysDirName) {
		t.Errorf("message = %s", pe.Msg)
	}
	assertSameSnapshot(t, before, snapshot(t, child))
	assertSameSnapshot(t, parentBefore, snapshot(t, filepath.Join(parent, DevsysDirName)))
	if _, err := os.Stat(filepath.Join(child, DevsysDirName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("nested init must not create .devsys/")
	}
}

func TestInitSucceedsWithoutGitOnPATH(t *testing.T) {
	dir := t.TempDir()
	if parent, ok := nestedUnderDevsys(dir); ok {
		t.Skipf("temp dir nested under project %s", parent)
	}
	t.Setenv("PATH", filepath.Join(t.TempDir(), "empty-bin"))
	if _, err := Init(dir, Options{Now: fixedNow}); err != nil {
		t.Fatalf("init with empty PATH: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, DevsysDirName, "project.yaml")); err != nil {
		t.Fatalf("layout missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("init must not run git init")
	}
}

func TestInitRefusesInvalidMetadata(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "BrokenProj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	devsys := filepath.Join(repo, DevsysDirName)

	if _, err := Init(repo, Options{Now: fixedNow}); err != nil {
		t.Fatal(err)
	}
	// An unsupported schema_version plus a missing directory init would
	// otherwise recreate: the write must be refused before any change.
	broken := []byte("# c\nschema_version: 99\nid: broken\nname: broken\n")
	if err := os.WriteFile(filepath.Join(devsys, "project.yaml"), broken, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(devsys, "knowledge")); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, devsys)

	_, err := Init(repo, Options{Now: fixedNow})
	var problems config.Problems
	if !errors.As(err, &problems) {
		t.Fatalf("err = %v (%T), want config.Problems", err, err)
	}
	problem := problems.Error()
	if len(problems) != 1 || !strings.Contains(problem, "unsupported version 99") ||
		!strings.Contains(problem, "migration must be explicit") {
		t.Errorf("problems = %s", problem)
	}
	if _, statErr := os.Stat(filepath.Join(devsys, "knowledge")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Error("init must not create anything while metadata is invalid")
	}
	assertSameSnapshot(t, before, snapshot(t, devsys))
}

func TestInitRecreatesMissingManagedFiles(t *testing.T) {
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "PartialProj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	devsys := filepath.Join(repo, DevsysDirName)

	if _, err := Init(repo, Options{Now: fixedNow}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(devsys, "state", "current.yaml")); err != nil {
		t.Fatal(err)
	}

	// A missing managed file is not an invalid state: init creates it.
	res, err := Init(repo, Options{Now: fixedNow})
	if err != nil {
		t.Fatalf("init over a missing managed file: %v", err)
	}
	want := DevsysDirName + "/state/current.yaml"
	if !slices.Contains(res.Created, want) {
		t.Errorf("created = %v, want it to contain %s", res.Created, want)
	}
	if _, err := os.Stat(filepath.Join(devsys, "state", "current.yaml")); err != nil {
		t.Errorf("state/current.yaml was not recreated: %v", err)
	}
}
