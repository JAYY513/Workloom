package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// defaultRootRel is where workspaces live when the project config does not
// name a root (方案 §4.8: 配置的工作区根目录; the default keeps them inside the
// project's .devsys/, which devsys init marks as ignored).
var defaultRootRel = filepath.Join(".devsys", "workspaces")

// Root resolves the workspace root for a project: the configured path when
// set (absolute, or relative to the project root), the default
// <project>/.devsys/workspaces otherwise. A root that exists as a file is a
// precondition failure; a root that does not exist yet is fine (it is created
// with the first workspace).
func Root(projectRoot, configured string) (string, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return "", errors.New("workspace root needs a project root")
	}
	base, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", err
	}
	switch configured = strings.TrimSpace(configured); {
	case configured == "":
		base = filepath.Join(base, defaultRootRel)
	case filepath.IsAbs(configured):
		base = configured
	default:
		base = filepath.Join(base, configured)
	}
	base = filepath.Clean(base)
	if info, err := os.Stat(base); err == nil && !info.IsDir() {
		return "", fmt.Errorf("workspace root %s exists and is not a directory", base)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("workspace root %s: %w", base, err)
	}
	return base, nil
}

// Validate enforces the workspace path invariant of 方案 §4.8: an execution
// workspace must sit inside the configured root. Both paths are made
// absolute and cleaned, and symlinks on the deepest existing ancestor of each
// are resolved, so neither a ".." segment nor a symlink can point outside the
// root. The root itself is not a workspace.
func Validate(root, path string) error {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(path) == "" {
		return errors.New("workspace path check needs both a root and a path")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	absRoot, absPath = filepath.Clean(absRoot), filepath.Clean(absPath)
	if samePath(absRoot, absPath) {
		return fmt.Errorf("path %s is the workspace root itself", absPath)
	}
	if !within(absRoot, absPath) {
		return fmt.Errorf("path %s is outside the workspace root %s", absPath, absRoot)
	}
	resolvedRoot, err := resolveExisting(absRoot)
	if err != nil {
		return fmt.Errorf("resolve workspace root %s: %w", absRoot, err)
	}
	resolvedPath, err := resolveExisting(absPath)
	if err != nil {
		return fmt.Errorf("resolve workspace path %s: %w", absPath, err)
	}
	if samePath(resolvedRoot, resolvedPath) || !within(resolvedRoot, resolvedPath) {
		return fmt.Errorf("path %s escapes the workspace root %s through a symlink", absPath, absRoot)
	}
	return nil
}

// within reports whether path (cleaned, absolute) lies strictly below root.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// samePath compares two cleaned absolute paths the way the host filesystem
// does: Windows paths are case-insensitive.
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// resolveExisting resolves symlinks on the deepest ancestor of path that
// exists, then re-appends the missing tail (which cannot contain symlinks
// because it does not exist yet).
func resolveExisting(path string) (string, error) {
	missing := make([]string, 0, 4)
	current := path
	for {
		_, err := os.Lstat(current)
		switch {
		case err == nil:
			resolved, rerr := filepath.EvalSymlinks(current)
			if rerr != nil {
				return "", rerr
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		case !errors.Is(err, fs.ErrNotExist):
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			// Nothing on the way to the volume root exists: the path is
			// already symbolic-link free.
			return path, nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
