// Package registry manages the user-level project path registry defined in
// 方案 §14.4: it stores only project id, path and last_seen_at — never project
// state. The file lives at <config dir>/devsys/registry.yaml and is written
// through the storage primitives (stable serialization + atomic replacement).
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

	"github.com/JAYY513/Workloom/internal/storage"
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

const header = "# workloom 用户级项目注册表（方案 §14.4）：仅保存项目路径与标识，不含项目状态。\n" +
	"# 由 `workloom init` 自动维护；删除本文件不影响任何项目。\n"

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

// registryFile and entryFile mirror the on-disk shape: field order is the key
// order (stable serialization), timestamps are RFC 3339 UTC strings.
type registryFile struct {
	Projects []entryFile `yaml:"projects"`
}

type entryFile struct {
	ID         string `yaml:"id"`
	Path       string `yaml:"path"`
	LastSeenAt string `yaml:"last_seen_at"`
}

func parse(path, data string) (*Registry, error) {
	var raw struct {
		Projects *[]entryFile `yaml:"projects"`
	}
	if err := storage.DecodeYAML([]byte(data), &raw); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	if raw.Projects == nil {
		return nil, fmt.Errorf("%s: missing \"projects:\" key", path)
	}
	reg := &Registry{}
	for i, e := range *raw.Projects {
		if e.ID == "" || e.Path == "" || e.LastSeenAt == "" {
			return nil, fmt.Errorf("%s: project entry %d is missing id, path or last_seen_at", path, i+1)
		}
		t, err := time.Parse(time.RFC3339, e.LastSeenAt)
		if err != nil {
			return nil, fmt.Errorf("%s: project entry %d: invalid last_seen_at %q", path, i+1, e.LastSeenAt)
		}
		reg.Projects = append(reg.Projects, Entry{ID: e.ID, Path: e.Path, LastSeenAt: t})
	}
	return reg, nil
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

// Save writes the registry deterministically (entries sorted by path, atomic
// replacement so readers never see a torn file).
func (r *Registry) Save(path string) error {
	projects := append([]Entry(nil), r.Projects...)
	sort.SliceStable(projects, func(i, j int) bool { return normKey(projects[i].Path) < normKey(projects[j].Path) })

	file := registryFile{}
	for _, e := range projects {
		file.Projects = append(file.Projects, entryFile{
			ID:         e.ID,
			Path:       e.Path,
			LastSeenAt: e.LastSeenAt.UTC().Format(time.RFC3339),
		})
	}
	body, err := storage.EncodeYAML(file)
	if err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create registry directory: %w", err)
	}
	if err := storage.AtomicWrite(path, append([]byte(header), body...), 0o644); err != nil {
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
