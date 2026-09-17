// Package registry manages the user-level project path registry defined in
// 方案 §14.4: it stores only project id, path and last_seen_at — never
// project state. The file lives at <config dir>/devsys/registry.yaml.
//
// Serialization goes through minyaml until the M0.3 storage primitives land.
package registry

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"workloom/internal/minyaml"
)

// EnvDir overrides the config directory; used by tests and portable installs.
const EnvDir = "DEVSYS_CONFIG_DIR"

// Entry is one registered project.
type Entry struct {
	ID         string
	Path       string
	LastSeenAt time.Time
}

// Registry is the parsed registry file.
type Registry struct {
	Projects []Entry
}

// Dir returns the user-level config directory holding devsys user files.
func Dir() (string, error) {
	if v := strings.TrimSpace(os.Getenv(EnvDir)); v != "" {
		return v, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(base, "devsys"), nil
}

// Path returns the registry file path.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "registry.yaml"), nil
}

const header = "# devsys 用户级项目注册表（方案 §14.4）：仅保存项目路径与标识，不含项目状态。\n" +
	"# 由 `devsys init` 自动维护；删除本文件不影响任何项目。\n"

// Load reads the registry; a missing file yields an empty registry.
func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Registry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}
	return parse(path, string(data))
}

func parse(path, data string) (*Registry, error) {
	reg := &Registry{}
	sawProjects := false
	var cur *Entry
	flush := func() {
		if cur != nil {
			reg.Projects = append(reg.Projects, *cur)
			cur = nil
		}
	}
	for n, raw := range strings.Split(data, "\n") {
		lineNo := n + 1
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !sawProjects {
			if trimmed != "projects:" && trimmed != "projects: []" {
				return nil, fmt.Errorf("%s:%d: expected top-level \"projects:\"", path, lineNo)
			}
			sawProjects = true
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			flush()
			key, val, ok := splitKey(strings.TrimPrefix(trimmed, "- "))
			if !ok || key != "id" {
				return nil, fmt.Errorf("%s:%d: expected \"- id:\" to start a project entry", path, lineNo)
			}
			id, err := minyaml.UnquoteScalar(val)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
			}
			cur = &Entry{ID: id}
			continue
		}
		if cur == nil {
			return nil, fmt.Errorf("%s:%d: entry field outside of a project entry", path, lineNo)
		}
		key, val, ok := splitKey(trimmed)
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected \"key: value\"", path, lineNo)
		}
		switch key {
		case "path":
			v, err := minyaml.UnquoteScalar(val)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
			}
			cur.Path = v
		case "last_seen_at":
			v, err := minyaml.UnquoteScalar(val)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
			}
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid last_seen_at %q", path, lineNo, v)
			}
			cur.LastSeenAt = t
		default:
			return nil, fmt.Errorf("%s:%d: unknown key %q", path, lineNo, key)
		}
	}
	flush()
	if !sawProjects {
		return nil, fmt.Errorf("%s: missing \"projects:\" key", path)
	}
	for i, e := range reg.Projects {
		if e.ID == "" || e.Path == "" || e.LastSeenAt.IsZero() {
			return nil, fmt.Errorf("%s: project entry %d is missing id, path or last_seen_at", path, i+1)
		}
	}
	return reg, nil
}

func splitKey(s string) (key, val string, ok bool) {
	idx := strings.Index(s, ":")
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+1:]), true
}

// Upsert inserts e or replaces the entry with the same path (case-insensitive
// on Windows). It reports whether an existing entry was replaced.
func (r *Registry) Upsert(e Entry) bool {
	for i := range r.Projects {
		if samePath(r.Projects[i].Path, e.Path) {
			r.Projects[i] = e
			return true
		}
	}
	r.Projects = append(r.Projects, e)
	return false
}

// Save writes the registry deterministically (entries sorted by path). The M0.3
// storage primitives replace this with the real atomic writer.
func (r *Registry) Save(path string) error {
	projects := append([]Entry(nil), r.Projects...)
	sort.SliceStable(projects, func(i, j int) bool { return normKey(projects[i].Path) < normKey(projects[j].Path) })

	var b strings.Builder
	b.WriteString(header)
	if len(projects) == 0 {
		b.WriteString("projects: []\n")
	} else {
		b.WriteString("projects:\n")
		for _, e := range projects {
			id, err := minyaml.QuoteScalar(e.ID)
			if err != nil {
				return err
			}
			p, err := minyaml.QuoteScalar(e.Path)
			if err != nil {
				return err
			}
			fmt.Fprintf(&b, "  - id: %s\n    path: %s\n    last_seen_at: %s\n",
				id, p, e.LastSeenAt.UTC().Format(time.RFC3339))
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create registry directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write registry: %w", err)
	}
	return nil
}

func samePath(a, b string) bool {
	a, b = normKey(a), normKey(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func normKey(p string) string { return filepath.ToSlash(filepath.Clean(p)) }
