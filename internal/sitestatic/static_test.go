package sitestatic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"workloom/internal/view"
)

var fixedGen = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

// extRef is the offline hard line: any external reference fails the build.
var extRef = regexp.MustCompile(`https?://|src="http|href="http|@import|url\(http|BeginRewrite|googletag|analytics`)

func testModel() *view.Model {
	dirty := true
	return &view.Model{
		SchemaVersion: 1,
		Project: view.Project{
			ID: "demo", Name: "演示项目", Status: "active", CurrentPhase: "M7",
			Description: "desc <b>escaped</b>",
			Goals:       []string{"g1"},
			Summary:     "state summary",
			Risks:       []string{"r1"},
			Blockers:    []string{"b1"},
			NextFocus:   []string{"n1"},
			TechStack:   []string{"go"},
			Constraints: []string{"c1"},
			Milestones:  nil,
			Provenance:  view.Provenance{Sources: []string{".devsys/project.yaml"}},
		},
		Trust:    view.Trust{State: view.TrustOK, InspectionOK: true},
		Baseline: view.Baseline{Available: true, Commit: "abc123def456789", Branch: "main", Dirty: &dirty, ChangedFiles: 2},
		Progress: view.Progress{
			Provenance: view.Provenance{Sources: []string{".devsys/workitems/"}},
			Counts:     map[string]int{"ready": 1},
			Items: []view.Item{{
				ID: "WLM-1", Title: "first <task>", Status: "ready", Priority: 5,
				Workflow: "gated", WorkflowStep: "implement",
				Dependencies: []string{"WLM-0"},
			}},
		},
		Runs: view.Runs{
			Provenance: view.Provenance{Sources: []string{".devsys/runs/"}},
			Entries: []view.RunEntry{{
				ID: "run-1", WorkitemID: "WLM-1", Status: "succeeded",
				StartedAt: time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC),
				Summary:   "done", StreamTorn: true,
			}},
		},
		Records: view.Records{
			Provenance: view.Provenance{Sources: []string{".devsys/decisions/"}},
			Decisions:  []view.RecordRef{{ID: "D-1", Title: "decide x", Status: "approved"}},
			Findings:   []view.RecordRef{{ID: "F-1", Title: "found y", Severity: "high"}},
			Artifacts:  []view.RecordRef{{ID: "A-1", Title: "artifact z", Version: 2}},
		},
		Knowledge: view.Knowledge{
			Provenance: view.Provenance{Sources: []string{".devsys/knowledge/state.json"}},
			Status:     view.KnowledgeStale, Reason: "2 pages stale",
			Baseline: "abc123def456789", Head: "def456abc123789", Branch: "main",
			IndexReady: true, IndexFiles: 3,
			Pages: []view.PageEntry{{
				Path: "docs/a.md", Status: "stale", SourceCommit: "abc123def456789",
				Stale: true, Reason: "touched", RelatedWorkItems: []string{"WLM-1"},
			}},
			Affected:     []string{"docs/a.md"},
			ChangedFiles: 2,
		},
		Sources: []string{".devsys/project.yaml"},
	}
}

func buildTo(t *testing.T, m *view.Model, out string) []string {
	t.Helper()
	pages, err := Build(m, out, fixedGen)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(pages) != 6 {
		t.Fatalf("pages = %v, want 6", pages)
	}
	return pages
}

func readFile(t *testing.T, out, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

// TestBuildLaysOutEightFiles pins the spec layout: 6 pages + CSS + model.
func TestBuildLaysOutEightFiles(t *testing.T) {
	out := filepath.Join(t.TempDir(), "site")
	buildTo(t, testModel(), out)
	for _, name := range append([]string{"assets/style.css", "data/model.json"}, PageFiles...) {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(name))); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
}

// TestBuildIsOffline: no external references anywhere (断网可看）.
func TestBuildIsOffline(t *testing.T) {
	out := filepath.Join(t.TempDir(), "site")
	buildTo(t, testModel(), out)
	for _, name := range append([]string{"assets/style.css", "data/model.json"}, PageFiles...) {
		content := name
		if strings.HasSuffix(name, ".json") {
			content = readFile(t, out, name)
			// model.json may legitimately name remote generator URLs? It must
			// not: the model only carries local paths and commits.
			if extRef.MatchString(content) {
				t.Errorf("%s references the network", name)
			}
			continue
		}
		if extRef.MatchString(readFile(t, out, name)) {
			t.Errorf("%s references the network", name)
		}
	}
	// No script elements at all.
	for _, page := range PageFiles {
		if strings.Contains(strings.ToLower(readFile(t, out, page)), "<script") {
			t.Errorf("%s contains <script>", page)
		}
	}
}

