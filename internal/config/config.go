// Package config validates the managed metadata files devsys owns inside a
// project's .devsys/ directory (方案 §14.1/§14.2/§14.3, 实施计划 M0.4).
//
// Validation is strict: unknown keys, wrong types and missing required fields
// are reported with their file, line and field, and every problem is collected
// before the command fails. A file whose schema_version this build does not
// understand is reported and otherwise left uninterpreted — reading a version
// we do not know must never tempt a caller into writing (方案 §14.1: 读到未知版本
// 时拒绝写入、仅允许只读诊断).
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/storage"
)

// SupportedSchemaVersion is the only schema_version this build understands.
const SupportedSchemaVersion = 1

// Managed metadata files, paths relative to .devsys/, slash-separated.
const (
	ProjectFile      = "project.yaml"
	ConfigFile       = "config.yaml"
	CurrentStateFile = "state/current.yaml"
	MilestonesFile   = "state/milestones.yaml"
)

// managedFiles lists the files Load and Diagnose check, in report order.
func managedFiles() []string {
	return []string{ProjectFile, ConfigFile, CurrentStateFile, MilestonesFile}
}

// ManagedFiles returns the managed metadata files in report order.
func ManagedFiles() []string { return managedFiles() }

// SeverityWarning marks a problem as advisory: it is recorded and rendered,
// but it is not a reason to refuse the file. An empty Severity is an error.
const SeverityWarning = "warning"

// Problem is one located defect in a managed file. Line is 1-based and zero
// when the file as a whole is at fault; Field is empty when the defect does
// not belong to a single field. Severity is SeverityWarning for advisories and
// empty for errors.
type Problem struct {
	File     string `json:"file"`
	Line     int    `json:"line,omitempty"`
	Field    string `json:"field,omitempty"`
	Severity string `json:"severity,omitempty"`
	Reason   string `json:"reason"`
}

// String renders the location as `[warning: ]file[:line][: field]: reason`.
func (p Problem) String() string {
	var b strings.Builder
	if p.Severity != "" {
		b.WriteString(p.Severity)
		b.WriteString(": ")
	}
	b.WriteString(p.File)
	if p.Line > 0 {
		fmt.Fprintf(&b, ":%d", p.Line)
	}
	if p.Field != "" {
		b.WriteString(": ")
		b.WriteString(p.Field)
	}
	b.WriteString(": ")
	b.WriteString(p.Reason)
	return b.String()
}

// Problems is a non-empty list of located defects. It implements error so a
// write path can refuse with it unchanged.
type Problems []Problem

func (ps Problems) Error() string {
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = p.String()
	}
	return strings.Join(parts, "; ")
}

// Config is the strict view of config.yaml. No project-level options are
// defined yet, so only schema_version is accepted; later milestones extend
// both the whitelist and this struct.
type Config struct {
	SchemaVersion int `json:"schema_version"`
	// WorkspaceRoot is where execution workspaces are created (方案 §4.8):
	// absolute, or relative to the project root. Empty selects the default
	// below .devsys/.
	WorkspaceRoot string `json:"workspace_root,omitempty"`
	// DispatchCommand is the command a scheduling tick runs for each attempt
	// it dispatches (M6.4). Empty means the tick refuses to start attempts.
	DispatchCommand string `json:"dispatch_command,omitempty"`
	// KnowledgePages are the page-layer roots (project-relative directories)
	// the knowledge commands read and validate (M5.1, 方案 §12.6). Empty means
	// the built-in candidates of knowledge.DefaultRoots.
	KnowledgePages []string `json:"knowledge_pages,omitempty"`
	// KnowledgeGenerator is the page generator command a refresh runs
	// (M5.3, 方案 §12.6). Empty means no page generator is installed, which the
	// layer reports as the documented degradation rather than as a failure.
	KnowledgeGenerator string `json:"knowledge_generator,omitempty"`
	// DefaultPolicy is the workflow policy id governing work items that
	// declare no instance of their own (方案 §4.7/§5.3). Empty keeps them
	// ungated: gates then apply only where a work item bound a policy.
	DefaultPolicy string `json:"default_policy,omitempty"`
}

