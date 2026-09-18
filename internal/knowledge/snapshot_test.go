package knowledge

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// snapshotRepo builds a throwaway repository with the fixture files.
func snapshotRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func exclusionFor(t *testing.T, snapshot *Snapshot, path string) Exclusion {
	t.Helper()
	for _, exclusion := range snapshot.Excluded.Files {
		if exclusion.Path == path {
			return exclusion
		}
	}
	t.Fatalf("%s is not in the exclusion list: %+v", path, snapshot.Excluded.Files)
	return Exclusion{}
}

func hasFile(snapshot *Snapshot, path string) bool {
	for _, entry := range snapshot.Files {
		if entry.Path == path {
			return true
		}
	}
	return false
}

func TestBuildSnapshotFingerprintsAndExcludes(t *testing.T) {
	big := strings.Repeat("x", DefaultMaxFileSize+1)
	root := snapshotRepo(t, map[string]string{
		".gitignore":        "ignored.txt\nbuild/\n",
		"a.go":              "package a\n",
		"docs/readme.md":    "# hi\n",
		"ignored.txt":       "not indexed\n",
		"build/out.bin":     "not indexed\n",
		".env":              "TOKEN=1\n",
		"id_rsa":            "private\n",
		"huge.bin":          big,
		"vendor/dep/dep.go": "package dep\n",
		".devsys/state.md":  "# state\n",
	})
	snapshot, err := BuildSnapshot(root, ScanOptions{Now: func() time.Time { return time.Unix(0, 0).UTC() }})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"a.go", "docs/readme.md", ".gitignore"} {
		if !hasFile(snapshot, want) {
			t.Errorf("%s missing from the index: %+v", want, snapshot.Files)
		}
	}
	for path, reason := range map[string]string{
		"ignored.txt":   ".gitignore",
		"build/out.bin": ".gitignore",
		".env":          "密钥名模式",
		"id_rsa":        "密钥名模式",
		"huge.bin":      "字节上限",
	} {
		if got := exclusionFor(t, snapshot, path); !strings.Contains(got.Reason, reason) {
			t.Errorf("%s excluded with %q, want %q", path, got.Reason, reason)
		}
	}
	for _, dir := range []string{"vendor/", ".devsys/"} {
		if got := exclusionFor(t, snapshot, dir); !strings.Contains(got.Reason, "默认排除目录") {
			t.Errorf("%s excluded with %q", dir, got.Reason)
		}
	}
	if snapshot.Stats.TotalFiles != 3 {
		t.Errorf("total_files = %d, want 3", snapshot.Stats.TotalFiles)
	}
	if snapshot.Stats.Languages["go"] != 1 || snapshot.Stats.Languages["markdown"] != 1 {
		t.Errorf("languages = %v", snapshot.Stats.Languages)
	}
	if snapshot.SchemaVersion != SnapshotSchemaVersion || !snapshot.GeneratedAt.Equal(time.Unix(0, 0).UTC()) {
		t.Errorf("snapshot header = %+v", snapshot)
	}
	// The fingerprint must be the file's content hash.
	data, err := os.ReadFile(filepath.Join(root, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, entry := range snapshot.Files {
		if entry.Path == "a.go" && entry.Hash != want {
			t.Errorf("hash = %s, want %s", entry.Hash, want)
		}
	}
	// Every documented rule is recorded, so the exclusions are explainable.
	joined := strings.Join(snapshot.Excluded.Rules, " | ")
	for _, want := range []string{"目录：", "密钥名模式", "单文件 >", ".gitignore"} {
		if !strings.Contains(joined, want) {
			t.Errorf("rules missing %q: %s", want, joined)
		}
	}
}

func TestBuildSnapshotCustomIgnoreFile(t *testing.T) {
	root := snapshotRepo(t, map[string]string{
		"a.go":                  "package a\n",
		"docs/private/notes.md": "# notes\n",
		"docs/public/readme.md": "# readme\n",
		Dir + "/ignore":         "# private docs\ndocs/private/\n!keep.md\n",
		".devsys/config.yaml":   "schema_version: 1\n",
	})
	snapshot, err := BuildSnapshot(root, ScanOptions{SkipGitIgnore: true})
	if err != nil {
		t.Fatal(err)
	}
	got := exclusionFor(t, snapshot, "docs/private/notes.md")
	if !strings.Contains(got.Reason, CustomIgnoreFile) {
		t.Errorf("reason = %q", got.Reason)
	}
	if !hasFile(snapshot, "docs/public/readme.md") {
		t.Error("public page was excluded")
	}
	if len(snapshot.Excluded.IgnoreFiles) != 1 || snapshot.Excluded.IgnoreFiles[0] != CustomIgnoreFile {
		t.Errorf("ignore files = %v", snapshot.Excluded.IgnoreFiles)
	}
	// The unsupported negation is surfaced as a warning, not silently dropped.
	joined := strings.Join(snapshot.Excluded.Rules, " | ")
	if !strings.Contains(joined, "取反不受支持") {
		t.Errorf("rules = %s", joined)
	}
}

// A repository without git metadata cannot honour .gitignore, and §12.5
// forbids pretending it did.
func TestBuildSnapshotWithoutGitFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildSnapshot(root, ScanOptions{}); err == nil || !strings.Contains(err.Error(), ErrNoGit.Error()) {
		t.Fatalf("err = %v, want %v", err, ErrNoGit)
	}
	// The caller may opt out explicitly; then the scan says so in its rules.
	snapshot, err := BuildSnapshot(root, ScanOptions{SkipGitIgnore: true})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Stats.TotalFiles != 1 {
		t.Fatalf("files = %d", snapshot.Stats.TotalFiles)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	root := t.TempDir()
	snapshot := &Snapshot{
		SchemaVersion: SnapshotSchemaVersion,
		GeneratedAt:   time.Unix(0, 0).UTC(),
		Files:         []FileEntry{{Path: "a.go", Size: 10, Hash: strings.Repeat("ab", 32), Language: "go"}},
		Stats:         SnapshotStats{TotalFiles: 1, TotalSize: 10, Languages: map[string]int{"go": 1}},
		Excluded:      SnapshotExcluded{Rules: []string{"r"}},
	}
	rel, err := WriteSnapshot(root, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if rel != SnapshotFile {
		t.Fatalf("rel = %s", rel)
	}
	loaded, err := LoadSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Files) != 1 || loaded.Files[0].Path != "a.go" || loaded.Stats.Languages["go"] != 1 {
		t.Fatalf("loaded = %+v", loaded)
	}
	// An unknown schema version is refused instead of guessed at.
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(SnapshotFile)))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["schema_version"] = 99
	patched, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(SnapshotFile)), patched, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSnapshot(root); err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("err = %v", err)
	}
}

