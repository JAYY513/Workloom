package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestJSONLStreamsOneRecordPerLine pins the exchange format: lists render as
// one JSON record per line, with the same field names the --json envelope
// carries.
func TestJSONLStreamsOneRecordPerLine(t *testing.T) {
	gatedProject(t)
	first := newWorkitem(t, "jsonl one")
	second := newWorkitem(t, "jsonl two")
	if code, _, errOut := run(t, "workitem", "comment", "--id", first, "--text", "hello", "--actor", "operator"); code != CodeOK {
		t.Fatalf("comment: %d %s", code, errOut)
	}

	code, out, errOut := run(t, "--jsonl", "workitem", "list")
	if code != CodeOK {
		t.Fatalf("workitem list --jsonl: code=%d stderr=%q", code, errOut)
	}
	lines := nonEmptyLines(out)
	if len(lines) != 2 {
		t.Fatalf("expected 2 records, got %d: %q", len(lines), out)
	}
	ids := map[string]bool{}
	for _, line := range lines {
		var record struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("record is not JSON: %v (%q)", err, line)
		}
		ids[record.ID] = true
	}
	if !ids[first] || !ids[second] {
		t.Fatalf("jsonl records lack the work items: %v", ids)
	}

	// Every documented list command streams one record per line.
	code, out, _ = run(t, "--jsonl", "event", "list", "--type", "comment")
	if code != CodeOK {
		t.Fatalf("event list --jsonl: code=%d", code)
	}
	lines = nonEmptyLines(out)
	if len(lines) != 1 || !strings.Contains(lines[0], `"type":"comment"`) {
		t.Fatalf("event jsonl = %q", out)
	}
}

// TestJSONLCoversEveryListCommand walks all seven documented list commands.
func TestJSONLCoversEveryListCommand(t *testing.T) {
	gatedProject(t)
	wi := newWorkitem(t, "jsonl coverage task")
	if code, _, errOut := run(t, "decision", "create", "--title", "d", "--decision", "d", "--by", "operator"); code != CodeOK {
		t.Fatalf("decision create: %d %s", code, errOut)
	}
	if code, _, errOut := run(t, "finding", "create", "--title", "f", "--description", "f"); code != CodeOK {
		t.Fatalf("finding create: %d %s", code, errOut)
	}
	if code, _, errOut := run(t, "artifact", "register", "--name", "a.md"); code != CodeOK {
		t.Fatalf("artifact register: %d %s", code, errOut)
	}
	if code, _, errOut := run(t, "run", "create", "--workitem", wi, "--actor", "operator", "--reason", "coverage"); code != CodeOK {
		t.Fatalf("run create: %d %s", code, errOut)
	}
	if code, _, errOut := run(t, "workitem", "comment", "--id", wi, "--text", "hello", "--actor", "operator"); code != CodeOK {
		t.Fatalf("comment: %d %s", code, errOut)
	}
	if code, _, errOut := run(t, "approval", "request", "--id", wi, "--stage", "review",
		"--actor", "operator", "--reason", "coverage"); code != CodeOK {
		t.Fatalf("approval request: %d %s", code, errOut)
	}

	for _, command := range [][]string{
		{"workitem", "list"},
		{"decision", "list"},
		{"finding", "list"},
		{"event", "list"},
		{"artifact", "list"},
		{"run", "list"},
		{"approval", "list"},
	} {
		args := append([]string{"--jsonl"}, command...)
		code, out, errOut := run(t, args...)
		if code != CodeOK {
			t.Fatalf("%v: code=%d stderr=%q", command, code, errOut)
		}
		lines := nonEmptyLines(out)
		if len(lines) == 0 {
			t.Fatalf("%v: no records", command)
		}
		for _, line := range lines {
			var record struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal([]byte(line), &record); err != nil || record.ID == "" {
				t.Fatalf("%v: record lacks an id: %v (%q)", command, err, line)
			}
		}
	}
}

func nonEmptyLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestJSONModesAreMutuallyExclusive(t *testing.T) {
	gatedProject(t)
	code, _, errOut := run(t, "--json", "--jsonl", "workitem", "list")
	if code != CodeUsage || !strings.Contains(errOut, "mutually exclusive") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

// TestExitCodeContract pins the documented table scripts branch on.
func TestExitCodeContract(t *testing.T) {
	gatedProject(t)
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"success", []string{"workitem", "list"}, CodeOK},
		{"usage", []string{"bogus-command"}, CodeUsage},
		{"precondition", []string{"workitem", "get", "NOPE-1"}, CodePrecondition},
	}
	for _, tc := range cases {
		if code, _, _ := run(t, tc.args...); code != tc.want {
			t.Fatalf("%s: code=%d, want %d", tc.name, code, tc.want)
		}
	}

	// Invalid managed state (unsupported schema_version) refuses reads too.
	path := filepath.Join(".devsys", "project.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "--json", "project", "get"); code != CodeInvalid {
		t.Fatalf("invalid state: code=%d, want %d", code, CodeInvalid)
	}
}
