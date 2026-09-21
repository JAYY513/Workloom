package project

import (
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/storage"
)

// DevsysDirName is the per-project state directory (方案 §14.3).
const DevsysDirName = storage.DevsysDirName

// layoutDirs lists the .devsys subdirectories required by 方案 §14.3.
var layoutDirs = []string{
	"state",
	"workitems",
	"specs",
	"workflows",
	"runs",
	"decisions",
	"findings",
	"approvals",
	"events",
	"artifacts",
	"context",
	"knowledge",
	".cache", // local-only; excluded via .devsys/.gitignore
	"local",  // local-only; excluded via .devsys/.gitignore
}

// ignoreEntries keep local-only state out of git (方案 §14.2). 方案 §14.3 does
// not list the .gitignore file itself; the deviation is recorded in the M0.2
// spec and in the task artifacts.
var ignoreEntries = []string{"local/", ".cache/", "workspaces/"}

// placeholder describes one minimal file devsys init writes when missing.
type placeholder struct {
	rel   string // path inside .devsys, slash-separated
	build func(id, name string, now time.Time) ([]byte, error)
}

func placeholders() []placeholder {
	return []placeholder{
		{rel: "project.yaml", build: projectYAML},
		{rel: "config.yaml", build: configYAML},
		{rel: "state/current.yaml", build: currentStateYAML},
		{rel: "state/milestones.yaml", build: milestonesYAML},
	}
}

// The placeholder shapes match the domain contracts internal/config enforces:
// unknown keys in these files are rejected, so the two must move together.
// They are written through domain.EncodeYAML, so key order is the field order
// of the domain structs and repeated runs produce identical bytes.

type configFile struct {
	SchemaVersion int `yaml:"schema_version"`
}

func projectYAML(id, name string, now time.Time) ([]byte, error) {
	return withHeader("# devsys 项目元数据（方案 §5.1）；由 devsys 命令维护。\n", domain.Project{
		SchemaVersion: domain.SchemaVersion,
		ID:            id, Name: name, Status: "active",
		CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	})
}

func configYAML(string, string, time.Time) ([]byte, error) {
	return withHeader("# devsys 项目配置（方案 §14.2/§14.3）；可选项见 `devsys config check` 校验的白名单：\n# workspace_root、dispatch_command、knowledge_pages、knowledge_generator、default_policy。\n", configFile{SchemaVersion: 1})
}

func currentStateYAML(string, string, time.Time) ([]byte, error) {
	return withHeader("# 项目当前状态（方案 §5.1 current_state）；由 devsys 命令维护。\n", domain.CurrentStateFile{
		SchemaVersion: domain.SchemaVersion,
		CurrentState:  domain.CurrentState{Risks: []string{}, Blockers: []string{}, NextFocus: []string{}},
	})
}

func milestonesYAML(string, string, time.Time) ([]byte, error) {
	return withHeader("# 里程碑（方案 §5.1）；由 devsys 命令维护。\n", domain.MilestonesFile{
		SchemaVersion: domain.SchemaVersion, Milestones: []domain.Milestone{},
	})
}

// withHeader prepends a comment line to an encoded document so hand editors
// keep seeing where the file comes from.
func withHeader(comment string, v any) ([]byte, error) {
	body, err := domain.EncodeYAML(v)
	if err != nil {
		return nil, err
	}
	return append([]byte(comment), body...), nil
}
