package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDirEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	got, err := Dir()
	if err != nil || got != dir {
		t.Fatalf("Dir() = %q, %v", got, err)
	}
	p, err := Path()
	if err != nil || p != filepath.Join(dir, "registry.yaml") {
		t.Fatalf("Path() = %q, %v", p, err)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	reg, err := Load(filepath.Join(t.TempDir(), "registry.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Projects) != 0 {
		t.Fatalf("Projects = %v", reg.Projects)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "registry.yaml")
	ts := time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC)
	reg := &Registry{Projects: []Entry{
		{ID: "workloom", Path: `C:/src/it's a "dir"`, LastSeenAt: ts},
		{ID: "温箱", Path: "D:/Projects/温箱 监控", LastSeenAt: ts.Add(time.Minute)},
	}}
	if err := reg.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Projects) != 2 {
		t.Fatalf("Projects = %v", got.Projects)
	}
	// deterministic order: sorted by cleaned path
	if got.Projects[0].ID != "workloom" || got.Projects[0].Path != `C:/src/it's a "dir"` {
		t.Errorf("entry 0 = %+v", got.Projects[0])
	}
	if !got.Projects[0].LastSeenAt.Equal(ts) {
		t.Errorf("LastSeenAt = %v", got.Projects[0].LastSeenAt)
	}
	if got.Projects[1].ID != "温箱" || got.Projects[1].Path != "D:/Projects/温箱 监控" {
		t.Errorf("entry 1 = %+v", got.Projects[1])
	}
}

func TestUpsertDedupesByPath(t *testing.T) {
	reg := &Registry{}
	if replaced := reg.Upsert(Entry{ID: "a", Path: "C:/x"}); replaced {
		t.Error("first insert must not report a replacement")
	}
	if replaced := reg.Upsert(Entry{ID: "a2", Path: "C:/x"}); !replaced {
		t.Error("same path must replace the entry")
	}
	if len(reg.Projects) != 1 || reg.Projects[0].ID != "a2" {
		t.Fatalf("Projects = %+v", reg.Projects)
	}
}

func TestSaveEmptyAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.yaml")
	if err := (&Registry{}).Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Projects) != 0 {
		t.Fatalf("Projects = %v", got.Projects)
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		"other: 1\n",
		"projects:\n  - path: 'x'\n",
		"projects:\n  - id: 'x'\n    unknown: 'y'\n",
		"projects:\n  - id: 'x'\n    path: 'y'\n    last_seen_at: 'not-a-time'\n",
		"projects:\n  - id: 'x\n",
		"projects:\n  - id: 'x'\n    path: 'y'\n",
	}
	for _, data := range bad {
		if _, err := parse("reg.yaml", data); err == nil {
			t.Errorf("expected parse error for %q", data)
		}
	}
}

func TestParseEmptyProjects(t *testing.T) {
	reg, err := parse("reg.yaml", "projects: []\n")
	if err != nil || len(reg.Projects) != 0 {
		t.Fatalf("reg=%+v err=%v", reg, err)
	}
}

func TestParseAcceptsCommentsAndPlainScalars(t *testing.T) {
	data := "# comment\nprojects:\n  - id: plain-id\n    path: D:/p\n    last_seen_at: 2026-09-17T08:00:00Z\n"
	reg, err := parse("reg.yaml", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Projects) != 1 || reg.Projects[0].ID != "plain-id" || reg.Projects[0].Path != "D:/p" {
		t.Fatalf("Projects = %+v", reg.Projects)
	}
}

func TestSaveHeaderMentionsSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.yaml")
	if err := (&Registry{}).Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "方案 §14.4") {
		t.Errorf("header missing spec reference: %s", data)
	}
}
