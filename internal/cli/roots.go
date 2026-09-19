package cli

import (
	"os"
	"path/filepath"
	"strings"

	"workloom/internal/app"
	"workloom/internal/project"
)

// resolveRoot finds the project root the command works on:
// DEVSYS_PROJECT_ROOT wins (worktree override, 方案 §4.8), otherwise walk up
// from the working directory looking for a directory carrying `.devsys/`.
// When nothing is found the working directory itself is returned so the
// existing precondition messages keep naming the directory the operator is in.
func resolveRoot() (string, error) {
	if override := strings.TrimSpace(os.Getenv("DEVSYS_PROJECT_ROOT")); override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", errInternal("resolve DEVSYS_PROJECT_ROOT %q: %v", override, err)
		}
		return abs, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", errInternal("resolve working directory: %v", err)
	}
	dir := cwd
	for {
		info, statErr := os.Stat(filepath.Join(dir, project.DevsysDirName))
		if statErr == nil && info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd, nil
		}
		dir = parent
	}
}

// resolveService binds the shared application service to the resolved root.
func resolveService() (*app.Service, error) {
	root, err := resolveRoot()
	if err != nil {
		return nil, err
	}
	return app.New(root), nil
}
