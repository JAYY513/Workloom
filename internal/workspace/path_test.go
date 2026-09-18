package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRootResolvesConfiguredAndDefault(t *testing.T) {
	project := t.TempDir()
	root, err := Root(project, "")
	if err != nil {
		t.Fatalf("Root(default): %v", err)
	}
	if want := filepath.Join(project, ".devsys", "workspaces"); root != want {
		t.Errorf("Root(default) = %q, want %q", root, want)
	}

	root, err = Root(project, filepath.Join("var", "ws"))
	if err != nil {
		t.Fatalf("Root(relative): %v", err)
	}
	if want := filepath.Join(project, "var", "ws"); root != want {
		t.Errorf("Root(relative) = %q, want %q", root, want)
	}

	abs := filepath.Join(t.TempDir(), "elsewhere")
	root, err = Root(project, abs)
	if err != nil {
		t.Fatalf("Root(absolute): %v", err)
	}
	if root != abs {
		t.Errorf("Root(absolute) = %q, want %q", root, abs)
	}
}

func TestRootRejectsFileAtRootPath(t *testing.T) {
	project := t.TempDir()
	file := filepath.Join(project, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Root(project, file); err == nil {
		t.Fatalf("Root over a file: want an error")
	}
}

func TestValidateAcceptsWorkspaceBelowRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	path := filepath.Join(root, "WLM-1")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Validate(root, path); err != nil {
		t.Errorf("Validate(inside) = %v, want nil", err)
	}
	// The path must not have to exist yet.
	if err := Validate(root, filepath.Join(root, "future", "ws")); err != nil {
		t.Errorf("Validate(missing but inside) = %v, want nil", err)
	}
}

func TestValidateRejectsEscapes(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"parent traversal": filepath.Join(root, "..", "outside"),
		"bare parent":      filepath.Join(root, ".."),
		"sibling prefix":   root + "2",
		"root itself":      root,
		"absolute outside": filepath.Join(base, "other"),
		"volume root":      filepath.VolumeName(root) + string(filepath.Separator),
	}
	for name, path := range cases {
		if err := Validate(root, path); err == nil {
			t.Errorf("Validate(%s: %s) = nil, want a refusal", name, path)
		}
	}
}

func TestValidateRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{root, outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := Validate(root, filepath.Join(link, "ws")); err == nil {
		t.Errorf("Validate(symlink escape) = nil, want a refusal")
	}
	if err := Validate(root, link); err == nil {
		t.Errorf("Validate(symlink directory itself) = nil, want a refusal")
	}
}

// Windows compares paths case-insensitively, so a differently-cased path is
// still the same workspace.
func TestValidateAcceptsCaseVariantsOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive paths are a Windows property")
	}
	root := filepath.Join(t.TempDir(), "Root")
	path := filepath.Join(root, "WLM-1")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	shouted := strings.ToUpper(path)
	if err := Validate(root, shouted); err != nil {
		t.Errorf("Validate(%q) with root %q = %v, want nil", shouted, root, err)
	}
	if err := Validate(strings.ToUpper(root), filepath.Join(root, "..", "elsewhere")); err == nil {
		t.Errorf("Validate(case variant of an escape) = nil, want a refusal")
	}
}
