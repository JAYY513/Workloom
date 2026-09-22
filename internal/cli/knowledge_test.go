package cli

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/knowledge"
)

// knowledgeHash mirrors the layer's content hash (whole file, sha256).
func knowledgeHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// The generator contract is exercised by running this test binary as the
// generator: a stub on PATH would be platform-specific, and a real generator
// is an optional external product (方案 §12.6).
const (
	generatorEnv     = "DEVSYS_TEST_GENERATOR"
	generatorExitEnv = "DEVSYS_TEST_GENERATOR_EXIT"
	lockHolderEnv    = "DEVSYS_TEST_LOCK_HOLDER"
)

func TestMain(m *testing.M) {
	if root := os.Getenv(lockHolderEnv); root != "" {
		lockHolderStub(root)
		return
	}
	if os.Getenv(generatorEnv) != "" {
		generatorStub()
		return
	}
	// `mcp install` probes the registered server by starting it. Inside `go
	// test` the resolved executable is the test binary, which cannot serve
	// MCP, so the CLI tests take the documented escape hatch; the probe itself
	// is exercised against a real stdio server in internal/app.
	os.Setenv("DEVSYS_MCP_PROBE", "0")
	os.Exit(m.Run())
}

// generatorStub records the scope it was handed, which is exactly what the
// contract promises a generator: project root, generation range, format
// version.
func generatorStub() {
	project, scopeFile := "", ""
	args := os.Args[1:]
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "--project":
			project = args[i+1]
		case "--scope-file":
			scopeFile = args[i+1]
		}
	}
	if project == "" || scopeFile == "" {
		fmt.Fprintln(os.Stderr, "stub generator: --project and --scope-file are required")
		os.Exit(2)
	}
	if code, err := strconv.Atoi(os.Getenv(generatorExitEnv)); err == nil && code != 0 {
		fmt.Printf("stub generator failing with exit code %d\n", code)
		os.Exit(code)
	}
	data, err := os.ReadFile(scopeFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stub generator: read scope: %v\n", err)
		os.Exit(2)
	}
	out := filepath.Join(project, ".devsys", "knowledge", "generated.json")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		os.Exit(2)
	}
	fmt.Println("stub generator ran")
	os.Exit(0)
}

// lockHolderStub holds the refresh mutex the way a concurrent refresh would,
// so the exclusive lock can be tested across processes rather than only inside
// one.
func lockHolderStub(root string) {
	release, err := knowledge.LockRefresh(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lock holder: %v\n", err)
		os.Exit(2)
	}
	defer func() { _ = release() }()
	fmt.Println("locked")
	for {
		time.Sleep(time.Second)
	}
}

// gitHead returns the repository's current commit.
func gitHead(t *testing.T, root string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// gitCommitAll commits everything under root with a fixed identity, so the
// freshness fixtures do not depend on the machine's git configuration.
func gitCommitAll(t *testing.T, root, message string) string {
	t.Helper()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "--allow-empty", "-m", message}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return gitHead(t, root)
}

// knowledgePage renders a page whose baseline is commit and whose sources are
// the given patterns.
func knowledgePage(commit string, sources ...string) string {
	var b strings.Builder
	b.WriteString("---\nstatus: stable\ntype: module\ntriggers:\n  - 存储层\ndescription: 可靠文本存储的定位\n")
	b.WriteString("source_commit: " + commit + "\n")
	if len(sources) > 0 {
		b.WriteString("sources:\n")
		for _, source := range sources {
			b.WriteString("  - " + source + "\n")
		}
	}
	b.WriteString("---\n\n# 存储层\n")
	return b.String()
}