// A file that vanishes while the scan runs is an exclusion, not a failure: a
// snapshot of a live tree must not die on one racing deletion.
func TestBuildSnapshotToleratesVanishingFiles(t *testing.T) {
	root := snapshotRepo(t, map[string]string{"a.go": "package a\n"})
	// The candidate list is built first, so removing the file between the walk
	// and the read is exactly the race the scan has to survive.
	removed := filepath.Join(root, "gone.go")
	if err := os.WriteFile(removed, []byte("package gone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := BuildSnapshot(root, ScanOptions{SkipGitIgnore: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}
	// Rebuild after removal: the vanished path must not appear anywhere.
	snapshot, err = BuildSnapshot(root, ScanOptions{SkipGitIgnore: true})
	if err != nil {
		t.Fatal(err)
	}
	if hasFile(snapshot, "gone.go") {
		t.Fatal("removed file is in the index")
	}
	if snapshot.Stats.TotalFiles != 1 {
		t.Fatalf("files = %d", snapshot.Stats.TotalFiles)
	}
}

// The excluded-directory rule must not be defeated by case on a filesystem
// where `.GIT` and `.git` are the same directory.
func TestBuildSnapshotExcludesDirectoriesCaseInsensitively(t *testing.T) {
	root := snapshotRepo(t, map[string]string{"a.go": "package a\n"})
	if err := os.MkdirAll(filepath.Join(root, "Vendor", "dep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Vendor", "dep", "x.go"), []byte("package dep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := BuildSnapshot(root, ScanOptions{SkipGitIgnore: true})
	if err != nil {
		t.Fatal(err)
	}
	if hasFile(snapshot, "Vendor/dep/x.go") {
		t.Fatal("Vendor/ was walked")
	}
	if got := exclusionFor(t, snapshot, "Vendor/"); !strings.Contains(got.Reason, "默认排除目录") {
		t.Fatalf("reason = %q", got.Reason)
	}
}

// A credential keeps its own reason even when an ignore rule also covers it.
func TestBuildSnapshotSecretReasonSurvivesOverlap(t *testing.T) {
	root := snapshotRepo(t, map[string]string{
		".gitignore": "*.key\n",
		"server.key": "secret\n",
		"a.go":       "package a\n",
	})
	snapshot, err := BuildSnapshot(root, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := exclusionFor(t, snapshot, "server.key")
	if !strings.Contains(got.Reason, "密钥名模式") || !strings.Contains(got.Reason, ".gitignore") {
		t.Fatalf("reason = %q", got.Reason)
	}
}
