package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workloom/internal/storage"
)

const (
	validProject = `# comment
schema_version: 1
id: demo
name: 示例项目
created_at: 2026-09-17T09:00:00Z
`
	validConfig     = "schema_version: 1\n"
	validCurrent    = "schema_version: 1\nsummary: \"\"\nrisks: []\nblockers: []\nnext_focus: []\n"
	validMilestones = "schema_version: 1\nmilestones: []\n"
)

func writeManaged(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, storage.DevsysDirName, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func validFiles() map[string]string {
	return map[string]string{
		ProjectFile:      validProject,
		ConfigFile:       validConfig,
		CurrentStateFile: validCurrent,
		MilestonesFile:   validMilestones,
	}
}

func render(ps Problems) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.String()
	}
	return out
}

func assertProblems(t *testing.T, got Problems, want []string) {
	t.Helper()
	rendered := render(got)
	if len(rendered) != len(want) {
		t.Fatalf("problems = %#v, want %#v", rendered, want)
	}
	for i := range want {
		if rendered[i] != want[i] {
			t.Errorf("problem %d = %q, want %q", i, rendered[i], want[i])
		}
	}
}

func TestLoadValidMetadata(t *testing.T) {
	root := writeManaged(t, validFiles())
	md, problems := Load(root)
	assertProblems(t, problems, nil)
	if md.Project == nil || md.Config == nil {
		t.Fatalf("metadata = %+v", md)
	}
	if md.Project.SchemaVersion != 1 || md.Project.ID != "demo" || md.Project.Name != "示例项目" {
		t.Errorf("project = %+v", md.Project)
	}
	if md.Project.CreatedAt.Format(time.RFC3339) != "2026-09-17T09:00:00Z" {
		t.Errorf("created_at = %q", md.Project.CreatedAt)
	}
	if md.Config.SchemaVersion != 1 {
		t.Errorf("config = %+v", md.Config)
	}
}

func TestLoadOptionalCreatedAtMayBeAbsent(t *testing.T) {
	files := validFiles()
	files[ProjectFile] = "schema_version: 1\nid: demo\nname: demo\n"
	md, problems := Load(writeManaged(t, files))
	assertProblems(t, problems, nil)
	if md.Project == nil || !md.Project.CreatedAt.IsZero() {
		t.Errorf("project = %+v", md.Project)
	}
}

