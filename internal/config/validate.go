package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// fieldKind is the wire type a field must have.
type fieldKind int

const (
	kindInt fieldKind = iota
	kindString
	kindTimestamp // scalar rendered as RFC3339
	kindStrings
	kindScope
	kindCurrent
	kindMilestones
)

type fieldSpec struct {
	name     string
	kind     fieldKind
	required bool
}

// fileSpec is the whitelist for one managed file or nested mapping.
type fileSpec struct {
	fields []fieldSpec
	// allowExtra permits keys outside the whitelist (config.yaml: M0.4 checks
	// only schema_version).
	allowExtra bool
}

var projectSpec = fileSpec{fields: []fieldSpec{
	{name: "schema_version", kind: kindInt, required: true},
	{name: "id", kind: kindString, required: true},
	{name: "name", kind: kindString, required: true},
	{name: "created_at", kind: kindTimestamp},
	{name: "updated_at", kind: kindTimestamp},
	{name: "description", kind: kindString},
	{name: "status", kind: kindString},
	{name: "current_phase", kind: kindString},
	{name: "goals", kind: kindStrings},
	{name: "constraints", kind: kindStrings},
	{name: "tech_stack", kind: kindStrings},
	{name: "scope", kind: kindScope},
	{name: "current_state", kind: kindCurrent},
	{name: "milestones", kind: kindMilestones},
	{name: "blueprint_artifact_id", kind: kindString},
}}

var configSpec = fileSpec{fields: []fieldSpec{
	{name: "schema_version", kind: kindInt, required: true},
	// workspace_root relocates the execution workspaces of 方案 §4.8; it is
	// optional and defaults to <project>/.devsys/workspaces.
	{name: "workspace_root", kind: kindString},
	// dispatch_command is the shell command a scheduling tick runs for each
	// dispatched attempt (M6.4); the per-harness adapters of M6.7 replace it.
	{name: "dispatch_command", kind: kindString},
	// knowledge_pages lists the page-layer roots the knowledge commands read
	// (M5.1, 方案 §12.6); empty selects the built-in candidates.
	{name: "knowledge_pages", kind: kindStrings},
	// knowledge_generator is the page generator command a refresh runs
	// (M5.3, 方案 §12.6); empty means no generator is installed.
	{name: "knowledge_generator", kind: kindString},
	// default_policy is the workflow policy that governs work items declaring
	// no instance of their own (方案 §4.7/§5.3 project-level default); empty
	// leaves such work items ungated, exactly as before the key existed.
	{name: "default_policy", kind: kindString},
}}

var currentFields = []fieldSpec{
	{name: "summary", kind: kindString},
	{name: "risks", kind: kindStrings},
	{name: "blockers", kind: kindStrings},
	{name: "next_focus", kind: kindStrings},
}
var currentSpec = fileSpec{fields: append([]fieldSpec{{name: "schema_version", kind: kindInt, required: true}}, currentFields...)}
var milestonesSpec = fileSpec{fields: []fieldSpec{
	{name: "schema_version", kind: kindInt, required: true},
	{name: "milestones", kind: kindMilestones},
}}

func findField(spec fileSpec, name string) *fieldSpec {
	for i := range spec.fields {
		if spec.fields[i].name == name {
			return &spec.fields[i]
		}
	}
	return nil
}

// validate checks one document against spec and returns the nodes of its
// known fields. A file with a missing or unsupported schema_version yields
// only the version problem: its shape cannot be interpreted, and reporting
// whitelist noise from an unknown schema would mislead (方案 §14.1).
func validate(rel string, data []byte, spec fileSpec) (map[string]*yaml.Node, Problems) {
	root, problems := parseDoc(rel, data)
	if problems != nil {
		return nil, problems
	}
	if root.Kind != yaml.MappingNode {
		return nil, Problems{{File: rel, Line: root.Line,
			Reason: fmt.Sprintf("expected a mapping at the top level, got %s", nodeKind(root))}}
	}

	versionNode, versionDup := mappingValue(root, "schema_version")
	switch {
	case versionDup > 0:
		return nil, Problems{{File: rel, Line: versionDup, Field: "schema_version", Reason: "duplicate key"}}
	case versionNode == nil:
		return nil, Problems{{File: rel, Line: root.Line, Field: "schema_version", Reason: "missing"}}
	}
	var version int
	if err := versionNode.Decode(&version); err != nil {
		return nil, Problems{{File: rel, Line: versionNode.Line, Field: "schema_version",
			Reason: fmt.Sprintf("expected integer, got %s", nodeKind(versionNode))}}
	}
	if version != SupportedSchemaVersion {
		return nil, Problems{{File: rel, Line: versionNode.Line, Field: "schema_version",
			Reason: fmt.Sprintf("unsupported version %d (this build supports %d); refusing to write, migration must be explicit",
				version, SupportedSchemaVersion)}}
	}

	values := map[string]*yaml.Node{}
	seen := map[string]bool{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, val := root.Content[i], root.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			problems = append(problems, Problem{File: rel, Line: key.Line,
				Reason: fmt.Sprintf("non-scalar key (%s)", nodeKind(key))})
			continue
		}
		name := key.Value
		if seen[name] {
			problems = append(problems, Problem{File: rel, Line: key.Line, Field: name, Reason: "duplicate key"})
			continue
		}
		seen[name] = true

		field := findField(spec, name)
		if field == nil {
			if !spec.allowExtra {
				problems = append(problems, Problem{File: rel, Line: key.Line, Field: name, Reason: "unknown key"})
			}
			continue
		}
		if p := checkField(rel, *field, val); p != nil {
			problems = append(problems, *p)
			continue
		}
		problems = append(problems, checkNested(rel, name, *field, val)...)
		values[name] = val
	}
	for _, field := range spec.fields {
		if field.required && !seen[field.name] {
			problems = append(problems, Problem{File: rel, Line: root.Line, Field: field.name, Reason: "required"})
		}
	}
	if problems != nil {
		return nil, problems
	}
	return values, nil
}

