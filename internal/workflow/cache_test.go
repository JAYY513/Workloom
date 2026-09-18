package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cacheRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"workflows", "local"} {
		if err := os.MkdirAll(filepath.Join(root, ".devsys", dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func writePolicyFile(t *testing.T, root, id, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".devsys", "workflows", id+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustResolve(t *testing.T, root, id string) Resolution {
	t.Helper()
	res, err := Resolve(context.Background(), root, id)
	if err != nil {
		t.Fatalf("resolve %s: %v", id, err)
	}
	return res
}

func TestResolveCurrentRefreshesLastKnownGood(t *testing.T) {
	root := cacheRoot(t)
	writePolicyFile(t, root, "sample", minimalPolicy("sample"))
	res := mustResolve(t, root, "sample")
	if res.Source != SourceCurrent || res.Issue != nil || res.Policy == nil {
		t.Fatalf("res = %+v", res)
	}
	cached, err := os.ReadFile(lastKnownGoodPath(root, "sample"))
	if err != nil || string(cached) != minimalPolicy("sample") {
		t.Fatalf("cache = %q, %v", cached, err)
	}
}

func TestResolveFallsBackToLastKnownGoodOnInvalid(t *testing.T) {
	root := cacheRoot(t)
	valid := "---\nid: sample\nname: 旧版本\nversion: 1\nsteps:\n  - id: inspect\n    type: inspect\n---\n"
	writePolicyFile(t, root, "sample", valid)
	mustResolve(t, root, "sample")

	writePolicyFile(t, root, "sample", "---\nname: 缺 id\nversion: \"x\"\n---\n")
	res := mustResolve(t, root, "sample")
	if res.Source != SourceLastKnownGood || res.Issue == nil {
		t.Fatalf("res = %+v", res)
	}
	if res.Policy.Name != "旧版本" {
		t.Errorf("policy = %+v", res.Policy)
	}
	if res.Issue.Line == 0 || res.Issue.Field == "" {
		t.Errorf("issue = %s", res.Issue.String())
	}
	// The snapshot stays at the last valid bytes.
	cached, err := os.ReadFile(lastKnownGoodPath(root, "sample"))
	if err != nil || string(cached) != valid {
		t.Fatalf("cache = %q, %v", cached, err)
	}
}

func TestResolveInvalidWithoutSnapshotFails(t *testing.T) {
	root := cacheRoot(t)
	writePolicyFile(t, root, "sample", "---\nname: 缺 id\nversion: 1\nsteps:\n  - id: inspect\n    type: inspect\n---\n")
	_, err := Resolve(context.Background(), root, "sample")
	if err == nil || !strings.Contains(err.Error(), "invalid") || !strings.Contains(err.Error(), "no last-known-good") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveMissingFallsBackToSnapshot(t *testing.T) {
	root := cacheRoot(t)
	writePolicyFile(t, root, "sample", minimalPolicy("sample"))
	mustResolve(t, root, "sample")
	if err := os.Remove(filepath.Join(root, ".devsys", "workflows", "sample.md")); err != nil {
		t.Fatal(err)
	}
	res := mustResolve(t, root, "sample")
	if res.Source != SourceLastKnownGood || res.Issue == nil || !strings.Contains(res.Issue.Reason, "missing") {
		t.Fatalf("res = %+v", res)
	}
}

func TestResolveMissingWithoutSnapshotFails(t *testing.T) {
	root := cacheRoot(t)
	_, err := Resolve(context.Background(), root, "sample")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveRejectsUnsafeID(t *testing.T) {
	root := cacheRoot(t)
	for _, id := range []string{"../escape", "Bad_ID", "", "a/b"} {
		if _, err := Resolve(context.Background(), root, id); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
}

func TestResolveIgnoresCorruptSnapshot(t *testing.T) {
	root := cacheRoot(t)
	writePolicyFile(t, root, "sample", minimalPolicy("sample"))
	mustResolve(t, root, "sample")
	if err := os.WriteFile(lastKnownGoodPath(root, "sample"), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePolicyFile(t, root, "sample", "---\n---\n")
	if _, err := Resolve(context.Background(), root, "sample"); err == nil {
		t.Fatal("corrupt snapshot was used as fallback")
	}
}