// TestBuildEscapesFacts: model text with markup renders escaped, not live.
func TestBuildEscapesFacts(t *testing.T) {
	out := filepath.Join(t.TempDir(), "site")
	buildTo(t, testModel(), out)
	if got := readFile(t, out, "index.html"); strings.Contains(got, "<b>escaped</b>") {
		t.Errorf("project description not escaped:\n%s", got)
	}
	if got := readFile(t, out, "tasks.html"); strings.Contains(got, "first <task>") {
		t.Errorf("item title not escaped:\n%s", got)
	}
}

// TestBuildNavAndProvenance: every page links the other five and names its
// sources, the baseline commit and the generation time.
func TestBuildNavAndProvenance(t *testing.T) {
	out := filepath.Join(t.TempDir(), "site")
	m := testModel()
	buildTo(t, m, out)
	for _, page := range PageFiles {
		content := readFile(t, out, page)
		for _, other := range PageFiles {
			if !strings.Contains(content, other) {
				t.Errorf("%s does not link %s", page, other)
			}
		}
		if !strings.Contains(content, "abc123def456789") {
			t.Errorf("%s missing baseline commit", page)
		}
		if !strings.Contains(content, "2026-09-19T12:00:00Z") {
			t.Errorf("%s missing generation time", page)
		}
		if !strings.Contains(content, "data/model.json") {
			t.Errorf("%s missing model link", page)
		}
	}
	for _, want := range []string{".devsys/project.yaml", ".devsys/workitems/", ".devsys/runs/"} {
		found := false
		for _, page := range PageFiles {
			if strings.Contains(readFile(t, out, page), want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no page names source %s", want)
		}
	}
}

// TestBuildPendingBanner: an untrusted model gets the recover banner on every
// page and renders no business rows.
func TestBuildPendingBanner(t *testing.T) {
	m := testModel()
	m.Trust = view.Trust{State: view.TrustPending, Pending: []string{"tx-1"}, Note: "pending transactions"}
	m.Progress.Items = nil
	m.Progress.Counts = map[string]int{}
	out := filepath.Join(t.TempDir(), "site")
	buildTo(t, m, out)
	for _, page := range PageFiles {
		content := readFile(t, out, page)
		if !strings.Contains(content, "devsys recover") || !strings.Contains(content, "tx-1") {
			t.Errorf("%s missing pending banner", page)
		}
	}
	if got := readFile(t, out, "tasks.html"); strings.Contains(got, "WLM-1") {
		t.Errorf("pending site still renders items")
	}
}

// TestBuildDeterministic: same model + same stamp renders byte-identical.
func TestBuildDeterministic(t *testing.T) {
	m := testModel()
	first := filepath.Join(t.TempDir(), "a")
	second := filepath.Join(t.TempDir(), "b")
	buildTo(t, m, first)
	buildTo(t, m, second)
	for _, name := range append([]string{"assets/style.css", "data/model.json"}, PageFiles...) {
		a, b := readFile(t, first, name), readFile(t, second, name)
		if a != b {
			t.Errorf("%s differs between builds", name)
		}
	}
}

// TestBuildModelJSONMatchesModel: data/model.json is the view model verbatim.
func TestBuildModelJSONMatchesModel(t *testing.T) {
	out := filepath.Join(t.TempDir(), "site")
	m := testModel()
	buildTo(t, m, out)
	raw, err := os.ReadFile(filepath.Join(out, "data", "model.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got view.Model
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("model.json: %v", err)
	}
	want, _ := json.Marshal(m)
	gotRaw, _ := json.Marshal(&got)
	if string(want) != string(gotRaw) {
		t.Errorf("model.json differs from the input model")
	}
}

// TestResolveOutRefusals: state, root and .git are never build targets.
func TestResolveOutRefusals(t *testing.T) {
	root := t.TempDir()
	for _, raw := range []string{".devsys", ".devsys/runs", ".devsys/workitems", ".", ".git", ".git/objects"} {
		if _, err := ResolveOut(root, raw); err == nil {
			t.Errorf("ResolveOut(%q) allowed", raw)
		}
	}
	for _, raw := range []string{"", ".devsys/dist/site", ".devsys/dist/custom", "site", "../outside"} {
		if _, err := ResolveOut(root, raw); err != nil {
			t.Errorf("ResolveOut(%q): %v", raw, err)
		}
	}
	if got, err := ResolveOut(root, ""); err != nil || got != DefaultOutDir(root) {
		t.Errorf("default = %q, %v", got, err)
	}
}

// TestResolveOutAbsoluteOutside: an absolute out elsewhere is allowed.
func TestResolveOutAbsoluteOutside(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(t.TempDir(), "published")
	if got, err := ResolveOut(root, abs); err != nil || got != abs {
		t.Errorf("absolute out = %q, %v", got, err)
	}
}
