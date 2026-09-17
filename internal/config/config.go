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

	"workloom/internal/storage"
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

// Problem is one located defect in a managed file. Line is 1-based and zero
// when the file as a whole is at fault; Field is empty when the defect does
// not belong to a single field.
type Problem struct {
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Field  string `json:"field,omitempty"`
	Reason string `json:"reason"`
}

// String renders the location as `file[:line][: field]: reason`.
func (p Problem) String() string {
	var b strings.Builder
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

// Project is the strict view of project.yaml. M0.4 covers the metadata header
// devsys init writes; the full 方案 §5.1 model arrives with the M1.1 domain
// types, which extend the whitelist together with this struct.
type Project struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	CreatedAt     string `json:"created_at,omitempty"` // RFC3339; empty when absent
}

// Config is the strict view of config.yaml. No project-level options are
// defined yet, so only schema_version is accepted; later milestones extend
// both the whitelist and this struct.
type Config struct {
	SchemaVersion int `json:"schema_version"`
}

// Metadata is what Load or Diagnose could read. A field is nil when its file
// was missing or invalid; callers must not fabricate defaults for it.
type Metadata struct {
	Project *Project
	Config  *Config
}

// Load validates every managed metadata file that exists under root (the
// project root; the .devsys/ prefix is added here). Missing files are not
// problems: creating them is devsys init's job, so a write command may run on
// a partial layout as long as every present file is valid.
func Load(root string) (*Metadata, Problems) { return load(root, false) }

// Diagnose is Load plus missing-file problems: the read-only view behind
// `devsys config check`. It reads files plainly — no lock, no recovery, no
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
				problems = append(problems, Problem{File: rel, Reason: "missing; run `devsys init` to create it"})
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

func parseProject(rel string, data []byte) (*Project, Problems) {
	values, problems := validate(rel, data, projectSpec)
	if problems != nil {
		return nil, problems
	}
	p := &Project{}
	if err := values["schema_version"].Decode(&p.SchemaVersion); err != nil {
		return nil, Problems{{File: rel, Field: "schema_version", Reason: fmt.Sprintf("decode: %v", err)}}
	}
	p.ID = values["id"].Value
	p.Name = values["name"].Value
	if n, ok := values["created_at"]; ok {
		p.CreatedAt = n.Value
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
	return c, nil
}

// checkStateFile validates a state file: the root must be a mapping with a
// supported schema_version. The rest of the shape (summary/risks/milestones)
// belongs to the M1.1 domain types, so unknown keys are allowed here on
// purpose.
func checkStateFile(rel string, data []byte) Problems {
	_, problems := validate(rel, data, stateSpec)
	return problems
}
