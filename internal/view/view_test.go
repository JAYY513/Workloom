package view

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"workloom/internal/domain"
	"workloom/internal/knowledge"
	"workloom/internal/next"
	"workloom/internal/project"
	"workloom/internal/record"
	"workloom/internal/run"
	"workloom/internal/workitem"
)

// testNow is the fixed clock every test builds with. The model must never
// carry it: two builds of the same state stay byte-identical.
var testNow = time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)

func fixedClock() time.Time { return testNow }

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
}

// gitRun runs one git command with a fixed identity, so fixtures do not depend
// on the machine's git configuration.
func gitRun(t *testing.T, root string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", root, "-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitAll(t *testing.T, root, message string) string {
	t.Helper()
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "--allow-empty", "-m", message)
	return gitRun(t, root, "rev-parse", "HEAD")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// projectOnly builds a project nobody has written state to since init: no lock
// file exists, which is the read-only-copy case.
func projectOnly(t *testing.T) string {
	t.Helper()
	requireGit(t)
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "init", "-q")
	t.Setenv("DEVSYS_CONFIG_DIR", t.TempDir())
	if _, err := project.Init(root, project.Options{Now: testNow}); err != nil {
		t.Fatalf("project init: %v", err)
	}
	return root
}

// fixture builds a project with one of everything the view aggregates: two
// work items, a run, a decision, a finding, an artifact, one knowledge page
// with its layer state and index.
func fixture(t *testing.T) string {
	t.Helper()
	root := projectOnly(t)
	ctx := context.Background()

	items := workitem.New(root)
	one := &domain.WorkItem{Title: "write the read-only view", Type: "task", Status: domain.StatusReady, Priority: 5}
	if _, err := items.Create(ctx, one, "WLM"); err != nil {
		t.Fatalf("create work item: %v", err)
	}
	two := &domain.WorkItem{Title: "unblock the board", Status: domain.StatusBlocked, Priority: 1, Dependencies: []string{one.ID}}
	if _, err := items.Create(ctx, two, "WLM"); err != nil {
		t.Fatalf("create work item: %v", err)
	}

	advanced := true
	runID, err := run.New(root).Create(ctx, &domain.Run{
		WorkItemID:   one.ID,
		Status:       "succeeded",
		Phase:        "finishing",
		Agent:        domain.RunAgent{ID: "agent-1", Harness: "codex", Model: "gpt-x"},
		Workspace:    domain.Workspace{Path: "/ws/wlm-1", Branch: "devsys/wlm-1"},
		Verification: domain.Verification{Advanced: &advanced},
		StartedAt:    testNow.Add(-2 * time.Hour),
		Result:       domain.RunResult{Summary: strPtr("done")},
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	// A completed event stream for that run: the view reports a torn tail but
	// never repairs one (M6.1 owns repair).
	writeFile(t, filepath.Join(root, ".devsys", "runs", runID+".jsonl"), "{\"type\":\"start\"}\n")

	records := record.New(root)
	if _, err := records.CreateDecision(ctx, &domain.Decision{
		Title: "keep the view read-only", Status: "accepted",
		RelatedWorkItems: []string{one.ID}, CreatedAt: testNow.Add(-3 * time.Hour),
	}); err != nil {
		t.Fatalf("create decision: %v", err)
	}
	if _, err := records.CreateFinding(ctx, &domain.Finding{
		Title: "a lock file would break read-only copies", Type: "risk", Severity: "major", Status: "open",
		RelatedWorkItems: []string{one.ID},
	}); err != nil {
		t.Fatalf("create finding: %v", err)
	}
	if _, err := records.CreateArtifact(ctx, &domain.Artifact{
		Name: "view spec", Type: "spec", Path: "docs/spec.md", Status: "current", Version: 1,
		RelatedWorkItems: []string{one.ID}, CreatedAt: testNow.Add(-4 * time.Hour),
	}); err != nil {
		t.Fatalf("create artifact: %v", err)
	}

	// The page layer: a source file, a page describing it, then the layer's
	// own state and index. The page's baseline is the commit that describes
	// the sources, exactly as a generator would record it.
	writeFile(t, filepath.Join(root, "internal", "view", "view.go"), "package view\n")
	base := commitAll(t, root, "sources")
	page := "---\nstatus: stable\ntype: module\ntriggers:\n  - view\ndescription: the read-only workspace view\nsource_commit: " + base + "\nsources:\n  - internal/view/**\n---\n\n# View\n"
	pageRel := "docs/repowiki/knowledge/view.md"
	pagePath := filepath.Join(root, filepath.FromSlash(pageRel))
	writeFile(t, pagePath, page)
	commitAll(t, root, "page")

	hash, err := knowledge.HashFile(pagePath)
	if err != nil {
		t.Fatalf("hash page: %v", err)
	}
	if _, err := knowledge.WriteState(root, &knowledge.State{
		SchemaVersion: knowledge.StateSchemaVersion,
		Baseline:      knowledge.Baseline{Commit: base, Branch: "master"},
		Pages: map[string]knowledge.PageState{
			pageRel: {Sources: []string{"internal/view/**"}, ContentHash: hash, SourceCommit: base},
		},
		UpdatedAt: testNow,
	}); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if _, err := knowledge.WriteSnapshot(root, &knowledge.Snapshot{
		SchemaVersion: knowledge.SnapshotSchemaVersion,
		GeneratedAt:   testNow,
		Stats:         knowledge.SnapshotStats{TotalFiles: 7},
	}); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	commitAll(t, root, "layer state")
	return root
}

func strPtr(s string) *string { return &s }

func build(t *testing.T, root string) *Model {
	t.Helper()
	m, err := Build(context.Background(), root, Options{Now: fixedClock})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return m
}

// TestBuildAssemblesEverySection is the M7.1 acceptance: one snapshot carries
// the blueprint, the progress with the readiness verdict, the runs, the
// records and the knowledge freshness, each naming its sources.
func TestBuildAssemblesEverySection(t *testing.T) {
	root := fixture(t)
	m := build(t, root)

	if m.SchemaVersion != SchemaVersion {
		t.Fatalf("schema_version = %d", m.SchemaVersion)
	}
	if m.Project.ID == "" || m.Project.Name == "" {
		t.Errorf("project identity = %q/%q", m.Project.ID, m.Project.Name)
	}
	if len(m.Project.Risks) != 0 || len(m.Project.Milestones) != 0 {
		t.Errorf("project state = %+v", m.Project)
	}
	if m.Project.Degraded {
		t.Errorf("project degraded: %s", m.Project.Reason)
	}

	if len(m.Progress.Items) != 2 {
		t.Fatalf("items = %d, want 2: %+v", len(m.Progress.Items), m.Progress.Items)
	}
	if m.Progress.Counts[domain.StatusReady] != 1 || m.Progress.Counts[domain.StatusBlocked] != 1 {
		t.Errorf("counts = %v", m.Progress.Counts)
	}
	// A blocked work item is a recorded risk (§7.4), so the verdict is not PASS.
	if m.Progress.Readiness.Verdict != "CONCERNS" {
		t.Errorf("verdict = %q, risks = %+v", m.Progress.Readiness.Verdict, m.Progress.Readiness.Risks)
	}

	if len(m.Runs.Entries) != 1 {
		t.Fatalf("runs = %d", len(m.Runs.Entries))
	}
	entry := m.Runs.Entries[0]
	if entry.WorkitemID != m.Progress.Items[0].ID || entry.Harness != "codex" || entry.Branch != "devsys/wlm-1" {
		t.Errorf("run entry = %+v", entry)
	}
	if entry.Advanced == nil || !*entry.Advanced || entry.Summary != "done" {
		t.Errorf("run verdict = %+v", entry)
	}

	if len(m.Records.Decisions) != 1 || len(m.Records.Findings) != 1 || len(m.Records.Artifacts) != 1 {
		t.Fatalf("records = %d/%d/%d", len(m.Records.Decisions), len(m.Records.Findings), len(m.Records.Artifacts))
	}
	if got := m.Records.Findings[0].Severity; got != "major" {
		t.Errorf("finding severity = %q", got)
	}
	if got := m.Records.Artifacts[0].Version; got != 1 {
		t.Errorf("artifact version = %d", got)
	}

	if m.Knowledge.Status != KnowledgeFresh {
		t.Fatalf("knowledge = %q (%s)", m.Knowledge.Status, m.Knowledge.Reason)
	}
	if !m.Knowledge.IndexReady || m.Knowledge.IndexFiles != 7 {
		t.Errorf("index = %+v", m.Knowledge)
	}
	if len(m.Knowledge.Pages) != 1 {
		t.Fatalf("pages = %+v", m.Knowledge.Pages)
	}
	// The page's trigger ("view") appears in WLM-1's title.
	if got := m.Knowledge.Pages[0].RelatedWorkItems; !reflect.DeepEqual(got, []string{m.Progress.Items[0].ID}) {
		t.Errorf("related work items = %v", got)
	}

	if !m.Baseline.Available || m.Baseline.Commit == "" || m.Baseline.Branch == "" {
		t.Fatalf("baseline = %+v", m.Baseline)
	}
	if m.Baseline.Dirty == nil || *m.Baseline.Dirty {
		t.Errorf("dirty = %v, want false", m.Baseline.Dirty)
	}

	// Every section names its sources; the union is the model's.
	for name, sources := range map[string][]string{
		"project":   m.Project.Sources,
		"progress":  m.Progress.Sources,
		"runs":      m.Runs.Sources,
		"records":   m.Records.Sources,
		"knowledge": m.Knowledge.Sources,
	} {
		if len(sources) == 0 {
			t.Errorf("%s section has no sources", name)
		}
	}
	for _, want := range []string{
		".devsys/project.yaml", ".devsys/state/current.yaml", ".devsys/workitems/",
		".devsys/runs/", ".devsys/runs/" + m.Runs.Entries[0].ID + ".jsonl",
		".devsys/decisions/", ".devsys/workflows/", ".devsys/knowledge/state.json", "docs/repowiki/",
	} {
		if !contains(m.Sources, want) {
			t.Errorf("sources miss %q:\n%s", want, strings.Join(m.Sources, "\n"))
		}
	}
	if m.Trust.State != TrustOK || !m.Trust.InspectionOK {
		t.Errorf("trust = %+v", m.Trust)
	}
	if len(m.Problems) != 0 {
		t.Errorf("problems = %v", m.Problems)
	}
}

// TestBuildIsDeterministic: two builds of the same state are byte-identical.
func TestBuildIsDeterministic(t *testing.T) {
	root := fixture(t)
	first, err := json.Marshal(build(t, root))
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(build(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("models differ:\n%s\n\n%s", first, second)
	}
}

// TestBuildWritesNothing: the aggregation leaves every file — the project lock
// included — untouched.
func TestBuildWritesNothing(t *testing.T) {
	root := fixture(t)
	before := fingerprint(t, root)
	build(t, root)
	after := fingerprint(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("build changed files:\n%s", diffFingerprints(before, after))
	}
}

// TestBuildWithoutLockFileStaysReadable: a copy nobody has written to has no
// project lock; the view reads lock-free, says so, and still renders the
// facts — it must not create the lock file to get one.
func TestBuildWithoutLockFileStaysReadable(t *testing.T) {
	root := projectOnly(t)
	lock := filepath.Join(root, ".devsys", "local", "lock")
	if _, err := os.Stat(lock); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("fixture precondition: lock file exists (err = %v)", err)
	}
	before := fingerprint(t, root)
	m := build(t, root)

	if m.Trust.State != TrustAdvisory || m.Trust.InspectionOK {
		t.Fatalf("trust = %+v, want advisory", m.Trust)
	}
	if m.Trust.Note == "" {
		t.Error("advisory snapshot carries no note")
	}
	if m.Project.ID == "" {
		t.Error("advisory snapshot rendered no project facts")
	}
	if _, err := os.Stat(lock); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("build created the project lock (err = %v)", err)
	}
	if after := fingerprint(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("build changed files:\n%s", diffFingerprints(before, after))
	}
}

// TestBuildOnAReadOnlyCopy is the acceptance in its literal form: copy the
// project, make every file read-only, and the view still comes out complete.
func TestBuildOnAReadOnlyCopy(t *testing.T) {
	source := fixture(t)
	copyRoot := filepath.Join(t.TempDir(), "copy")
	copyTree(t, source, copyRoot)

	var restore []string
	err := filepath.WalkDir(copyRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		restore = append(restore, path)
		return os.Chmod(path, 0o444)
	})
	if err != nil {
		t.Fatalf("make read-only: %v", err)
	}
	t.Cleanup(func() {
		for _, path := range restore {
			_ = os.Chmod(path, 0o644)
		}
	})

	m := build(t, copyRoot)
	if len(m.Progress.Items) != 2 || len(m.Runs.Entries) != 1 || m.Knowledge.Status != KnowledgeFresh {
		t.Fatalf("read-only copy rendered an incomplete view: items=%d runs=%d knowledge=%q",
			len(m.Progress.Items), len(m.Runs.Entries), m.Knowledge.Status)
	}
}

