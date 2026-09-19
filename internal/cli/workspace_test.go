package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"workloom/internal/view"
)

// TestWorkspaceViewJSON: the M7.1 model comes out through the documented
// envelope, with the project's facts and the sources it read.
func TestWorkspaceViewJSON(t *testing.T) {
	root, items := gatedProject(t)
	id := createGatedWorkitem(t, items, "see the board", "read-only view")
	gitCommitAll(t, root, "work item")

	code, out, errOut := run(t, "--json", "workspace", "view")
	if code != CodeOK {
		t.Fatalf("workspace view --json: code=%d stderr=%q", code, errOut)
	}
	var payload struct {
		OK bool `json:"ok"`
		view.Model
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if !payload.OK || payload.SchemaVersion != view.SchemaVersion {
		t.Fatalf("payload header: ok=%v schema=%d", payload.OK, payload.SchemaVersion)
	}
	if payload.Project.ID == "" || payload.Project.Name == "" {
		t.Errorf("project = %+v", payload.Project)
	}
	if len(payload.Progress.Items) != 1 || payload.Progress.Items[0].ID != id {
		t.Errorf("items = %+v", payload.Progress.Items)
	}
	if !payload.Baseline.Available || payload.Baseline.Commit == "" {
		t.Errorf("baseline = %+v", payload.Baseline)
	}
	if !containsPath(payload.Sources, ".devsys/project.yaml") {
		t.Errorf("sources = %v", payload.Sources)
	}
}

// TestWorkspaceViewHumanOutput: the text face summarizes the same model.
func TestWorkspaceViewHumanOutput(t *testing.T) {
	_, items := gatedProject(t)
	createGatedWorkitem(t, items, "see the board", "read-only view")

	code, out, errOut := run(t, "workspace", "view")
	if code != CodeOK {
		t.Fatalf("workspace view: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"project:", "progress:", "readiness:", "knowledge:", "sources:"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout misses %q:\n%s", want, out)
		}
	}
}

// TestWorkspaceViewWritesNothing is the read-only contract at the CLI level:
// running the command leaves every project file — the lock included — alone.
func TestWorkspaceViewWritesNothing(t *testing.T) {
	root, items := gatedProject(t)
	createGatedWorkitem(t, items, "see the board", "read-only view")
	gitCommitAll(t, root, "work item")

	before := workspaceFingerprint(t, root)
	if code, _, errOut := run(t, "--json", "workspace", "view"); code != CodeOK {
		t.Fatalf("workspace view: code=%d stderr=%q", code, errOut)
	}
	after := workspaceFingerprint(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("workspace view changed files:\n%s", workspaceDiff(before, after))
	}
}

// TestWorkspaceViewWithoutProject: a directory without .devsys/ is a
// precondition failure naming the fix.
func TestWorkspaceViewWithoutProject(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	initRepo(t, dir)
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	t.Chdir(dir)

	code, _, errOut := run(t, "workspace", "view")
	if code != CodePrecondition {
		t.Fatalf("code = %d, want %d (stderr=%q)", code, CodePrecondition, errOut)
	}
	if !strings.Contains(errOut, "devsys init") {
		t.Errorf("stderr = %q", errOut)
	}
}

// TestWorkspaceUsageErrors: the family rejects an unknown subcommand and
// misuse of the view flags.
func TestWorkspaceUsageErrors(t *testing.T) {
	cases := [][]string{
		{"workspace"},
		{"workspace", "bogus"},
		{"workspace", "view", "--limit", "0"},
		{"workspace", "view", "extra"},
	}
	for _, args := range cases {
		if code, _, errOut := run(t, args...); code != CodeUsage {
			t.Errorf("%v: code = %d (stderr=%q), want %d", args, code, errOut, CodeUsage)
		}
	}
}

func containsPath(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// workspaceFingerprint digests every file under root except .git/ — git's own
// index housekeeping is not project state.
func workspaceFingerprint(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == ".git" {
			return fs.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return statErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		sum := sha256.Sum256(data)
		out[rel] = fmt.Sprintf("%d %d %s", info.Size(), info.ModTime().UnixNano(), hex.EncodeToString(sum[:]))
		return nil
	})
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	return out
}

func workspaceDiff(before, after map[string]string) string {
	var b strings.Builder
	for path, want := range before {
		if got, ok := after[path]; !ok {
			fmt.Fprintf(&b, "- %s\n", path)
		} else if got != want {
			fmt.Fprintf(&b, "~ %s\n", path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			fmt.Fprintf(&b, "+ %s\n", path)
		}
	}
	return b.String()
}
