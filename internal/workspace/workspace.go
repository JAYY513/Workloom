// Package workspace manages execution workspaces for runs (方案 §4.8): one
// workspace per work item below a configured root, its lifecycle hooks, and
// the path invariants that keep an agent inside its workspace. A git project
// uses a linked worktree; a directory without git uses a plain directory.
//
// The package is pure filesystem and git: it knows nothing about .devsys
// state, runs or events. The application service binds it to the project and
// records what happened.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// branchPrefix namespaces the branches devsys creates for workspaces, so they
// are recognizable next to human ones.
const branchPrefix = "devsys/"

// Workspace is one execution workspace, addressed by a sanitized key and
// reused across runs while it exists (方案 §4.8). Branch is set for git
// worktrees and empty for directory workspaces.
type Workspace struct {
	Key     string      `json:"key"`
	Path    string      `json:"path"`
	Branch  string      `json:"branch"`
	Created bool        `json:"created"`
	Hook    *HookReport `json:"hook,omitempty"`
}

// EnsureOptions describes the workspace a work item needs.
type EnsureOptions struct {
	ProjectRoot string
	// Root is the configured workspace root; empty uses the default below
	// the project's .devsys/.
	Root       string
	Identifier string
	// Branch overrides the derived branch; empty uses devsys/<key>.
	Branch string
	Hooks  map[string]Hook
}

// Ensure creates the workspace for an identifier or reuses the existing one.
//
// Git projects get a worktree of the repository on the expected branch; a
// workspace that already exists is reused as it stands and after_create is
// not run again. Non-git projects get a plain directory under the workspace
// root, with no branch. A newly created workspace whose after_create hook
// fails is taken back down so a failed bootstrap leaves no half-made
// workspace behind.
func Ensure(ctx context.Context, opts EnsureOptions) (Workspace, error) {
	if strings.TrimSpace(opts.ProjectRoot) == "" {
		return Workspace{}, errors.New("workspace needs a project root")
	}
	if strings.TrimSpace(opts.Identifier) == "" {
		return Workspace{}, errors.New("workspace needs an identifier")
	}
	root, err := Root(opts.ProjectRoot, opts.Root)
	if err != nil {
		return Workspace{}, err
	}
	key := Key(opts.Identifier)
	path := filepath.Join(root, key)
	if err := Validate(root, path); err != nil {
		return Workspace{}, err
	}
	if GitProject(opts.ProjectRoot) {
		return ensureWorktree(ctx, opts, key, path)
	}
	return ensureDirectory(ctx, opts, key, path)
}

func ensureWorktree(ctx context.Context, opts EnsureOptions, key, path string) (Workspace, error) {
	branch := strings.TrimSpace(opts.Branch)
	if branch == "" {
		branch = branchPrefix + key
	}

	if info, err := os.Stat(path); err == nil {
		if !info.IsDir() {
			return Workspace{}, fmt.Errorf("workspace %s exists and is not a directory", path)
		}
		ok, err := sameRepo(path, opts.ProjectRoot)
		if err != nil {
			return Workspace{}, err
		}
		if !ok {
			return Workspace{}, fmt.Errorf("workspace %s is not a worktree of %s", path, opts.ProjectRoot)
		}
		current, err := currentBranch(path)
		if err != nil {
			return Workspace{}, err
		}
		if current != branch {
			return Workspace{}, fmt.Errorf("workspace %s is on branch %q, expected %q", path, current, branch)
		}
		return Workspace{Key: key, Path: path, Branch: branch}, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Workspace{}, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Workspace{}, fmt.Errorf("create workspace root %s: %w", filepath.Dir(path), err)
	}
	existing, err := branchExists(opts.ProjectRoot, branch)
	if err != nil {
		return Workspace{}, err
	}
	base := ""
	if existing {
		if base, err = branchSHA(opts.ProjectRoot, branch); err != nil {
			return Workspace{}, err
		}
	} else if base, err = headSHA(opts.ProjectRoot); err != nil {
		return Workspace{}, err
	}
	if err := addWorktree(opts.ProjectRoot, path, branch, !existing); err != nil {
		return Workspace{}, err
	}

	hook := opts.Hooks[HookAfterCreate]
	rep, herr := RunHook(ctx, RunHookOptions{
		Name: HookAfterCreate,
		Hook: hook,
		Dir:  path,
		Env:  HookEnv(opts.ProjectRoot, path, branch, opts.Identifier, HookAfterCreate),
	})
	if herr != nil {
		if residue := discard(opts.ProjectRoot, path, branch, !existing, base); residue != nil {
			return Workspace{}, fmt.Errorf("%w; cleanup left residue: %v", herr, residue)
		}
		return Workspace{}, herr
	}
	out := Workspace{Key: key, Path: path, Branch: branch, Created: true}
	if rep.Ran {
		out.Hook = &rep
	}
	return out, nil
}