// TestBuildPendingTransactionRendersNoBusinessFacts: while unfinished
// transactions exist, the model names them and stays silent about business
// state (方案 §15.4) — and the git baseline is still reported.
func TestBuildPendingTransactionRendersNoBusinessFacts(t *testing.T) {
	root := fixture(t)
	if err := os.MkdirAll(filepath.Join(root, ".devsys", "local", "txn", "txn-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := build(t, root)

	if m.Trust.State != TrustPending {
		t.Fatalf("trust = %+v", m.Trust)
	}
	if !reflect.DeepEqual(m.Trust.Pending, []string{"txn-1"}) {
		t.Errorf("pending = %v", m.Trust.Pending)
	}
	if len(m.Progress.Items) != 0 || !m.Progress.Degraded || len(m.Runs.Entries) != 0 {
		t.Errorf("business facts rendered: %+v / %+v", m.Progress, m.Runs)
	}
	// The verdict must not claim PASS while the state is untrustworthy.
	if m.Progress.Readiness.Verdict != next.VerdictFail || len(m.Progress.Readiness.Fixes) == 0 {
		t.Errorf("readiness = %+v, want FAIL with the recovery fix", m.Progress.Readiness)
	}
	if m.Knowledge.Status != KnowledgeUnavailable {
		t.Errorf("knowledge = %q", m.Knowledge.Status)
	}
	if m.Project.Reason != pendingReason {
		t.Errorf("project reason = %q", m.Project.Reason)
	}
	if !m.Baseline.Available {
		t.Errorf("baseline = %+v, want the git state even while untrusted", m.Baseline)
	}
}

// TestBuildDegradesOneSectionOnly: a corrupt state file is reported and
// degrades its section; the rest of the view still renders.
func TestBuildDegradesOneSectionOnly(t *testing.T) {
	root := fixture(t)
	writeFile(t, filepath.Join(root, ".devsys", "workitems", "WLM-9.yaml"), "not: [a work item\n")
	m := build(t, root)

	if !m.Progress.Degraded || m.Progress.Reason == "" {
		t.Errorf("progress = %+v", m.Progress)
	}
	if !contains(m.Problems, "workitems/WLM-9.yaml") {
		t.Errorf("problems = %v", m.Problems)
	}
	if len(m.Runs.Entries) != 1 || len(m.Records.Decisions) != 1 || m.Project.Degraded {
		t.Errorf("other sections degraded: runs=%d decisions=%d project=%+v",
			len(m.Runs.Entries), len(m.Records.Decisions), m.Project.Degraded)
	}
}

// TestBuildCapsLists: the list sections honor the limit and say so.
func TestBuildCapsLists(t *testing.T) {
	root := fixture(t)
	ctx := context.Background()
	records := record.New(root)
	for i := range 3 {
		if _, err := records.CreateFinding(ctx, &domain.Finding{Title: fmt.Sprintf("finding %d", i), Status: "open"}); err != nil {
			t.Fatalf("create finding: %v", err)
		}
	}
	m, err := Build(ctx, root, Options{Limit: 2, Now: fixedClock})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(m.Records.Findings) != 2 || !m.Records.Truncated {
		t.Fatalf("findings = %d (truncated=%v)", len(m.Records.Findings), m.Records.Truncated)
	}
	if m.Records.Findings[0].ID > m.Records.Findings[1].ID {
		t.Errorf("findings not in identifier order: %v", m.Records.Findings)
	}
}

// TestBuildReportsTornRunStream: an interrupted append to a run's event stream
// is reported (and named in the sources), never repaired.
func TestBuildReportsTornRunStream(t *testing.T) {
	root := fixture(t)
	matches, err := filepath.Glob(filepath.Join(root, ".devsys", "runs", "run-*.yaml"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("run files = %v (err = %v)", matches, err)
	}
	id := strings.TrimSuffix(filepath.Base(matches[0]), ".yaml")
	writeFile(t, filepath.Join(root, ".devsys", "runs", id+".jsonl"), `{"type":"start"}`)

	m := build(t, root)
	if len(m.Runs.Entries) != 1 || !m.Runs.Entries[0].StreamTorn {
		t.Fatalf("stream_torn = %+v", m.Runs.Entries)
	}
	if !contains(m.Runs.Sources, ".devsys/runs/"+id+".jsonl") {
		t.Errorf("runs sources = %v", m.Runs.Sources)
	}
}

// TestBuildWithoutProject: there is nothing to view, and that is a caller
// error, not an empty model.
func TestBuildWithoutProject(t *testing.T) {
	root := t.TempDir()
	if _, err := Build(context.Background(), root, Options{}); err == nil {
		t.Fatal("build on a project without .devsys/ succeeded")
	}
}

// fingerprint digests every file under root except .git/: git's own index
// housekeeping is not project state, so it stays out of the comparison.
func fingerprint(t *testing.T, root string) map[string]string {
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

func diffFingerprints(before, after map[string]string) string {
	var b strings.Builder
	for path, want := range before {
		if got, ok := after[path]; !ok {
			fmt.Fprintf(&b, "- %s\n", path)
		} else if got != want {
			fmt.Fprintf(&b, "~ %s\n  before %s\n  after  %s\n", path, want, got)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			fmt.Fprintf(&b, "+ %s\n", path)
		}
	}
	return b.String()
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, statErr := d.Info()
		if statErr != nil {
			return statErr
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatalf("copy tree: %v", err)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want || strings.HasPrefix(item, want) {
			return true
		}
	}
	return false
}
