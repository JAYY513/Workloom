package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archiveProject(t *testing.T) string {
	t.Helper()
	requireGit(t)
	repo := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, repo)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(repo)
	if code, _, errOut := run(t, "init"); code != CodeOK {
		t.Fatalf("init: code=%d stderr=%q", code, errOut)
	}
	return repo
}

func writeShard(t *testing.T, repo, month, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, ".devsys", "events", month+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// shardBody is one well-formed old event: the reader decodes full events,
// so the fixture keeps schema_version/type/subject/time/actor.
func shardBody() string {
	return "{\"schema_version\":1,\"id\":\"ev-old-1\",\"project_id\":\"p\",\"type\":\"note\",\"subject\":{\"type\":\"workitem\",\"id\":\"WLM-1\"},\"time\":\"2020-01-05T00:00:00Z\",\"actor\":\"t\",\"content\":\"old\"}\n"
}

// Archiving an old shard keeps history queryable and shrinks the live tree.
func TestArchiveEventsKeepsHistoryQueryable(t *testing.T) {
	repo := archiveProject(t)
	writeShard(t, repo, "2020-01", shardBody())
	if code, _, errOut := run(t, "event", "record", "--type", "note",
		"--subject-type", "workitem", "--subject", "WLM-1",
		"--actor", "t", "--content", "live-event"); code != CodeOK {
		t.Fatalf("event record: code=%d stderr=%q", code, errOut)
	}
	code, out, errOut := run(t, "--json", "event", "list")
	if code != CodeOK {
		t.Fatalf("event list: code=%d stderr=%q", code, errOut)
	}
	var before struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal([]byte(out), &before); err != nil {
		t.Fatalf("json: %v (%q)", err, out)
	}
	nBefore := len(before.Events)

	code, out, errOut = run(t, "archive", "events", "--before", "2026-01", "--actor", "t", "--reason", "trim")
	if code != CodeOK {
		t.Fatalf("archive events: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if !strings.Contains(out, "events/2020-01.jsonl -> archive/events/2020-01.jsonl") {
		t.Fatalf("stdout = %q, want the move listed", out)
	}
	if _, err := os.Stat(filepath.Join(repo, ".devsys", "events", "2020-01.jsonl")); !os.IsNotExist(err) {
		t.Fatal("live shard still present after archive")
	}

	code, out, _ = run(t, "--json", "event", "list")
	if code != CodeOK {
		t.Fatalf("event list after: code=%d", code)
	}
	var after struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal([]byte(out), &after); err != nil {
		t.Fatalf("json: %v (%q)", err, out)
	}
	if len(after.Events) != nBefore {
		t.Fatalf("events after = %d, want %d", len(after.Events), nBefore)
	}
}

// The archive family needs its selectors and its audit trail.
func TestArchiveUsage(t *testing.T) {
	archiveProject(t)
	if code, _, _ := run(t, "archive", "events", "--actor", "t", "--reason", "r"); code != CodeUsage {
		t.Fatalf("missing --before: code=%d, want %d", code, CodeUsage)
	}
	if code, _, _ := run(t, "archive", "runs", "--actor", "t", "--reason", "r"); code != CodeUsage {
		t.Fatalf("missing --id: code=%d, want %d", code, CodeUsage)
	}
	if code, _, _ := run(t, "archive", "bogus", "--actor", "t", "--reason", "r"); code != CodeUsage {
		t.Fatalf("bogus subcommand: code=%d, want %d", code, CodeUsage)
	}
}
