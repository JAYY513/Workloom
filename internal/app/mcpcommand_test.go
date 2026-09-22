package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An npm-installed CLI runs from a nested node_modules tree whose layout
// belongs to the package manager, so the registration has to point at the
// wrapper script: the same path for every install of that package name.
func TestResolveMCPCommandPrefersTheWrapperForNPMTrees(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, "node_modules", "@kaki317", "workloom")
	platform := filepath.Join(wrapper, "node_modules", "@kaki317", "workloom-win32-x64")
	if err := os.MkdirAll(filepath.Join(wrapper, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(platform, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(wrapper, "package.json"), `{"name":"@kaki317/workloom","version":"0.1.12"}`)
	writeTestFile(t, filepath.Join(wrapper, "bin", "workloom.js"), "#!/usr/bin/env node\n")

	got := ResolveMCPCommand(filepath.Join(platform, "workloom.exe"), func(name string) (string, error) {
		if name != "node" {
			t.Fatalf("looked up %q, want node", name)
		}
		return `C:\Program Files\nodejs\node.exe`, nil
	})
	if !strings.HasSuffix(got.Command, "node.exe") {
		t.Fatalf("command = %q, want the node executable", got.Command)
	}
	if len(got.Args) != 1 {
		t.Fatalf("args = %v, want the wrapper script only", got.Args)
	}
	wantScript := filepath.ToSlash(filepath.Join(wrapper, "bin", "workloom.js"))
	if got.Args[0] != wantScript {
		t.Fatalf("script = %q, want %q (the wrapper, not the nested platform binary)", got.Args[0], wantScript)
	}
	argv := got.ServeArgs([]string{"mcp", "serve"})
	if len(argv) != 3 || argv[0] != wantScript || argv[1] != "mcp" || argv[2] != "serve" {
		t.Fatalf("serve argv = %v", argv)
	}
}

// Release-script, go install and source builds keep the binary path: there is
// no package manager between the operator and the executable.
func TestResolveMCPCommandKeepsNonNPMInstallPaths(t *testing.T) {
	for _, exe := range []string{
		`C:\Users\tester\AppData\Local\workloom\workloom.exe`,
		"/usr/local/bin/workloom",
		"",
	} {
		got := ResolveMCPCommand(exe, func(string) (string, error) {
			t.Fatalf("node looked up for non-npm path %q", exe)
			return "", nil
		})
		if got.Command != filepath.ToSlash(exe) || len(got.Args) != 0 {
			t.Fatalf("%q resolved to %+v, want the path itself", exe, got)
		}
	}
}

// Without node on PATH the wrapper cannot be started, so the binary path is
// the honest fallback rather than a registration that never runs.
func TestResolveMCPCommandFallsBackWithoutNode(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, "node_modules", "@kaki317", "workloom")
	platform := filepath.Join(wrapper, "node_modules", "@kaki317", "workloom-win32-x64")
	if err := os.MkdirAll(filepath.Join(wrapper, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(platform, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(wrapper, "package.json"), `{"name":"@kaki317/workloom"}`)
	writeTestFile(t, filepath.Join(wrapper, "bin", "workloom.js"), "#!/usr/bin/env node\n")

	exe := filepath.Join(platform, "workloom.exe")
	got := ResolveMCPCommand(exe, func(string) (string, error) { return "", os.ErrNotExist })
	if got.Command != filepath.ToSlash(exe) || len(got.Args) != 0 {
		t.Fatalf("resolved to %+v, want the binary path when node is missing", got)
	}
}

// A node_modules directory that is not the wrapper (some other package) must
// not be mistaken for it.
func TestResolveMCPCommandIgnoresOtherPackages(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "left-pad")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(pkg, "package.json"), `{"name":"left-pad"}`)
	exe := filepath.Join(pkg, "bin", "left-pad.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}

	got := ResolveMCPCommand(exe, func(string) (string, error) {
		t.Fatal("node looked up for a foreign package")
		return "", nil
	})
	if got.Command != filepath.ToSlash(exe) {
		t.Fatalf("command = %q, want the original path", got.Command)
	}
}

// A project-local install hoists the platform package next to the wrapper
// under the same node_modules. The registration has to come out identical to
// the nested layout's: same script, same command.
func TestResolveMCPCommandCoversHoistedLayouts(t *testing.T) {
	root := t.TempDir()
	modules := filepath.Join(root, "node_modules", "@kaki317")
	wrapper := filepath.Join(modules, "workloom")
	platform := filepath.Join(modules, "workloom-win32-x64")
	if err := os.MkdirAll(filepath.Join(wrapper, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(platform, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(wrapper, "package.json"), `{"name":"@kaki317/workloom","version":"0.1.12"}`)
	writeTestFile(t, filepath.Join(wrapper, "bin", "workloom.js"), "#!/usr/bin/env node\n")

	got := ResolveMCPCommand(filepath.Join(platform, "workloom.exe"), func(name string) (string, error) {
		if name != "node" {
			t.Fatalf("looked up %q, want node", name)
		}
		return "/usr/local/bin/node", nil
	})
	wantScript := filepath.ToSlash(filepath.Join(wrapper, "bin", "workloom.js"))
	if got.Command != "/usr/local/bin/node" || len(got.Args) != 1 || got.Args[0] != wantScript {
		t.Fatalf("hoisted layout resolved to %+v, want node + %s", got, wantScript)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
