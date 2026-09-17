package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWriteCreatesReplacesAndLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")

	if err := AtomicWrite(path, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "v1\n" {
		t.Fatalf("content = %q", got)
	}
	if err := AtomicWrite(path, []byte("v2 is longer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "v2 is longer\n" {
		t.Fatalf("content after replace = %q", got)
	}
	if err := AtomicWrite(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); len(got) != 0 {
		t.Fatalf("content after empty write = %q", got)
	}
	assertNoTemporaryFiles(t, dir)

	// The parent directory is created on demand.
	nested := filepath.Join(dir, "a", "b", "state.yaml")
	if err := AtomicWrite(nested, []byte("nested\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(nested); string(got) != "nested\n" {
		t.Fatalf("nested content = %q", got)
	}
}

func TestAtomicWriteCleansUpWhenReplacementFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(target, []byte("x"), 0o644); err == nil {
		t.Fatal("replacing a directory must fail")
	}
	assertNoTemporaryFiles(t, dir)
}

func TestWriteFileSyncCreatesAndTruncates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.yaml")
	if err := WriteFileSync(path, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileSync(path, []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "second\n" {
		t.Fatalf("content = %q", got)
	}
}

func TestReadFileMaybe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.yaml")
	if _, exists, err := readFileMaybe(path); err != nil || exists {
		t.Fatalf("missing file = exists %v, err %v", exists, err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, exists, err := readFileMaybe(path)
	if err != nil || !exists || string(data) != "x" {
		t.Fatalf("existing file = %q, %v, %v", data, exists, err)
	}
}

// assertNoTemporaryFiles fails when an atomic write left staging files behind.
func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestRenameWithRetryReportsTheTarget(t *testing.T) {
	err := renameWithRetry(filepath.Join(t.TempDir(), "missing-source"), filepath.Join(t.TempDir(), "target"))
	if err == nil {
		t.Fatal("renaming a missing source must fail")
	}
	if !strings.Contains(err.Error(), "target") {
		t.Errorf("error should name the destination: %v", err)
	}
	if errors.Is(err, os.ErrNotExist) && !strings.Contains(err.Error(), "replace") {
		t.Errorf("error should be wrapped as a replacement failure: %v", err)
	}
}
