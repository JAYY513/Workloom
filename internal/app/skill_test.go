package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The committed skill files must match the generator. `wire --check` already
// flags drift in a live project; this pins the same invariant in the
// Workloom repo so a one-sided edit fails in CI.
func TestCommittedSkillMatchesGenerator(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	for _, f := range skillFiles() {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.rel)))
		if err != nil {
			t.Fatalf("read %s: %v", f.rel, err)
		}
		if string(got) != f.body {
			t.Errorf("%s differs from the generator; run `workloom wire --skill`", f.rel)
		}
	}
}