func TestValidateProblems(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{
			name:  "type error",
			files: map[string]string{ProjectFile: "# c\nschema_version: 1\nid: 42\nname: demo\n"},
			want:  []string{"project.yaml:3: id: expected string, got !!int"},
		},
		{
			name:  "missing required",
			files: map[string]string{ProjectFile: "# c\nschema_version: 1\nid: demo\n"},
			want:  []string{"project.yaml:2: name: required"},
		},
		{
			name:  "empty required",
			files: map[string]string{ProjectFile: "# c\nschema_version: 1\nid: \"\"\nname: demo\n"},
			want:  []string{"project.yaml:3: id: required, but empty"},
		},
		{
			name:  "unsupported version reports only the version problem",
			files: map[string]string{ProjectFile: "# c\nschema_version: 99\nid: demo\nname: demo\nbogus: 1\n"},
			want:  []string{"project.yaml:2: schema_version: unsupported version 99 (this build supports 1); refusing to write, migration must be explicit (实施计划 M9.2)"},
		},
		{
			name:  "missing version",
			files: map[string]string{ProjectFile: "# c\nid: demo\nname: demo\n"},
			want:  []string{"project.yaml:2: schema_version: missing"},
		},
		{
			name:  "quoted version is a type error",
			files: map[string]string{ProjectFile: "# c\nschema_version: \"1\"\nid: demo\nname: demo\n"},
			want:  []string{"project.yaml:2: schema_version: expected integer, got !!str"},
		},
		{
			name:  "duplicate key",
			files: map[string]string{ProjectFile: "# c\nschema_version: 1\nid: demo\nname: demo\nname: other\n"},
			want:  []string{"project.yaml:5: name: duplicate key"},
		},
		{
			name:  "bad timestamp",
			files: map[string]string{ProjectFile: "# c\nschema_version: 1\nid: demo\nname: demo\ncreated_at: yesterday\n"},
			want:  []string{`project.yaml:5: created_at: expected RFC3339 timestamp, got "yesterday"`},
		},
		{
			name:  "top level sequence",
			files: map[string]string{ProjectFile: "- just\n- a list\n"},
			want:  []string{"project.yaml:1: expected a mapping at the top level, got !!seq"},
		},
		{
			name:  "empty file",
			files: map[string]string{ProjectFile: ""},
			want:  []string{"project.yaml: empty file; expected a mapping with schema_version"},
		},
		{
			name:  "second document",
			files: map[string]string{ProjectFile: "schema_version: 1\nid: demo\nname: demo\n---\nschema_version: 1\n"},
			want:  []string{"project.yaml:4: unexpected second document"},
		},
		{
			name:  "non-scalar key",
			files: map[string]string{ProjectFile: "schema_version: 1\nid: demo\nname: demo\n[weird]: 1\n"},
			want:  []string{"project.yaml:4: non-scalar key (!!seq)"},
		},
		{
			name:  "config unknown key",
			files: map[string]string{ConfigFile: "schema_version: 1\ntimeout: 5\n"},
			want:  []string{"config.yaml:2: timeout: unknown key"},
		},
		{
			name:  "state file missing version",
			files: map[string]string{CurrentStateFile: "summary: hi\n"},
			want:  []string{"state/current.yaml:1: schema_version: missing"},
		},
		{
			name:  "state file with unsupported version",
			files: map[string]string{MilestonesFile: "schema_version: 2\nmilestones: []\n"},
			want:  []string{"state/milestones.yaml:1: schema_version: unsupported version 2 (this build supports 1); refusing to write, migration must be explicit (实施计划 M9.2)"},
		},
		{
			name:  "state file rejects unknown keys",
			files: map[string]string{CurrentStateFile: "schema_version: 1\nfuture_key: []\n"},
			want:  []string{"state/current.yaml:2: future_key: unknown key"},
		},
		{
			name:  "all problems are collected",
			files: map[string]string{ProjectFile: "# c\nschema_version: 1\nid: 42\ncreated_at: yesterday\n"},
			want: []string{
				"project.yaml:3: id: expected string, got !!int",
				"project.yaml:4: created_at: expected RFC3339 timestamp, got \"yesterday\"",
				"project.yaml:2: name: required",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := validFiles()
			for rel, content := range tc.files {
				files[rel] = content
			}
			md, problems := Load(writeManaged(t, files))
			assertProblems(t, problems, tc.want)
			if len(tc.want) > 0 && strings.HasPrefix(tc.want[0], ProjectFile) && md.Project != nil {
				// A file with any problem must not yield a usable value.
				t.Errorf("project must be nil on problems, got %+v", md.Project)
			}
		})
	}
}

func TestDiagnoseReportsMissingFiles(t *testing.T) {
	root := writeManaged(t, map[string]string{ProjectFile: validProject, ConfigFile: validConfig})

	md, problems := Load(root)
	assertProblems(t, problems, nil)
	if md.Project == nil || md.Config == nil {
		t.Fatalf("metadata = %+v", md)
	}

	_, problems = Diagnose(root)
	assertProblems(t, problems, []string{
		"state/current.yaml: missing; run `devsys init` to create it",
		"state/milestones.yaml: missing; run `devsys init` to create it",
	})
}

func TestProblemsError(t *testing.T) {
	single := Problems{{File: "project.yaml", Line: 3, Field: "id", Reason: "expected string, got !!int"}}
	if got := single.Error(); got != "project.yaml:3: id: expected string, got !!int" {
		t.Errorf("Error() = %q", got)
	}
	multi := Problems{
		{File: "config.yaml", Reason: "empty file; expected a mapping with schema_version"},
		{File: "state/current.yaml", Field: "schema_version", Reason: "missing"},
	}
	want := "config.yaml: empty file; expected a mapping with schema_version; state/current.yaml: schema_version: missing"
	if got := multi.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
