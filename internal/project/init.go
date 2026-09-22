// Package project implements project-local devsys operations. M0.2 ships
// `workloom init` only; later milestones add the rest of the command family.
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
// (nested .devsys/, missing directory). The CLI maps it to its dedicated
// exit code so scripts can branch without parsing messages.
type PreconditionError struct{ Msg string }

func (e *PreconditionError) Error() string { return e.Msg }

// Options tunes Init; zero values are valid.
type Options struct {
	// Now is the clock used for timestamps. Zero means time.Now().
	Now time.Time
}

// Result reports what Init created.
type Result struct {
	Root    string   // absolute project directory
	ID      string   // project identifier (Slug of the root directory name)
	Name    string   // root directory name
	Created []string // paths created by this run, relative to Root, slash-separated, sorted
}

// Init creates the .devsys/ layout (方案 §14.3) in dir — the operator's
// directory after Abs — and returns what it created. Git is optional: the
// project identity is the directory that holds .devsys/, not a git toplevel.
// It is idempotent: existing files and directories are never modified or
// overwritten; only missing entries are added. Precondition failures happen
// before any filesystem change. Init never runs `git init`.
func Init(dir string, opts Options) (*Result, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &PreconditionError{Msg: fmt.Sprintf("%s is not a directory", abs)}
	}
	if parent, ok := nestedUnderDevsys(abs); ok {
		return nil, &PreconditionError{Msg: fmt.Sprintf(
			".devsys/ already exists at %s; refusing to nest another project", parent)}
	}
	root := abs

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

	// Root .gitattributes only when this directory itself is a git toplevel.
	// A subdirectory init (monorepo package) must not rewrite the parent
	// repo; a non-git directory has nothing to attribute.
	if isGitToplevel(root) {
		attrs := filepath.Join(root, ".gitattributes")
		_, attrsErr := os.Stat(attrs)
		newAttrs := errors.Is(attrsErr, fs.ErrNotExist)
		if err := ensureAttrsEntries(attrs, attrsEntries); err != nil {
			return nil, err
		}
		if newAttrs {
			res.Created = append(res.Created, ".gitattributes")
		}
	}

	sort.Strings(res.Created)
	return res, nil
}

// nestedUnderDevsys reports the nearest ancestor of dir that already holds
// .devsys/, so Init can refuse to nest a project. dir itself is not checked:
// re-running init in an existing project is idempotent.
func nestedUnderDevsys(dir string) (string, bool) {
	parent := filepath.Dir(dir)
	for parent != dir {
		info, err := os.Stat(filepath.Join(parent, DevsysDirName))
		if err == nil && info.IsDir() {
			return parent, true
		}
		next := filepath.Dir(parent)
		if next == parent {
			break
		}
		parent = next
	}
	return "", false
}

// isGitToplevel reports whether dir itself is a git repository root. Missing
// git, a non-repository, or a subdirectory of a repository are all false —
// Init must not write .gitattributes into a parent repo.
func isGitToplevel(dir string) bool {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return false
	}
	top := filepath.Clean(filepath.FromSlash(strings.TrimSpace(string(out))))
	if top == "" {
		return false
	}
	a, err := os.Stat(dir)
	if err != nil {
		return false
	}
	b, err := os.Stat(top)
	if err != nil {
		return false
	}
	return os.SameFile(a, b)
}

// ensureIgnoreEntries creates path with the given entries when missing, or
// appends only the missing entries to an existing file. Existing lines are
// never rewritten.
func ensureIgnoreEntries(path string, entries []string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		content := "# workloom 本地文件（方案 §14.2）：不提交，可删除。\n" + strings.Join(entries, "\n") + "\n"
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
	out += "# workloom 本地文件（方案 §14.2）：不提交，可删除。\n" + strings.Join(missing, "\n") + "\n"
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
		content := "# workloom 写的文件保持 LF：消除 Windows 换行噪音。\n" + strings.Join(entries, "\n") + "\n"
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
	out += "# workloom 写的文件保持 LF：消除 Windows 换行噪音。\n" + strings.Join(missing, "\n") + "\n"
	return storage.AtomicWrite(path, []byte(out), 0o644)
}