// TestKnowledgeStatusFreshStaleMissing is the M5.3 acceptance: a change marks
// exactly the pages it touches, and the exit code carries the state.
func TestKnowledgeStatusFreshStaleMissing(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/存储.md", knowledgePage(base, "internal/store/**"))
	gitCommitAll(t, root, "page")

	// The page describes the committed tree: fresh.
	code, out, errOut := run(t, "knowledge", "status")
	if code != CodeOK {
		t.Fatalf("fresh: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if !strings.Contains(out, "knowledge: fresh") {
		t.Fatalf("stdout = %q", out)
	}

	// Changing one covered file makes exactly that page stale.
	writePage(t, root, "internal/store/a.go", "package store // edited\n")
	code, out, _ = run(t, "knowledge", "status")
	if code != CodeStale {
		t.Fatalf("stale: code=%d stdout=%q", code, out)
	}
	if !strings.Contains(out, "affected: docs/repowiki/knowledge/存储.md") {
		t.Fatalf("stdout = %q", out)
	}

	// --json carries the same verdict in a machine-readable shape.
	code, out, _ = run(t, "--json", "knowledge", "status")
	if code != CodeStale {
		t.Fatalf("json stale: code=%d", code)
	}
	var status struct {
		OK       bool     `json:"ok"`
		Status   string   `json:"status"`
		Pages    int      `json:"pages"`
		Affected []string `json:"affected_pages"`
		Head     string   `json:"head"`
		Baseline string   `json:"baseline"`
	}
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatal(err)
	}
	if !status.OK || status.Status != "stale" || status.Pages != 1 || len(status.Affected) != 1 || status.Head == "" {
		t.Fatalf("status = %s", out)
	}

	// Without a page layer the answer is `missing`, which is a state, not a
	// failure (方案 §12.6).
	if err := os.RemoveAll(filepath.Join(root, "docs", "repowiki")); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = run(t, "knowledge", "status")
	if code != CodeMissing {
		t.Fatalf("missing: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if !strings.Contains(out, "无知识页面层") {
		t.Fatalf("stdout = %q", out)
	}
	if strings.Contains(errOut, "workloom:") {
		t.Fatalf("missing was reported as an error: %q", errOut)
	}
}

// A page nobody can attribute or date is not fresh: "cannot tell" must not be
// reported as "up to date".
func TestKnowledgeStatusUnverifiablePage(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/无来源.md", knowledgePage(base))
	gitCommitAll(t, root, "page")

	code, out, errOut := run(t, "knowledge", "status")
	if code != CodeStale {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if !strings.Contains(out, "unverifiable: docs/repowiki/knowledge/无来源.md") {
		t.Fatalf("stdout = %q", out)
	}
}

// Without a configured generator a refresh cannot do the work; the answer is
// the documented degradation, carrying the `missing` code.
func TestKnowledgeRefreshWithoutGenerator(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/存储.md", knowledgePage(base, "internal/store/**"))
	gitCommitAll(t, root, "page")
	writePage(t, root, "internal/store/a.go", "package store // edited\n")

	code, out, _ := run(t, "--json", "knowledge", "refresh", "--affected")
	if code != CodeMissing {
		t.Fatalf("code=%d stdout=%q", code, out)
	}
	var view struct {
		OK      bool     `json:"ok"`
		Missing bool     `json:"missing"`
		Ran     bool     `json:"ran"`
		Pages   []string `json:"pages"`
		Reason  string   `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if !view.OK || !view.Missing || view.Ran || len(view.Pages) != 1 || !strings.Contains(view.Reason, "knowledge_generator") {
		t.Fatalf("view = %s", out)
	}
}

// With a generator configured the refresh hands it the affected pages and
// reports the state it left behind.
func TestKnowledgeRefreshRunsGenerator(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/存储.md", knowledgePage(base, "internal/store/**"))
	gitCommitAll(t, root, "page")
	writePage(t, root, "internal/store/b.go", "package store // new\n")

	// The stub is this test binary; the environment tells it to behave as the
	// generator instead of running the suite again.
	generator := strings.ReplaceAll(os.Args[0], "\\", "/")
	t.Setenv(generatorEnv, "1")
	configPath := filepath.Join(root, ".devsys", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), "schema_version: 1",
		"schema_version: 1\nknowledge_generator: \""+generator+"\"", 1)
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := run(t, "--json", "knowledge", "refresh", "--affected")
	if code != CodeOK {
		t.Fatalf("refresh: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	var view struct {
		OK        bool     `json:"ok"`
		Ran       bool     `json:"ran"`
		Scope     string   `json:"scope"`
		Pages     []string `json:"pages"`
		ScopeFile string   `json:"scope_file"`
		Output    string   `json:"output"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if !view.OK || !view.Ran || view.Scope != "affected" || len(view.Pages) != 1 || !strings.Contains(view.Output, "stub generator ran") {
		t.Fatalf("view = %s", out)
	}

	// The generator saw the contract: the scope file it read is what the
	// refresh wrote, and it names the page to regenerate.
	scopeData, err := os.ReadFile(filepath.Join(root, ".devsys", "knowledge", "generated.json"))
	if err != nil {
		t.Fatalf("generator was not handed a scope: %v", err)
	}
	var scope struct {
		FormatVersion int      `json:"format_version"`
		Mode          string   `json:"mode"`
		Pages         []string `json:"pages"`
		Project       string   `json:"project"`
	}
	if err := json.Unmarshal(scopeData, &scope); err != nil {
		t.Fatal(err)
	}
	if scope.FormatVersion != 1 || scope.Mode != "affected" || len(scope.Pages) != 1 || scope.Project != root {
		t.Fatalf("scope = %s", scopeData)
	}
}

// configureGenerator points the project at the stub generator (this test
// binary re-entered through generatorEnv).
func configureGenerator(t *testing.T, root string) {
	t.Helper()
	generator := strings.ReplaceAll(os.Args[0], "\\", "/")
	t.Setenv(generatorEnv, "1")
	configPath := filepath.Join(root, ".devsys", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), "schema_version: 1",
		"schema_version: 1\nknowledge_generator: \""+generator+"\"", 1)
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A generator that fails is a failure: the report says which run failed, and
// the exit code is the precondition class — never a silent success.
func TestKnowledgeRefreshGeneratorFails(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/存储.md", knowledgePage(base, "internal/store/**"))
	gitCommitAll(t, root, "page")
	writePage(t, root, "internal/store/b.go", "package store // new\n")
	configureGenerator(t, root)
	t.Setenv(generatorExitEnv, "7")

	code, out, errOut := run(t, "knowledge", "refresh", "--affected")
	if code != CodePrecondition {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if !strings.Contains(out, "stub generator failing with exit code 7") {
		t.Fatalf("the generator's output is missing from the report: %q", out)
	}
	if !strings.Contains(errOut, "failed") {
		t.Fatalf("stderr = %q", errOut)
	}

	// Under --json the partial report still reaches the script, marked as a
	// failed run rather than a successful one.
	code, out, _ = run(t, "--json", "knowledge", "refresh", "--affected")
	if code != CodePrecondition {
		t.Fatalf("json code=%d", code)
	}
	var view struct {
		OK     bool   `json:"ok"`
		Ran    bool   `json:"ran"`
		Failed bool   `json:"failed"`
		Error  string `json:"error"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if view.OK || !view.Ran || !view.Failed || view.Error == "" || !strings.Contains(view.Output, "exit code 7") {
		t.Fatalf("view = %s", out)
	}
}

// --full hands the generator every page, and says so in the scope.
func TestKnowledgeRefreshFullScope(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/存储.md", knowledgePage(base, "internal/store/**"))
	writePage(t, root, "docs/repowiki/knowledge/接口.md", knowledgePage(base, "internal/store/**"))
	gitCommitAll(t, root, "pages")
	configureGenerator(t, root)

	code, out, errOut := run(t, "--json", "knowledge", "refresh", "--full")
	if code != CodeOK {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	var view struct {
		Ran   bool     `json:"ran"`
		Scope string   `json:"scope"`
		Pages []string `json:"pages"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Ran || view.Scope != "full" || len(view.Pages) != 2 {
		t.Fatalf("view = %s", out)
	}
	scopeData, err := os.ReadFile(filepath.Join(root, ".devsys", "knowledge", "generated.json"))
	if err != nil {
		t.Fatal(err)
	}
	var scope struct {
		Mode  string   `json:"mode"`
		Pages []string `json:"pages"`
	}
	if err := json.Unmarshal(scopeData, &scope); err != nil {
		t.Fatal(err)
	}
	if scope.Mode != "full" || len(scope.Pages) != 2 {
		t.Fatalf("scope = %s", scopeData)
	}
}

// Protection (M5.4): a locked page and a hand-edited page are kept out of the
// scope and reported; --force regenerates them and says that it overrode them.
func TestKnowledgeRefreshProtection(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/锁定.md",
		knowledgePage(base, "internal/store/**")+"\n")
	writePage(t, root, "docs/repowiki/knowledge/手改.md", knowledgePage(base, "internal/store/**"))
	gitCommitAll(t, root, "pages")

	// The generator's records: the locked page is locked, the other page's hash
	// is stale because the file was edited after it was written.
	lockedBody, err := os.ReadFile(filepath.Join(root, "docs", "repowiki", "knowledge", "锁定.md"))
	if err != nil {
		t.Fatal(err)
	}
	lockedBody = []byte(strings.Replace(string(lockedBody), "---\n\n# 存储层", "protected: true\n---\n\n# 存储层", 1))
	if err := os.WriteFile(filepath.Join(root, "docs", "repowiki", "knowledge", "锁定.md"), lockedBody, 0o644); err != nil {
		t.Fatal(err)
	}
	writePage(t, root, "docs/repowiki/knowledge/手改.md", knowledgePage(base, "internal/store/**")+"\n人工补充。\n")
	stateDir := filepath.Join(root, ".devsys", "knowledge")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePage(t, root, ".devsys/knowledge/state.json", fmt.Sprintf(`{
  "schema_version": 1,
  "baseline": {"commit": "%s"},
  "pages": {
    "docs/repowiki/knowledge/手改.md": {"sources": ["internal/store/**"], "content_hash": "%s"}
  },
  "generator": "stub"
}
`, base, strings.Repeat("0", 64)))
	gitCommitAll(t, root, "protection fixtures")
	configureGenerator(t, root)
	writePage(t, root, "internal/store/b.go", "package store // new\n")

	// Both pages are affected by the new file and both are protected from it.
	code, out, errOut := run(t, "--json", "knowledge", "refresh", "--affected")
	if code != CodeOK {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	var view struct {
		Ran     bool     `json:"ran"`
		Pages   []string `json:"pages"`
		Skipped []struct {
			Path       string `json:"path"`
			Reason     string `json:"reason"`
			Overridden bool   `json:"overridden"`
		} `json:"skipped"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if view.Ran || len(view.Pages) != 0 || len(view.Skipped) != 2 {
		t.Fatalf("view = %s", out)
	}
	reasons := map[string]string{}
	for _, skip := range view.Skipped {
		reasons[skip.Path] = skip.Reason
	}
	if !strings.Contains(reasons["docs/repowiki/knowledge/锁定.md"], "protected") {
		t.Errorf("locked reason = %q", reasons["docs/repowiki/knowledge/锁定.md"])
	}
	if !strings.Contains(reasons["docs/repowiki/knowledge/手改.md"], "手工修改") {
		t.Errorf("edited reason = %q", reasons["docs/repowiki/knowledge/手改.md"])
	}
	if !strings.Contains(view.Reason, "protected or hand-edited") {
		t.Errorf("reason = %q", view.Reason)
	}

	// --force regenerates both, and still reports what it overrode.
	code, out, errOut = run(t, "--json", "knowledge", "refresh", "--affected", "--force")
	if code != CodeOK {
		t.Fatalf("force: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Ran || len(view.Pages) != 2 || len(view.Skipped) != 2 {
		t.Fatalf("view = %s", out)
	}
	for _, skip := range view.Skipped {
		if !skip.Overridden {
			t.Errorf("%s not marked overridden", skip.Path)
		}
	}
}

// --full with every page protected regenerates nothing: the generator must not
// be started with an empty scope, and the answer says what happened.
func TestKnowledgeRefreshFullAllSkipped(t *testing.T) {
	root, _ := gatedProject(t)
	base := gitCommitAll(t, root, "empty")
	writePage(t, root, "docs/repowiki/knowledge/锁定.md",
		strings.Replace(knowledgePage(base, "internal/store/**"), "---\n\n# 存储层",
			"protected: true\n---\n\n# 存储层", 1))
	gitCommitAll(t, root, "locked page")
	configureGenerator(t, root)

	code, out, errOut := run(t, "--json", "knowledge", "refresh", "--full")
	if code != CodeOK {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	var view struct {
		Ran     bool     `json:"ran"`
		Pages   []string `json:"pages"`
		Skipped []struct {
			Path string `json:"path"`
		} `json:"skipped"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if view.Ran || len(view.Pages) != 0 || len(view.Skipped) != 1 {
		t.Fatalf("view = %s", out)
	}
	if !strings.Contains(view.Reason, "--force") {
		t.Fatalf("reason = %q", view.Reason)
	}
	if _, err := os.Stat(filepath.Join(root, ".devsys", "knowledge", "generated.json")); err == nil {
		t.Fatal("the generator ran with an empty scope")
	}
}

// writeState records the generator's page mapping, as a generator run would.
func writeState(t *testing.T, root, baseline string, pages map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".devsys", "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries := map[string]map[string]any{}
	for path, hash := range pages {
		entries[path] = map[string]any{"sources": []string{"internal/store/**"}, "content_hash": hash}
	}
	data, err := json.MarshalIndent(map[string]any{
		"schema_version": 1,
		"baseline":       map[string]string{"commit": baseline},
		"pages":          entries,
		"generator":      "stub",
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".devsys", "knowledge", "state.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeCheckpoint records an interrupted run: the pages it had asked for and
// had not finished when it died.
func writeCheckpoint(t *testing.T, root, mode string, pages []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".devsys", "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(map[string]any{
		"schema_version": 1,
		"pid":            1234,
		"host":           "host",
		"phase":          "generating",
		"mode":           mode,
		"started_at":     "2026-01-01T00:00:00Z",
		"updated_at":     "2026-01-01T00:00:00Z",
		"pages":          pages,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".devsys", "knowledge", "run.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A refresh that finds a checkpoint continues it: the pages already written are
// left out, and the run says it resumed.
func TestKnowledgeRefreshResumes(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/未完成.md", knowledgePage(base, "internal/store/**"))
	writePage(t, root, "docs/repowiki/knowledge/已完成.md", knowledgePage(base, "internal/store/**"))
	gitCommitAll(t, root, "pages")
	// The generator recorded the finished page correctly and the other one not
	// at all: the interruption happened between them.
	finished, err := os.ReadFile(filepath.Join(root, "docs", "repowiki", "knowledge", "已完成.md"))
	if err != nil {
		t.Fatal(err)
	}
	// The finished page matches its record; the unfinished one has no record at
	// all (the generator died before writing it), which is not a hand edit.
	writeState(t, root, base, map[string]string{
		"docs/repowiki/knowledge/已完成.md": knowledgeHash(finished),
	})
	writeCheckpoint(t, root, "affected", []string{
		"docs/repowiki/knowledge/已完成.md",
		"docs/repowiki/knowledge/未完成.md",
	})
	configureGenerator(t, root)

	code, out, errOut := run(t, "--json", "knowledge", "refresh", "--affected")
	if code != CodeOK {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	var view struct {
		Resumed    bool     `json:"resumed"`
		Ran        bool     `json:"ran"`
		Pages      []string `json:"pages"`
		Checkpoint string   `json:"checkpoint"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Resumed || !view.Ran || len(view.Pages) != 1 || !strings.HasSuffix(view.Pages[0], "未完成.md") {
		t.Fatalf("view = %s", out)
	}
	if view.Checkpoint != ".devsys/knowledge/run.json" {
		t.Fatalf("checkpoint = %q", view.Checkpoint)
	}
	// A clean finish clears the checkpoint: the next run starts from freshness.
	if _, err := os.Stat(filepath.Join(root, ".devsys", "knowledge", "run.json")); !os.IsNotExist(err) {
		t.Fatalf("checkpoint survived a clean run: %v", err)
	}
}

// A checkpoint whose pages are all written already is closed out without
// starting the generator.
func TestKnowledgeRefreshResumeNothingLeft(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/已完成.md", knowledgePage(base, "internal/store/**"))
	gitCommitAll(t, root, "page")
	finished, err := os.ReadFile(filepath.Join(root, "docs", "repowiki", "knowledge", "已完成.md"))
	if err != nil {
		t.Fatal(err)
	}
	writeState(t, root, base, map[string]string{"docs/repowiki/knowledge/已完成.md": knowledgeHash(finished)})
	writeCheckpoint(t, root, "full", []string{"docs/repowiki/knowledge/已完成.md"})
	configureGenerator(t, root)

	code, out, errOut := run(t, "--json", "knowledge", "refresh", "--full")
	if code != CodeOK {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	var view struct {
		Resumed bool   `json:"resumed"`
		Ran     bool   `json:"ran"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Resumed || view.Ran || !strings.Contains(view.Reason, "already written") {
		t.Fatalf("view = %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, ".devsys", "knowledge", "generated.json")); err == nil {
		t.Fatal("the generator ran with nothing to do")
	}
	if _, err := os.Stat(filepath.Join(root, ".devsys", "knowledge", "run.json")); !os.IsNotExist(err) {
		t.Fatal("checkpoint was not cleared")
	}
}

// Two refreshes cannot run at once, and the refusal names the holder.
func TestKnowledgeRefreshRefusesWhenLocked(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/存储.md", knowledgePage(base, "internal/store/**"))
	gitCommitAll(t, root, "page")
	configureGenerator(t, root)
	// There is work to do (a new file under the page's sources), so the refresh
	// reaches the mutex; the checkpoint is what tells the refusal who holds it.
	writePage(t, root, "internal/store/b.go", "package store // new\n")
	writeCheckpoint(t, root, "affected", []string{"docs/repowiki/knowledge/存储.md"})

	release, err := knowledge.LockRefresh(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = release() }()
	code, _, errOut := run(t, "knowledge", "refresh", "--affected")
	if code != CodePrecondition {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(errOut, "another refresh is already running") || !strings.Contains(errOut, "pid 1234") {
		t.Fatalf("stderr = %q", errOut)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	// With the mutex free the same command proceeds.
	if code, _, errOut := run(t, "knowledge", "refresh", "--affected"); code != CodeOK {
		t.Fatalf("after release: code=%d stderr=%q", code, errOut)
	}
}

// The mutex holds across processes: a refresh in another process is refused,
// and the refusal names the holder.
func TestKnowledgeRefreshLockedByAnotherProcess(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/存储.md", knowledgePage(base, "internal/store/**"))
	gitCommitAll(t, root, "page")
	configureGenerator(t, root)
	writePage(t, root, "internal/store/b.go", "package store // new\n")

	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), lockHolderEnv+"="+root)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var childErr strings.Builder
	child.Stderr = &childErr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = child.Process.Kill()
		_, _ = child.Process.Wait()
	}()
	reader := bufio.NewReader(stdout)
	line, lineErr := reader.ReadString('\n')
	if lineErr != nil || !strings.Contains(line, "locked") {
		t.Fatalf("lock holder did not start: %q %v stderr=%q", line, lineErr, childErr.String())
	}

	code, _, errOut := run(t, "knowledge", "refresh", "--affected")
	if code != CodePrecondition {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(errOut, "another refresh is already running") || !strings.Contains(errOut, "holder unknown") {
		t.Fatalf("stderr = %q", errOut)
	}
}

// A failed run leaves a checkpoint that the next run resumes.
func TestKnowledgeRefreshResumesAfterFailure(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/存储.md", knowledgePage(base, "internal/store/**"))
	gitCommitAll(t, root, "page")
	configureGenerator(t, root)
	writePage(t, root, "internal/store/b.go", "package store // new\n")

	t.Setenv(generatorExitEnv, "7")
	if code, _, _ := run(t, "knowledge", "refresh", "--affected"); code != CodePrecondition {
		t.Fatalf("failing run: code=%d", code)
	}
	// The checkpoint survived the failure and names the page that was left.
	checkpoint, err := knowledge.LoadCheckpoint(root)
	if err != nil {
		t.Fatalf("checkpoint after failure: %v", err)
	}
	if checkpoint.Phase != "failed" || len(checkpoint.Pages) != 1 || checkpoint.Error == "" {
		t.Fatalf("checkpoint = %+v", checkpoint)
	}
	// The next run resumes it instead of restarting, and clears it on success.
	t.Setenv(generatorExitEnv, "")
	code, out, errOut := run(t, "--json", "knowledge", "refresh", "--affected")
	if code != CodeOK {
		t.Fatalf("resume: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	var view struct {
		Resumed bool     `json:"resumed"`
		Ran     bool     `json:"ran"`
		Pages   []string `json:"pages"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Resumed || !view.Ran || len(view.Pages) != 1 {
		t.Fatalf("view = %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, ".devsys", "knowledge", "run.json")); !os.IsNotExist(err) {
		t.Fatalf("checkpoint survived a successful resume: %v", err)
	}
}

// --force reaches the resumed pages too, and a protected page stays reported.
func TestKnowledgeRefreshResumeWithForce(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/锁定.md",
		strings.Replace(knowledgePage(base, "internal/store/**"), "---\n\n# 存储层",
			"protected: true\n---\n\n# 存储层", 1))
	gitCommitAll(t, root, "page")
	writeCheckpoint(t, root, "affected", []string{"docs/repowiki/knowledge/锁定.md"})
	configureGenerator(t, root)

	if _, err := os.Stat(filepath.Join(root, ".devsys", "knowledge", "run.json")); err != nil {
		t.Fatalf("checkpoint was not written: %v", err)
	}
	// Without --force the resumed page is protected: nothing runs.
	code, out, _ := run(t, "--json", "knowledge", "refresh", "--affected")
	if code != CodeOK {
		t.Fatalf("code=%d view=%s", code, out)
	}
	var view struct {
		Ran     bool `json:"ran"`
		Skipped []struct {
			Path       string `json:"path"`
			Overridden bool   `json:"overridden"`
		} `json:"skipped"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if view.Ran || len(view.Skipped) != 1 || view.Skipped[0].Overridden {
		t.Fatalf("view = %s", out)
	}

	if _, err := os.Stat(filepath.Join(root, ".devsys", "knowledge", "run.json")); !os.IsNotExist(err) {
		t.Fatalf("a checkpoint with nothing actionable survived: %v", err)
	}

	// With --force it runs, and the override is still reported.
	writeCheckpoint(t, root, "affected", []string{"docs/repowiki/knowledge/锁定.md"})
	code, out, errOut := run(t, "--json", "knowledge", "refresh", "--affected", "--force")
	if code != CodeOK {
		t.Fatalf("force: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Ran || len(view.Skipped) != 1 || !view.Skipped[0].Overridden {
		t.Fatalf("view = %s", out)
	}
}

// A corrupted checkpoint is reported as invalid state, not silently ignored.
func TestKnowledgeRefreshRejectsCorruptCheckpoint(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/存储.md", knowledgePage(base, "internal/store/**"))
	gitCommitAll(t, root, "page")
	configureGenerator(t, root)
	writePage(t, root, "internal/store/b.go", "package store // new\n")
	writePage(t, root, ".devsys/knowledge/run.json", "{not json")

	code, _, errOut := run(t, "knowledge", "refresh", "--affected")
	if code != CodeInvalid {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(errOut, "run.json") {
		t.Fatalf("stderr = %q", errOut)
	}
}

// The layered assembly is the point of M5.6: `context get --task` carries the
// project summary, the task with its records, and the pages the task touches.
func TestContextGetTaskLayers(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/存储.md", knowledgePage(base, "internal/store/**"))
	gitCommitAll(t, root, "page")

	// The page's triggers are 存储层: the task text has to contain one of them
	// for a trigger match, which is exactly what the layer promises.
	code, out, errOut := run(t, "workitem", "create", "--title", "整理存储层的写入路径", "--actor", "t", "--reason", "fixture")
	if code != CodeOK {
		t.Fatalf("workitem create: code=%d stderr=%q", code, errOut)
	}
	id := strings.Fields(out)[0]

	// Trigger match: the page's trigger 存储层 appears in the title.
	code, out, errOut = run(t, "--json", "context", "get", "--task", id)
	if code != CodeOK {
		t.Fatalf("context get --task: code=%d stderr=%q", code, errOut)
	}
	var first struct {
		Summary struct {
			Project struct {
				ID string `json:"id"`
			} `json:"project"`
		} `json:"summary"`
		Task struct {
			WorkItem struct {
				ID string `json:"id"`
			} `json:"workitem"`
			Knowledge struct {
				Baseline string `json:"baseline"`
				Pages    []struct {
					Path   string `json:"path"`
					Match  string `json:"match"`
					Reason string `json:"reason"`
					Stale  bool   `json:"stale"`
				} `json:"pages"`
				Degraded bool   `json:"degraded"`
				Notice   string `json:"notice"`
			} `json:"knowledge"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(out), &first); err != nil {
		t.Fatal(err)
	}
	if first.Summary.Project.ID == "" || first.Task.WorkItem.ID != id {
		t.Fatalf("assembly = %s", out)
	}
	if len(first.Task.Knowledge.Pages) != 1 || first.Task.Knowledge.Pages[0].Match != "trigger" || first.Task.Knowledge.Pages[0].Reason != "存储层" {
		t.Fatalf("knowledge = %s", out)
	}
	// No generator has written state.json here, so the layer has no baseline:
	// the verdict rests on the page's own source_commit instead, and the
	// assembly says so by leaving the field empty rather than inventing one.
	if first.Task.Knowledge.Baseline != "" {
		t.Fatalf("baseline = %q, want empty", first.Task.Knowledge.Baseline)
	}
	if first.Task.Knowledge.Pages[0].Stale {
		t.Fatalf("page judged stale although nothing changed: %s", out)
	}

	// The same task assembles identically: a second read must not see a
	// different world.
	code, again, errOut := run(t, "--json", "context", "get", "--task", id)
	if code != CodeOK {
		t.Fatalf("second assembly: code=%d stderr=%q", code, errOut)
	}
	if again != out {
		t.Fatalf("assembly is not stable:\n%s\n---\n%s", out, again)
	}

	// A named path pulls in a page whose sources cover it even when the task
	// text says nothing about it.
	code, out, errOut = run(t, "--json", "context", "get", "--task", id, "--path", "internal/store/other.go")
	_ = errOut
	if code != CodeOK {
		t.Fatalf("context get --path: code=%d stderr=%q", code, errOut)
	}
	if err := json.Unmarshal([]byte(out), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Task.Knowledge.Pages) != 1 {
		t.Fatalf("knowledge = %s", out)
	}
	page := first.Task.Knowledge.Pages[0]
	if page.Match != "trigger" || !strings.Contains(page.Reason, "存储层") {
		t.Fatalf("match = %q reason = %q", page.Match, page.Reason)
	}
}

// Without a page layer the assembly still works and says why the knowledge
// section is empty.
func TestContextGetTaskDegrades(t *testing.T) {
	gatedProject(t)
	code, out, errOut := run(t, "workitem", "create", "--title", "没有任何知识页", "--actor", "t", "--reason", "fixture")
	if code != CodeOK {
		t.Fatalf("workitem create: code=%d stderr=%q", code, errOut)
	}
	id := strings.Fields(out)[0]
	code, out, errOut = run(t, "--json", "context", "get", "--task", id)
	if code != CodeOK {
		t.Fatalf("context get --task: code=%d stderr=%q", code, errOut)
	}
	var view struct {
		Task struct {
			Knowledge struct {
				Degraded bool   `json:"degraded"`
				Notice   string `json:"notice"`
				Pages    []any  `json:"pages"`
			} `json:"knowledge"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Task.Knowledge.Degraded || len(view.Task.Knowledge.Pages) != 0 || !strings.Contains(view.Task.Knowledge.Notice, "生成器") {
		t.Fatalf("degradation = %s", out)
	}
}

// A bundle whose pages keep their sources in the generator's state becomes
// attributable once the configured adapter imports them: the same pages stop
// being "unverifiable" and are judged against the tree.
func TestKnowledgeStatusUsesAdapterImport(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	page := strings.Replace(knowledgePage(base, "internal/store/**"), "sources:\n  - internal/store/**\n", "", 1)
	writePage(t, root, "docs/repowiki/knowledge/存储.md", page)
	gitCommitAll(t, root, "page")
	pagePath := filepath.Join(root, "docs", "repowiki", "knowledge", "存储.md")
	data, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatal(err)
	}
	writePage(t, root, ".repowiki/state.json", fmt.Sprintf(`{
  "git": {"commit": "%s", "branch": "master"},
  "pages": {"knowledge/存储.md": {"content_hash": "%s", "sources": ["internal/store/**"]}}
}
`, base, knowledgeHash(data)))

	// Without an adapter the page has no sources: unverifiable, hence stale.
	code, out, _ := run(t, "--json", "knowledge", "status")
	if code != CodeStale {
		t.Fatalf("without adapter: code=%d out=%s", code, out)
	}
	if !strings.Contains(out, "unverifiable_pages") {
		t.Fatalf("without adapter: %s", out)
	}

	// Naming the adapter imports the mapping (the tool itself is only needed to
	// generate, not to read a page's provenance).
	configPath := filepath.Join(root, ".devsys", "config.yaml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(strings.Replace(string(config), "schema_version: 1",
		"schema_version: 1\nknowledge_generator: repowiki", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "--json", "knowledge", "status")
	if code != CodeOK {
		t.Fatalf("with adapter: code=%d stderr=%q out=%s", code, errOut, out)
	}
	var view struct {
		Status           string   `json:"status"`
		Adapter          string   `json:"adapter"`
		AdapterInstalled bool     `json:"adapter_installed"`
		Baseline         string   `json:"baseline"`
		Affected         []string `json:"affected_pages"`
		Unverifiable     []string `json:"unverifiable_pages"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if view.Status != "fresh" || view.Adapter != "repowiki" || view.Baseline != base {
		t.Fatalf("view = %s", out)
	}
	if len(view.Affected) != 0 || len(view.Unverifiable) != 0 {
		t.Fatalf("view = %s", out)
	}
	if !view.AdapterInstalled {
		t.Fatalf("repowiki is installed on this machine but the probe says otherwise: %s", out)
	}
}

// --path brings in a page whose triggers do not match the task at all: the
// sources half of the selection rule, on its own.
func TestContextGetTaskPathOnlyMatch(t *testing.T) {
	root, _ := gatedProject(t)
	writePage(t, root, "internal/store/a.go", "package store\n")
	base := gitCommitAll(t, root, "source")
	writePage(t, root, "docs/repowiki/knowledge/数据库.md",
		strings.Replace(knowledgePage(base, "internal/store/**"), "- 存储层", "- 数据库", 1))
	gitCommitAll(t, root, "page")

	code, out, errOut := run(t, "workitem", "create", "--title", "无关的任务", "--description", "与页面触发词毫无关系", "--actor", "t", "--reason", "fixture")
	if code != CodeOK {
		t.Fatalf("workitem create: code=%d stderr=%q", code, errOut)
	}
	id := strings.Fields(out)[0]

	var view struct {
		Task struct {
			Knowledge struct {
				Pages []struct {
					Path   string `json:"path"`
					Match  string `json:"match"`
					Reason string `json:"reason"`
				} `json:"pages"`
			} `json:"knowledge"`
		} `json:"task"`
	}
	// Without --path the page is not selected: no trigger matches.
	code, out, errOut = run(t, "--json", "context", "get", "--task", id)
	if code != CodeOK {
		t.Fatalf("context get: code=%d stderr=%q", code, errOut)
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Task.Knowledge.Pages) != 0 {
		t.Fatalf("a page was selected without a match: %s", out)
	}
	// With --path naming a covered file the page joins through its sources.
	code, out, errOut = run(t, "--json", "context", "get", "--task", id, "--path", "internal/store/a.go")
	if code != CodeOK {
		t.Fatalf("context get --path: code=%d stderr=%q", code, errOut)
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Task.Knowledge.Pages) != 1 {
		t.Fatalf("sources match missing: %s", out)
	}
	page := view.Task.Knowledge.Pages[0]
	if page.Match != "sources" || !strings.Contains(page.Reason, "internal/store/**") {
		t.Fatalf("page = %+v", page)
	}
}
