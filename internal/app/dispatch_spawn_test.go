package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The dispatch spawner must start attempts through the same command form MCP
// registration writes into client configs: an npm-managed install resolves to
// node + the wrapper script, so an attempt spawned from a nested node_modules
// tree does not embed the package manager's layout in its own spawn chain.
func TestAttemptSpawnCommandMatchesMCPRegistrationInNPMTrees(t *testing.T) {
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

	exe := filepath.ToSlash(filepath.Join(platform, "workloom.exe"))
	lookPath := func(name string) (string, error) {
		if name != "node" {
			t.Fatalf("looked up %q, want node", name)
		}
		return `C:\Program Files\nodejs\node.exe`, nil
	}
	argv := []string{"run", "exec", "--id", "run-1", "--actor", "dispatch", "--reason", "tick"}
	registered := ResolveMCPCommand(exe, lookPath)
	got := attemptSpawnCommand(exe, argv, lookPath)
	if got.Command != registered.Command {
		t.Fatalf("attempt command = %q, want the MCP registration form %q", got.Command, registered.Command)
	}
	wantArgs := append(append([]string{}, registered.Args...), argv...)
	if !slices.Equal(got.Args, wantArgs) {
		t.Fatalf("attempt args = %q, want %q (wrapper script + devsys argv)", got.Args, wantArgs)
	}
}

// Release-script, go install and source builds keep the bare binary: there is
// no package manager in the spawn chain, and node must not be consulted.
func TestAttemptSpawnCommandKeepsPlainInstalls(t *testing.T) {
	argv := []string{"run", "exec", "--id", "run-1"}
	for _, exe := range []string{
		`C:\Users\tester\AppData\Local\workloom\workloom.exe`,
		"/usr/local/bin/workloom",
	} {
		got := attemptSpawnCommand(exe, argv, func(string) (string, error) {
			t.Fatalf("node looked up for non-npm path %q", exe)
			return "", nil
		})
		if got.Command != filepath.ToSlash(exe) || !slices.Equal(got.Args, argv) {
			t.Fatalf("%q spawned as %+v, want the bare binary with the devsys argv", exe, got)
		}
	}
}
