package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrGitMissing reports that the git executable is not on PATH; the CLI maps
// it to its precondition exit code (D6: a project is always a git repository).
var ErrGitMissing = errors.New("git is not available on PATH")

// GitError is a failed git invocation, carrying what git printed.
type GitError struct {
	Args   []string
	Output string
	Err    error
}

func (e *GitError) Error() string {
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), e.Output)
}

func (e *GitError) Unwrap() error { return e.Err }

// requireGit checks the executable before any worktree operation, so a
// missing git is reported as such instead of as a failed command.
func requireGit() error {
	if _, err := exec.LookPath("git"); err != nil {
		return ErrGitMissing
	}
	return nil
}

// gitOutput runs one git command in dir and returns its trimmed combined
// output. Hooks and worktree commands share the process-wide environment;
// nothing here is interactive.
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Run(); err != nil {
		out := strings.TrimSpace(buf.String())
		if out == "" {
			out = err.Error()
		}
		return "", &GitError{Args: args, Output: out, Err: err}
	}
	return strings.TrimSpace(buf.String()), nil
}

// commonDir resolves the repository a directory belongs to. Inside a linked
// worktree this is the main repository's .git directory, which is what makes
// two checkouts comparable.
func commonDir(dir string) (string, error) {
	out, err := gitOutput(dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(dir, out)
	}
	return filepath.Clean(out), nil
}

// sameRepo reports whether dir is a checkout of the repository at projectRoot.
func sameRepo(dir, projectRoot string) (bool, error) {
	a, err := commonDir(dir)
	if err != nil {
		return false, err
	}
	b, err := commonDir(projectRoot)
	if err != nil {
		return false, err
	}
	ra, err := resolveExisting(a)
	if err != nil {
		return false, err
	}
	rb, err := resolveExisting(b)
	if err != nil {
		return false, err
	}
	return samePath(ra, rb), nil
}

// branchExists reports whether refs/heads/<branch> is present.
func branchExists(root, branch string) (bool, error) {
	if _, err := gitOutput(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		var ge *GitError
		if errors.As(err, &ge) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func headSHA(dir string) (string, error) { return gitOutput(dir, "rev-parse", "HEAD") }

// HeadSHA is the commit a checkout currently points at (the completion check
// compares the claim head with it, 方案 §4.8).
func HeadSHA(dir string) (string, error) { return headSHA(dir) }

// BranchSHA is the commit a branch points at in the project repository.
func BranchSHA(root, branch string) (string, error) { return branchSHA(root, branch) }

func branchSHA(root, branch string) (string, error) {
	return gitOutput(root, "rev-parse", "refs/heads/"+branch)
}

// currentBranch reads the branch a checkout is on; a detached HEAD is an
// error because a run must not work on one.
func currentBranch(dir string) (string, error) {
	out, err := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	if out == "HEAD" {
		return "", fmt.Errorf("checkout %s has a detached HEAD", dir)
	}
	return out, nil
}

// addWorktree checks branch out at path, creating the branch from the
// project's current HEAD when it does not exist yet.
func addWorktree(root, path, branch string, createBranch bool) error {
	args := []string{"worktree", "add"}
	if createBranch {
		args = append(args, "-b", branch)
	}
	args = append(args, "--", path)
	if !createBranch {
		args = append(args, branch)
	}
	_, err := gitOutput(root, args...)
	return err
}

func removeWorktree(root, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, "--", path)
	_, err := gitOutput(root, args...)
	return err
}

func pruneWorktrees(root string) error {
	_, err := gitOutput(root, "worktree", "prune")
	return err
}

func deleteBranch(root, branch string) error {
	_, err := gitOutput(root, "branch", "-D", branch)
	return err
}

// worktreeBranches maps checkout paths to the branch they hold, straight from
// git (the authoritative registry, not the directory listing).
func worktreeBranches(root string) (map[string]string, error) {
	out, err := gitOutput(root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	branches := map[string]string{}
	path := ""
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = filepath.Clean(strings.TrimPrefix(line, "worktree "))
		case strings.HasPrefix(line, "branch ") && path != "":
			branches[path] = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case strings.HasPrefix(line, "detached") && path != "":
			branches[path] = ""
		}
	}
	return branches, nil
}
