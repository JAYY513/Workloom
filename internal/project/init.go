// Package project implements project-local devsys operations. M0.2 ships
// `devsys init` only; later milestones add the rest of the command family.
package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JAYY513/Workloom/internal/config"
	"github.com/JAYY513/Workloom/internal/storage"
)

// PreconditionError marks failures the user can fix in the environment
// (no git repository, wrong directory, missing git). The CLI maps it to its
// dedicated exit code so scripts can branch without parsing messages.
type PreconditionError struct{ Msg string }

func (e *PreconditionError) Error() string { return e.Msg }

// Options tunes Init; zero values are valid.
type Options struct {
	// Now is the clock used for timestamps. Zero means time.Now().
	Now time.Time
}

// Result reports what Init created.
type Result struct {
	Root    string   // absolute repository root
	ID      string   // project identifier (Slug of the root directory name)
	Name    string   // root directory name
	Created []string // paths created by this run, relative to Root, slash-separated, sorted
}

// Init creates the .devsys/ layout (方案 §14.3) inside the git repository root
// dir and returns what it created. It is idempotent: existing files and
// directories are never modified or overwritten; only missing entries are
// added. Precondition failures happen before any filesystem change.
func Init(dir string, opts Options) (*Result, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root, err := gitRoot(abs)
	if err != nil {
		return nil, err
	}
	if err := requireSameDir(abs, root); err != nil {
		return nil, err
	}

	// A write command must not touch a project whose managed files are already
	// invalid (方案 §14.1: 读到未知版本时拒绝写入). Missing files are not problems
	// here — creating them is exactly init's job. This runs before anything on
	// disk changes.
	if _, problems := config.Load(root); len(problems) > 0 {
		return nil, problems
	}

	name := filepath.Base(root)
	id := Slug(name)
	devsys := filepath.Join(root, DevsysDirName)
	res := &Result{Root: root, ID: id, Name: name}

	if _, err := os.Stat(devsys); errors.Is(err, fs.ErrNotExist) {
		res.Created = append(res.Created, DevsysDirName)
	} else if err != nil {
		return nil, err
	}

	for _, d := range layoutDirs {
		p := filepath.Join(devsys, d)
		if _, err := os.Stat(p); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if err := os.MkdirAll(p, 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", p, err)
		}
		res.Created = append(res.Created, DevsysDirName+"/"+d)
	}

	for _, ph := range placeholders() {
		p := filepath.Join(devsys, filepath.FromSlash(ph.rel))
		if _, err := os.Stat(p); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		content, err := ph.build(id, name, now)
		if err != nil {
			return nil, err
		}
		if err := storage.AtomicWrite(p, content, 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", p, err)
		}
		res.Created = append(res.Created, DevsysDirName+"/"+ph.rel)
	}

	gitignore := filepath.Join(devsys, ".gitignore")
	_, statErr := os.Stat(gitignore)
	newIgnore := errors.Is(statErr, fs.ErrNotExist)
	if err := ensureIgnoreEntries(gitignore, ignoreEntries); err != nil {
		return nil, err
	}
	if newIgnore {
		res.Created = append(res.Created, DevsysDirName+"/.gitignore")
	}

	// Root .gitattributes: devsys writes LF files (.devsys/ state and the
	// .agents skill); without these rules Windows Git warns about CRLF
	// conversion on every status/diff (#345 N2). Missing entries are
	// appended to an existing file — hand-written rules are never touched.
	attrs := filepath.Join(dir, ".gitattributes")
	_, attrsErr := os.Stat(attrs)
	newAttrs := errors.Is(attrsErr, fs.ErrNotExist)
	if err := ensureAttrsEntries(attrs, attrsEntries); err != nil {
		return nil, err
	}
	if newAttrs {
		res.Created = append(res.Created, ".gitattributes")
	}

	sort.Strings(res.Created)
	return res, nil
}

// gitRoot resolves the repository root containing dir, or returns a
// PreconditionError explaining how to fix the environment.
func gitRoot(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", &PreconditionError{Msg: "git executable not found in PATH: install git first"}
		}
		return "", &PreconditionError{Msg: fmt.Sprintf(
			"%s is not inside a git repository: run `git init` in the project root first", dir)}
	}
	root := filepath.FromSlash(strings.TrimSpace(string(out)))
	if root == "" {
		return "", &PreconditionError{Msg: fmt.Sprintf("git returned an empty repository root for %s", dir)}
	}
	return filepath.Clean(root), nil
}

// requireSameDir insists that the working directory is the repository root, so
// .devsys/ always lands at the top of the project (方案 §14.3).
func requireSameDir(dir, root string) error {
	a, err := os.Stat(dir)
	if err != nil {
		return err
	}
	b, err := os.Stat(root)
	if err != nil {
		return &PreconditionError{Msg: fmt.Sprintf("cannot stat repository root %s: %v", root, err)}
	}
	if !os.SameFile(a, b) {
		return &PreconditionError{Msg: fmt.Sprintf("run `devsys init` from the repository root (%s)", root)}
	}
	return nil
}

// ensureIgnoreEntries creates path with the given entries when missing, or
// appends only the missing entries to an existing file. Existing lines are
// never rewritten.
func ensureIgnoreEntries(path string, entries []string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		content := "# devsys 本地文件（方案 §14.2）：不提交，可删除。\n" + strings.Join(entries, "\n") + "\n"
		return storage.AtomicWrite(path, []byte(content), 0o644)
	}
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(data), "\n") {
		have[strings.TrimSpace(strings.TrimRight(l, "\r"))] = true
	}
	var missing []string
	for _, e := range entries {
		if have[e] || have[strings.TrimSuffix(e, "/")] {
			continue
		}
		missing = append(missing, e)
	}
	if len(missing) == 0 {
		return nil
	}
	out := string(data)
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += "# devsys 本地文件（方案 §14.2）：不提交，可删除。\n" + strings.Join(missing, "\n") + "\n"
	return storage.AtomicWrite(path, []byte(out), 0o644)
}

// attrsEntries pins devsys-written trees to LF so Windows Git stops
// warning about CRLF conversion on every status/diff (#345 N2).
var attrsEntries = []string{".devsys/** text eol=lf", ".agents/** text eol=lf"}

// ensureAttrsEntries mirrors ensureIgnoreEntries for the repository-root
// .gitattributes.
func ensureAttrsEntries(path string, entries []string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		content := "# devsys 写的文件保持 LF：消除 Windows 换行噪音。\n" + strings.Join(entries, "\n") + "\n"
		return storage.AtomicWrite(path, []byte(content), 0o644)
	}
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(data), "\n") {
		have[strings.TrimSpace(strings.TrimRight(l, "\r"))] = true
	}
	var missing []string
	for _, e := range entries {
		if have[e] {
			continue
		}
		missing = append(missing, e)
	}
	if len(missing) == 0 {
		return nil
	}
	out := string(data)
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += "# devsys 写的文件保持 LF：消除 Windows 换行噪音。\n" + strings.Join(missing, "\n") + "\n"
	return storage.AtomicWrite(path, []byte(out), 0o644)
}
