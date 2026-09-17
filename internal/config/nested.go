package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
)

func checkNested(rel, path string, field fieldSpec, node *yaml.Node) Problems {
	var problems Problems
	switch field.kind {
	case kindStrings:
		for i, child := range node.Content {
			name := fmt.Sprintf("%s[%d]", path, i)
			if p := checkField(rel, fieldSpec{name: name, kind: kindString}, child); p != nil {
				problems = append(problems, *p)
			}
		}
	case kindMilestones:
		fields := []fieldSpec{{name: "id", kind: kindString, required: true}, {name: "name", kind: kindString, required: true}, {name: "status", kind: kindString, required: true}}
		for i, child := range node.Content {
			problems = append(problems, checkMapping(rel, fmt.Sprintf("%s[%d]", path, i), child, fields)...)
		}
	case kindScope:
		problems = append(problems, checkMapping(rel, path, node, []fieldSpec{{name: "in", kind: kindStrings}, {name: "out", kind: kindStrings}})...)
	case kindCurrent:
		problems = append(problems, checkMapping(rel, path, node, currentFields)...)
	}
	return problems
}

func checkMapping(rel, path string, node *yaml.Node, fields []fieldSpec) Problems {
	if node.Kind != yaml.MappingNode {
		return Problems{{File: rel, Line: node.Line, Field: path, Reason: "expected mapping"}}
	}
	var problems Problems
	seen := map[string]bool{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		name := path + "." + key.Value
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			problems = append(problems, Problem{File: rel, Line: key.Line, Field: path, Reason: "expected string key"})
			continue
		}
		if seen[key.Value] {
			problems = append(problems, Problem{File: rel, Line: key.Line, Field: name, Reason: "duplicate key"})
			continue
		}
		seen[key.Value] = true
		field := findField(fileSpec{fields: fields}, key.Value)
		if field == nil {
			problems = append(problems, Problem{File: rel, Line: key.Line, Field: name, Reason: "unknown key"})
			continue
		}
		f := *field
		f.name = name
		if p := checkField(rel, f, val); p != nil {
			problems = append(problems, *p)
			continue
		}
		problems = append(problems, checkNested(rel, name, f, val)...)
	}
	for _, field := range fields {
		if field.required && !seen[field.name] {
			problems = append(problems, Problem{File: rel, Line: node.Line, Field: path + "." + field.name, Reason: "required"})
		}
	}
	return problems
}