func ensureDirectory(ctx context.Context, opts EnsureOptions, key, path string) (Workspace, error) {
	if info, err := os.Stat(path); err == nil {
		if !info.IsDir() {
			return Workspace{}, fmt.Errorf("workspace %s exists and is not a directory", path)
		}
		return Workspace{Key: key, Path: path}, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Workspace{}, err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return Workspace{}, fmt.Errorf("create workspace %s: %w", path, err)
	}
	hook := opts.Hooks[HookAfterCreate]
	rep, herr := RunHook(ctx, RunHookOptions{
		Name: HookAfterCreate,
		Hook: hook,
		Dir:  path,
		Env:  HookEnv(opts.ProjectRoot, path, "", opts.Identifier, HookAfterCreate),
	})
	if herr != nil {
		if remErr := os.RemoveAll(path); remErr != nil {
			return Workspace{}, fmt.Errorf("%w; cleanup left residue: %v", herr, remErr)
		}
		return Workspace{}, herr
	}
	out := Workspace{Key: key, Path: path, Created: true}
	if rep.Ran {
		out.Hook = &rep
	}
	return out, nil
}

// discard takes a half-made workspace back down: the worktree registration
// (which also removes the directory), the directory itself when git could not,
// and the branch when this call created it and nothing was committed to it.
// It reports what could not be undone instead of leaving it silent.
func discard(projectRoot, path, branch string, createdBranch bool, base string) error {
	var problems []string
	if err := removeWorktree(projectRoot, path, true); err != nil {
		problems = append(problems, err.Error())
	}
	if err := pruneWorktrees(projectRoot); err != nil {
		problems = append(problems, err.Error())
	}
	if _, err := os.Stat(path); err == nil {
		if err := os.RemoveAll(path); err != nil {
			problems = append(problems, fmt.Sprintf("remove %s: %v", path, err))
		}
	}
	if createdBranch {
		// The branch is deleted only while it still points at the base this
		// call branched from: a hook that committed during after_create left
		// work behind, and discarding it would destroy evidence nobody asked
		// to destroy.
		sha, err := branchSHA(projectRoot, branch)
		switch {
		case err != nil:
			problems = append(problems, fmt.Sprintf("inspect branch %s: %v", branch, err))
		case sha == base:
			if err := deleteBranch(projectRoot, branch); err != nil {
				problems = append(problems, err.Error())
			}
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// RemoveOptions describes the workspace to take down.
type RemoveOptions struct {
	ProjectRoot string
	Root        string
	// Path names the workspace directly; Identifier derives it from the key.
	Path       string
	Identifier string
	Force      bool
	Hooks      map[string]Hook
}

// RemoveReport says what removal did.
type RemoveReport struct {
	Path     string      `json:"path"`
	Removed  bool        `json:"removed"`
	Notice   string      `json:"notice,omitempty"`
	Hook     *HookReport `json:"hook,omitempty"`
	Warnings []string    `json:"warnings,omitempty"`
}

// Remove takes a workspace down: the before_remove hook first (its failure is
// recorded, never fatal — 方案 §4.8), then the worktree registration (git
// projects) and the directory. A workspace that is already gone is a no-op,
// not an error. The branch is kept: it may hold the run's commits, and
// removing it is a deliberate act, not a cleanup side effect.
func Remove(ctx context.Context, opts RemoveOptions) (RemoveReport, error) {
	rep := RemoveReport{}
	if strings.TrimSpace(opts.ProjectRoot) == "" {
		return rep, errors.New("workspace removal needs a project root")
	}
	root, err := Root(opts.ProjectRoot, opts.Root)
	if err != nil {
		return rep, err
	}
	path := strings.TrimSpace(opts.Path)
	if path == "" {
		if strings.TrimSpace(opts.Identifier) == "" {
			return rep, errors.New("workspace removal needs a path or an identifier")
		}
		path = filepath.Join(root, Key(opts.Identifier))
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return rep, err
	}
	path = filepath.Clean(abs)
	rep.Path = path
	if err := Validate(root, path); err != nil {
		return rep, err
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		rep.Notice = "workspace directory does not exist"
		return rep, nil
	} else if err != nil {
		return rep, err
	}

	if hook := opts.Hooks[HookBeforeRemove]; hook.Command != "" {
		hookRep, herr := RunHook(ctx, RunHookOptions{
			Name: HookBeforeRemove,
			Hook: hook,
			Dir:  path,
			Env:  HookEnv(opts.ProjectRoot, path, "", opts.Identifier, HookBeforeRemove),
		})
		if hookRep.Ran {
			rep.Hook = &hookRep
		}
		if herr != nil {
			rep.Warnings = append(rep.Warnings, herr.Error())
		}
	}
	if GitProject(opts.ProjectRoot) {
		if err := removeWorktree(opts.ProjectRoot, path, opts.Force); err != nil {
			if !opts.Force {
				return rep, fmt.Errorf("%w (use --force to discard local changes)", err)
			}
			return rep, err
		}
		if err := pruneWorktrees(opts.ProjectRoot); err != nil {
			rep.Warnings = append(rep.Warnings, err.Error())
		}
	}
	if _, err := os.Stat(path); err == nil {
		if err := os.RemoveAll(path); err != nil {
			return rep, fmt.Errorf("remove workspace directory %s: %w", path, err)
		}
	}
	rep.Removed = true
	return rep, nil
}

// Listing is one workspace found below the root (List).
type Listing struct {
	Key        string `json:"key"`
	Path       string `json:"path"`
	Branch     string `json:"branch,omitempty"`
	Registered bool   `json:"registered"`
}

// List reports the workspaces below the root, ordered by key. It is read-only:
// nothing is created, repaired or removed.
func List(projectRoot, configuredRoot string) ([]Listing, error) {
	root, err := Root(projectRoot, configuredRoot)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	branches := map[string]string{}
	if GitProject(projectRoot) {
		var err error
		branches, err = worktreeBranches(projectRoot)
		if err != nil {
			return nil, err
		}
	}
	out := make([]Listing, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		item := Listing{Key: entry.Name(), Path: path}
		if branch, ok := branches[path]; ok {
			item.Registered = true
			item.Branch = branch
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// HookEnv is the environment a hook (or an attempt) sees, so a script does not
// have to parse the policy or guess where it is running (方案 §4.8).
func HookEnv(projectRoot, workspacePath, branch, identifier, hookName string) []string {
	env := []string{
		"DEVSYS_PROJECT_ROOT=" + projectRoot,
		"DEVSYS_WORKSPACE=" + workspacePath,
	}
	if branch != "" {
		env = append(env, "DEVSYS_BRANCH="+branch)
	}
	if identifier != "" {
		env = append(env, "DEVSYS_WORKITEM="+identifier)
	}
	if hookName != "" {
		env = append(env, "DEVSYS_HOOK="+hookName)
	}
	return env
}