func checkField(rel string, field fieldSpec, val *yaml.Node) *Problem {
	switch field.kind {
	case kindStrings:
		// null keeps the "not recorded yet" state distinct from an empty list.
		if val.Tag == "!!null" {
			return nil
		}
		if val.Kind != yaml.SequenceNode {
			return &Problem{File: rel, Line: val.Line, Field: field.name, Reason: "expected sequence or null"}
		}
		for i, item := range val.Content {
			if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
				return &Problem{File: rel, Line: item.Line, Field: field.name,
					Reason: fmt.Sprintf("item %d: expected string, got %s", i+1, nodeKind(item))}
			}
		}
	case kindMilestones:
		// null keeps the "not recorded yet" state distinct from an empty list.
		if val.Tag != "!!null" && val.Kind != yaml.SequenceNode {
			return &Problem{File: rel, Line: val.Line, Field: field.name, Reason: "expected sequence or null"}
		}
	case kindScope, kindCurrent:
		if val.Kind != yaml.MappingNode {
			return &Problem{File: rel, Line: val.Line, Field: field.name, Reason: "expected mapping"}
		}
	case kindInt:
		var n int
		if err := val.Decode(&n); err != nil {
			return &Problem{File: rel, Line: val.Line, Field: field.name,
				Reason: fmt.Sprintf("expected integer, got %s", nodeKind(val))}
		}
	case kindString:
		if val.Kind != yaml.ScalarNode || val.Tag != "!!str" {
			return &Problem{File: rel, Line: val.Line, Field: field.name,
				Reason: fmt.Sprintf("expected string, got %s", nodeKind(val))}
		}
		if field.required && val.Value == "" {
			return &Problem{File: rel, Line: val.Line, Field: field.name, Reason: "required, but empty"}
		}
	case kindTimestamp:
		if val.Kind != yaml.ScalarNode || (val.Tag != "!!str" && val.Tag != "!!timestamp") {
			return &Problem{File: rel, Line: val.Line, Field: field.name,
				Reason: fmt.Sprintf("expected RFC3339 timestamp, got %s", nodeKind(val))}
		}
		if _, err := time.Parse(time.RFC3339, val.Value); err != nil {
			return &Problem{File: rel, Line: val.Line, Field: field.name,
				Reason: fmt.Sprintf("expected RFC3339 timestamp, got %q", val.Value)}
		}
	}
	return nil
}

// parseDoc decodes exactly one YAML document and returns its root node.
func parseDoc(rel string, data []byte) (*yaml.Node, Problems) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, Problems{{File: rel, Reason: "empty file; expected a mapping with schema_version"}}
		}
		return nil, Problems{{File: rel, Reason: fmt.Sprintf("yaml syntax: %v", err)}}
	}
	var extra yaml.Node
	switch err := dec.Decode(&extra); {
	case err == nil:
		// extra.Line is the `---` document start; fall back to its root node.
		line := extra.Line
		if line == 0 && len(extra.Content) > 0 {
			line = extra.Content[0].Line
		}
		return nil, Problems{{File: rel, Line: line, Reason: "unexpected second document"}}
	case errors.Is(err, io.EOF):
	default:
		return nil, Problems{{File: rel, Reason: fmt.Sprintf("yaml syntax: %v", err)}}
	}
	if len(doc.Content) == 0 {
		return nil, Problems{{File: rel, Reason: "empty document; expected a mapping with schema_version"}}
	}
	return doc.Content[0], nil
}

// mappingValue returns the value node of the first occurrence of name, plus
// the line of a second occurrence when the key is duplicated.
func mappingValue(root *yaml.Node, name string) (val *yaml.Node, dupLine int) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i]
		if key.Kind != yaml.ScalarNode || key.Value != name {
			continue
		}
		if val != nil {
			return val, key.Line
		}
		val = root.Content[i+1]
	}
	return val, 0
}

// nodeKind names a node the way YAML users see it (!!str, !!int, !!seq, ...).
func nodeKind(n *yaml.Node) string {
	switch n.Kind {
	case yaml.ScalarNode:
		return strings.TrimPrefix(n.Tag, "tag:yaml.org,2002:")
	case yaml.SequenceNode:
		return "!!seq"
	case yaml.MappingNode:
		return "!!map"
	case yaml.AliasNode:
		return "alias"
	default:
		return "unknown"
	}
}
