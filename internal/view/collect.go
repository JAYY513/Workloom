package view

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"workloom/internal/domain"
	"workloom/internal/storage"
)

// inspectSnapshot runs fn under the project's shared lock. When no lock file
// exists — no writer ever ran, e.g. a read-only mount of a fresh checkout —
// it retries lock-free and reports an advisory snapshot
// (storage.InspectUnlocked). Pending transactions are refused by both paths
// and surface as *storage.PendingTxnError; callers must not read business
// facts then (方案 §15.4).
func inspectSnapshot(ctx context.Context, st *storage.Store, fn func(*storage.Reader) error) (advisory bool, err error) {
	err = st.Inspect(ctx, fn)
	if errors.Is(err, storage.ErrLockUnavailable) {
		return true, st.InspectUnlocked(ctx, fn)
	}
	return false, err
}

// reader reads managed files through a storage.Reader and records what it
// read. The model's provenance is exactly this list: every path a section
// consulted, whether it existed or not, so a reader can retrace the view.
type reader struct {
	r       *storage.Reader
	devsys  string   // absolute path of .devsys
	sources []string // project-relative slash paths, in read order
}

func newReader(st *storage.Store) *reader {
	return &reader{devsys: st.DevsysDir()}
}

// read loads one managed YAML file into v. A missing file is (false, nil):
// a partial layout is a fact to render, not an error.
func (rd *reader) read(rel string, v any) (bool, error) {
	data, exists, err := rd.r.Read(rel)
	if err != nil || !exists {
		return exists, err
	}
	rd.noteDevsys(rel)
	if err := domain.DecodeYAML(data, v); err != nil {
		return true, fmt.Errorf("%s: %w", rel, err)
	}
	return true, nil
}

// list returns the sorted names of the files with the given suffix in one
// managed directory and records the directory. A missing directory lists as
// empty.
func (rd *reader) list(rel, suffix string) ([]string, error) {
	dir := filepath.Join(rd.devsys, filepath.FromSlash(rel))
	items, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", rel, err)
	}
	rd.noteDevsysDir(rel)
	names := make([]string, 0, len(items))
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), suffix) {
			continue
		}
		names = append(names, item.Name())
	}
	sort.Strings(names)
	return names, nil
}

// mark returns the current position in the source list; since then reports
// the sources recorded after it, for one section.
func (rd *reader) mark() int { return len(rd.sources) }

func (rd *reader) since(mark int) []string {
	return sortedUnique(rd.sources[mark:])
}

// exists reports whether one managed file is present, without reading it.
func (rd *reader) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(rd.devsys, filepath.FromSlash(rel)))
	return err == nil
}

// noteDevsys records a path below .devsys as project-relative.
func (rd *reader) noteDevsys(rel string) {
	rd.noteRel(filepath.ToSlash(filepath.Join(storage.DevsysDirName, filepath.FromSlash(rel))))
}

// noteDevsysDir records a listed directory below .devsys; the trailing slash
// marks it as a directory rather than a file.
func (rd *reader) noteDevsysDir(rel string) {
	rd.noteRel(filepath.ToSlash(filepath.Join(storage.DevsysDirName, filepath.FromSlash(rel))) + "/")
}

// noteRel records a project-relative path (a page root, for example).
func (rd *reader) noteRel(rel string) {
	rel = filepath.ToSlash(rel)
	if rel == "" {
		return
	}
	rd.sources = append(rd.sources, rel)
}

// noteRelDir records a project-relative directory, marked by a trailing slash.
func (rd *reader) noteRelDir(rel string) {
	rel = filepath.ToSlash(rel)
	if rel == "" {
		return
	}
	rd.sources = append(rd.sources, rel+"/")
}

// sortedUnique sorts paths and drops duplicates; the model's provenance must
// be byte-stable.
func sortedUnique(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// problem records one located defect for the model's problems list.
func (b *builder) problem(err error) {
	if err == nil {
		return
	}
	b.problems = append(b.problems, err.Error())
}

// problemf records one formatted defect for the model's problems list.
func (b *builder) problemf(format string, args ...any) {
	b.problems = append(b.problems, fmt.Sprintf(format, args...))
}
