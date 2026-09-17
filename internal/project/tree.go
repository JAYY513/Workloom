package project

import (
	"fmt"
	"time"

	"workloom/internal/minyaml"
)

// DevsysDirName is the per-project state directory (方案 §14.3).
const DevsysDirName = ".devsys"

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
var ignoreEntries = []string{"local/", ".cache/"}

// placeholder describes one minimal file devsys init writes when missing.
type placeholder struct {
	rel   string // path inside .devsys, slash-separated
	build func(id, name string, now time.Time) (string, error)
}

func placeholders() []placeholder {
	return []placeholder{
		{rel: "project.yaml", build: projectYAML},
		{rel: "config.yaml", build: configYAML},
		{rel: "state/current.yaml", build: currentStateYAML},
		{rel: "state/milestones.yaml", build: milestonesYAML},
	}
}

func projectYAML(id, name string, now time.Time) (string, error) {
	idQ, err := minyaml.QuoteScalar(id)
	if err != nil {
		return "", err
	}
	nameQ, err := minyaml.QuoteScalar(name)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("# devsys 项目元数据（方案 §5.1）；字段自 M0.4 起严格校验，可手工维护。\n"+
		"schema_version: 1\n"+
		"id: %s\n"+
		"name: %s\n"+
		"created_at: %s\n", idQ, nameQ, now.UTC().Format(time.RFC3339)), nil
}

func configYAML(string, string, time.Time) (string, error) {
	return "# devsys 项目配置（方案 §14.2/§14.3）；键位自 M0.4 起严格校验。\n" +
		"schema_version: 1\n", nil
}

func currentStateYAML(string, string, time.Time) (string, error) {
	return "# 项目当前状态（方案 §5.1 current_state）；由 devsys 命令维护。\n" +
		"schema_version: 1\n" +
		"summary: ''\n" +
		"risks: []\n" +
		"blockers: []\n" +
		"next_focus: []\n", nil
}

func milestonesYAML(string, string, time.Time) (string, error) {
	return "# 里程碑（方案 §5.1）；由 devsys 命令维护。\n" +
		"schema_version: 1\n" +
		"milestones: []\n", nil
}