// Metadata is what Load or Diagnose could read. A field is nil when its file
// was missing or invalid; callers must not fabricate defaults for it.
type Metadata struct {
	Project *domain.Project
	Config  *Config
}

// Load validates every managed metadata file that exists under root (the
// project root; the .devsys/ prefix is added here). Missing files are not
// problems: creating them is devsys init's job, so a write command may run on
// a partial layout as long as every present file is valid.
func Load(root string) (*Metadata, Problems) { return load(root, false) }

// Diagnose is Load plus missing-file problems: the read-only view behind
// `workloom config check`. It reads files plainly — no lock, no recovery, no
// writes — so it also works on state that write commands must refuse.
func Diagnose(root string) (*Metadata, Problems) { return load(root, true) }

func load(root string, reportMissing bool) (*Metadata, Problems) {
	md := &Metadata{}
	var problems Problems
	for _, rel := range managedFiles() {
		path := filepath.Join(root, storage.DevsysDirName, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			if reportMissing {
				problems = append(problems, Problem{File: rel, Reason: "missing; run `workloom init` to create it"})
			}
			continue
		}
		if err != nil {
			problems = append(problems, Problem{File: rel, Reason: fmt.Sprintf("read: %v", err)})
			continue
		}
		switch rel {
		case ProjectFile:
			p, ps := parseProject(rel, data)
			problems = append(problems, ps...)
			if p != nil {
				md.Project = p
			}
		case ConfigFile:
			c, ps := parseConfig(rel, data)
			problems = append(problems, ps...)
			if c != nil {
				md.Config = c
			}
		default:
			problems = append(problems, checkStateFile(rel, data)...)
		}
	}
	return md, problems
}

func parseProject(rel string, data []byte) (*domain.Project, Problems) {
	_, problems := validate(rel, data, projectSpec)
	if problems != nil {
		return nil, problems
	}
	p := &domain.Project{}
	if err := domain.DecodeYAML(data, p); err != nil {
		return nil, Problems{{File: rel, Reason: fmt.Sprintf("decode: %v", err)}}
	}
	return p, nil
}

func parseConfig(rel string, data []byte) (*Config, Problems) {
	values, problems := validate(rel, data, configSpec)
	if problems != nil {
		return nil, problems
	}
	c := &Config{}
	if err := values["schema_version"].Decode(&c.SchemaVersion); err != nil {
		return nil, Problems{{File: rel, Field: "schema_version", Reason: fmt.Sprintf("decode: %v", err)}}
	}
	if node, ok := values["workspace_root"]; ok {
		if err := node.Decode(&c.WorkspaceRoot); err != nil {
			return nil, Problems{{File: rel, Field: "workspace_root", Reason: fmt.Sprintf("decode: %v", err)}}
		}
	}
	if node, ok := values["dispatch_command"]; ok {
		if err := node.Decode(&c.DispatchCommand); err != nil {
			return nil, Problems{{File: rel, Field: "dispatch_command", Reason: fmt.Sprintf("decode: %v", err)}}
		}
	}
	if node, ok := values["knowledge_pages"]; ok && node.Tag != "!!null" {
		if err := node.Decode(&c.KnowledgePages); err != nil {
			return nil, Problems{{File: rel, Field: "knowledge_pages", Reason: fmt.Sprintf("decode: %v", err)}}
		}
	}
	if node, ok := values["knowledge_generator"]; ok {
		if err := node.Decode(&c.KnowledgeGenerator); err != nil {
			return nil, Problems{{File: rel, Field: "knowledge_generator", Reason: fmt.Sprintf("decode: %v", err)}}
		}
	}
	if node, ok := values["default_policy"]; ok {
		if err := node.Decode(&c.DefaultPolicy); err != nil {
			return nil, Problems{{File: rel, Field: "default_policy", Reason: fmt.Sprintf("decode: %v", err)}}
		}
	}
	return c, nil
}

// checkStateFile applies the matching versioned state document contract.
func checkStateFile(rel string, data []byte) Problems {
	spec := currentSpec
	if rel == MilestonesFile {
		spec = milestonesSpec
	}
	_, problems := validate(rel, data, spec)
	return problems
}
