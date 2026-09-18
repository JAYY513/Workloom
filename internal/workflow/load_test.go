package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadScansSortedAndIsolatesInvalidFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".devsys", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Written out of order; Load must still report file name order.
	if err := os.WriteFile(filepath.Join(dir, "beta.md"), []byte("---\nname: 缺 id\nversion: 1\nsteps:\n  - id: inspect\n    type: inspect\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "alpha.md"), []byte(minimalPolicy("alpha")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a policy"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := Load(root)
	if len(results) != 3 {
		t.Fatalf("results = %v", results)
	}
	if results[0].File != "workflows/alpha.md" || results[0].Policy == nil {
		t.Errorf("results[0] = %+v", results[0])
	}
	if results[1].File != "workflows/beta.md" {
		t.Errorf("results[1] = %+v", results[1])
	}
	if results[1].Policy != nil {
		t.Errorf("invalid policy returned: %+v", results[1].Policy)
	}
	foundRequired := false
	for _, is := range results[1].Issues {
		if is.Severity == SeverityError && is.Field == "id" {
			foundRequired = true
		}
	}
	if !foundRequired {
		t.Errorf("beta.md issues = %v", results[1].Issues)
	}
	if results[2].File != "workflows/notes.txt" || len(results[2].Issues) != 1 || results[2].Issues[0].Severity != SeverityWarning {
		t.Errorf("results[2] = %+v", results[2])
	}
}

func TestLoadMissingWorkflowsDir(t *testing.T) {
	results := Load(t.TempDir())
	if len(results) != 1 || results[0].File != "workflows/" {
		t.Fatalf("results = %+v", results)
	}
	if len(results[0].Issues) != 1 || results[0].Issues[0].Severity != SeverityError {
		t.Fatalf("issues = %+v", results[0].Issues)
	}
}

func TestExamplePoliciesParse(t *testing.T) {
	dir := filepath.Join("..", "..", "docs", "examples", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read examples: %v", err)
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		p, issues := Parse("workflows/"+e.Name(), data)
		if p == nil {
			t.Fatalf("%s: %v", e.Name(), issues)
		}
		requireNoErrors(t, issues)
		if len(p.Steps) == 0 || p.Body == "" {
			t.Errorf("%s: steps=%d body=%q", e.Name(), len(p.Steps), p.Body)
		}
		count++
	}
	if count != 3 {
		t.Fatalf("examples = %d, want 3", count)
	}
}
