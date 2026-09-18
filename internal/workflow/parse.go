package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"workloom/internal/domain"
)

var (
	policyIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	tokenPattern    = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

// knownTopLevel lists every front matter key this build understands, in the
// order 方案 §5.3 declares them. Keys outside this list are recorded as
// warnings, not errors.
var knownTopLevel = []string{
	"id", "name", "version",
	"input",
	"steps", "transitions",
	"approval_points", "completion_rules",
	"gates", "hooks", "concurrency", "limits", "quality_gate", "on_reject",
}

// hookNames lists the workspace lifecycle hooks of 方案 §4.8.
var hookNames = []string{"after_create", "before_run", "after_run", "before_remove"}

// Parse parses one policy file. rel is the slash-separated path below .devsys/
// (used for reporting and to require id == basename). A policy is returned
// only when no error-severity issue was found; warnings accompany a valid
// policy.
func Parse(rel string, data []byte) (*Policy, []Issue) {
	c := &collector{rel: rel}
	front, body, bodyLine, ok := splitFrontMatter(data, c)
	if !ok {
		return nil, c.sorted()
	}
	root, ok := parseFrontMatter(rel, front, c)
	if !ok {
		return nil, c.sorted()
	}
	if root.Kind != yaml.MappingNode {
		c.errorf(root.Line, "", "expected a mapping at the top level, got %s", nodeKind(root))
		return nil, c.sorted()
	}
	if _, rerr := scanTemplate(body); rerr != nil {
		c.errorf(bodyLine+rerr.Line-1, "body", "%s", rerr.Reason)
	}

	fields := c.mapping("", root, knownTopLevel)
	rootLine := root.Line
	p := &Policy{File: rel, Body: body, OnReject: "block"}

	if id, ok := c.requiredString("id", fields["id"], rootLine); ok {
		switch {
		case !policyIDPattern.MatchString(id):
			c.errorf(fields["id"].Line, "id", "invalid id %q: expected %s", id, policyIDPattern)
		case id != baseName(rel):
			c.errorf(fields["id"].Line, "id", "id %q does not match the file name (want %q.md)", id, baseName(rel))
		default:
			p.ID = id
		}
	}
	if name, ok := c.requiredString("name", fields["name"], rootLine); ok {
		p.Name = name
	}
	if v, ok := c.intField("version", fields["version"], 1, rootLine, true); ok {
		p.Version = v
	}

	if in := fields["input"]; in != nil {
		f := c.mapping("input", in, []string{"required", "optional"})
		if list, ok := c.tokenList("input.required", f["required"]); ok {
			p.Input.Required = list
		}
		if list, ok := c.tokenList("input.optional", f["optional"]); ok {
			p.Input.Optional = list
		}
	}

	errorsBeforeSteps := c.errors()
	p.Steps = c.steps(fields["steps"], rootLine)
	stepsInvalid := c.errors() > errorsBeforeSteps
	p.Transitions = c.transitions(fields["transitions"], p.Steps, stepsInvalid)

	if list, ok := c.tokenList("approval_points", fields["approval_points"]); ok {
		p.ApprovalPoints = list
	}
	if list, ok := c.tokenList("completion_rules", fields["completion_rules"]); ok {
		p.CompletionRules = list
	}
	p.Gates = c.gates(fields["gates"])
	p.Hooks = c.hooks(fields["hooks"])
	p.Concurrency = c.concurrency(fields["concurrency"])
	p.Limits = c.limits(fields["limits"])
	p.QualityGate = c.qualityGate(fields["quality_gate"])

	if s, ok := c.optionalString("on_reject", fields["on_reject"]); ok {
		switch {
		case s == "block":
			p.OnReject = "block"
		case strings.HasPrefix(s, "regress:"):
			if st := strings.TrimPrefix(s, "regress:"); !validStatus(st) {
				c.errorf(fields["on_reject"].Line, "on_reject", "unknown regression status %q (valid: %s)", st, statusNames())
			} else {
				p.OnReject = s
			}
		default:
			c.errorf(fields["on_reject"].Line, "on_reject", "expected \"block\" or \"regress:<status>\", got %q", s)
		}
	}

	issues := c.sorted()
	for _, is := range issues {
		if is.Severity == SeverityError {
			return nil, issues
		}
	}
	return p, issues
}

// splitFrontMatter splits a policy file into its YAML front matter (with one
// leading newline so yaml line numbers equal file line numbers), the prompt
// body below the closing `---` line, and the body's 1-based start line.
func splitFrontMatter(data []byte, c *collector) ([]byte, string, int, bool) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	firstEnd := bytes.IndexByte(data, '\n')
	var firstLine string
	if firstEnd < 0 {
		firstLine = string(data)
	} else {
		firstLine = string(data[:firstEnd])
	}
	if strings.TrimRight(firstLine, " \t\r") != "---" {
		c.errorf(1, "", "missing front matter: the file must start with a --- line")
		return nil, "", 0, false
	}
	if firstEnd < 0 {
		c.errorf(1, "", "missing closing --- line for the front matter")
		return nil, "", 0, false
	}

	start := firstEnd + 1
	offset := start
	closeStart, closeEnd := -1, -1
	for {
		lineEnd := len(data)
		if nl := bytes.IndexByte(data[offset:], '\n'); nl >= 0 {
			lineEnd = offset + nl
		}
		if strings.TrimRight(string(data[offset:lineEnd]), " \t\r") == "---" {
			closeStart, closeEnd = offset, lineEnd
			break
		}
		if lineEnd == len(data) {
			break
		}
		offset = lineEnd + 1
	}
	if closeStart < 0 {
		c.errorf(1, "", "missing closing --- line for the front matter")
		return nil, "", 0, false
	}

	front := append([]byte("\n"), data[start:closeStart]...)
	bodyStart := closeEnd
	if bodyStart < len(data) && data[bodyStart] == '\r' {
		bodyStart++
	}
	if bodyStart < len(data) && data[bodyStart] == '\n' {
		bodyStart++
	}
	bodyLine := 1 + bytes.Count(data[:bodyStart], []byte{'\n'})
	return front, string(data[bodyStart:]), bodyLine, true
}

// parseFrontMatter decodes exactly one YAML document from the front matter and
// returns its root node.
func parseFrontMatter(rel string, front []byte, c *collector) (*yaml.Node, bool) {
	dec := yaml.NewDecoder(bytes.NewReader(front))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			c.errorf(0, "", "empty front matter; expected a mapping")
			return nil, false
		}
		c.errorf(0, "", "yaml syntax: %v", err)
		return nil, false
	}
	if len(doc.Content) == 0 {
		c.errorf(doc.Line, "", "empty front matter; expected a mapping")
		return nil, false
	}
	var extra yaml.Node
	switch err := dec.Decode(&extra); {
	case err == nil:
		line := extra.Line
		if line == 0 && len(extra.Content) > 0 {
			line = extra.Content[0].Line
		}
		c.errorf(line, "", "unexpected second document in the front matter")
		return nil, false
	case errors.Is(err, io.EOF):
		return doc.Content[0], true
	default:
		c.errorf(0, "", "yaml syntax: %v", err)
		return nil, false
	}
}

// collector accumulates issues for one file in walk order.
type collector struct {
	rel    string
	issues []Issue
}

func (c *collector) addf(sev Severity, line int, field, format string, args ...any) {
	c.issues = append(c.issues, Issue{
		File: c.rel, Line: line, Field: field,
		Reason: fmt.Sprintf(format, args...), Severity: sev,
	})
}

func (c *collector) errorf(line int, field, format string, args ...any) {
	c.addf(SeverityError, line, field, format, args...)
}

func (c *collector) warnf(line int, field, format string, args ...any) {
	c.addf(SeverityWarning, line, field, format, args...)
}

func (c *collector) errors() int {
	n := 0
	for _, is := range c.issues {
		if is.Severity == SeverityError {
			n++
		}
	}
	return n
}

// sorted returns the collected issues in document order (stable by line).
func (c *collector) sorted() []Issue {
	out := append([]Issue(nil), c.issues...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

// mappingEntry is one scalar-keyed entry of a YAML mapping.
type mappingEntry struct {
	name string
	line int
	val  *yaml.Node
}

// mappingEntries checks that node is a mapping and returns its entries in
// document order, flagging duplicate and non-scalar keys.
func (c *collector) mappingEntries(field string, node *yaml.Node) []mappingEntry {
	if node.Kind != yaml.MappingNode {
		c.errorf(node.Line, field, "expected mapping, got %s", nodeKind(node))
		return nil
	}
	out := make([]mappingEntry, 0, len(node.Content)/2)
	seen := map[string]bool{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			c.errorf(key.Line, field, "non-scalar key (%s)", nodeKind(key))
			continue
		}
		name := key.Value
		if seen[name] {
			c.errorf(key.Line, joinField(field, name), "duplicate key")
			continue
		}
		seen[name] = true
		out = append(out, mappingEntry{name: name, line: key.Line, val: val})
	}
	return out
}

// mapping checks that node is a mapping and returns the value nodes of its
// known keys. Unknown keys are recorded as warnings (实施计划 M3.1).
func (c *collector) mapping(field string, node *yaml.Node, known []string) map[string]*yaml.Node {
	if node == nil {
		return nil
	}
	entries := c.mappingEntries(field, node)
	if entries == nil {
		return nil
	}
	out := make(map[string]*yaml.Node, len(entries))
	for _, e := range entries {
		if !contains(known, e.name) {
			c.warnf(e.line, joinField(field, e.name), "unknown key")
			continue
		}
		out[e.name] = e.val
	}
	return out
}

func (c *collector) requiredString(field string, node *yaml.Node, at int) (string, bool) {
	if node == nil {
		c.errorf(at, field, "required")
		return "", false
	}
	return c.stringValue(field, node)
}

func (c *collector) optionalString(field string, node *yaml.Node) (string, bool) {
	if node == nil {
		return "", false
	}
	return c.stringValue(field, node)
}

func (c *collector) stringValue(field string, node *yaml.Node) (string, bool) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		c.errorf(node.Line, field, "expected string, got %s", nodeKind(node))
		return "", false
	}
	if strings.TrimSpace(node.Value) == "" {
		c.errorf(node.Line, field, "must not be empty")
		return "", false
	}
	return node.Value, true
}

func (c *collector) intField(field string, node *yaml.Node, min int, at int, required bool) (int, bool) {
	if node == nil {
		if required {
			c.errorf(at, field, "required")
		}
		return 0, false
	}
	if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
		c.errorf(node.Line, field, "expected integer, got %s", nodeKind(node))
		return 0, false
	}
	var n int
	if err := node.Decode(&n); err != nil {
		c.errorf(node.Line, field, "expected integer: %v", err)
		return 0, false
	}
	if n < min {
		c.errorf(node.Line, field, "must be >= %d, got %d", min, n)
		return 0, false
	}
	return n, true
}

func (c *collector) boolField(field string, node *yaml.Node) (bool, bool) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
		c.errorf(node.Line, field, "expected boolean, got %s", nodeKind(node))
		return false, false
	}
	var b bool
	if err := node.Decode(&b); err != nil {
		c.errorf(node.Line, field, "expected boolean: %v", err)
		return false, false
	}
	return b, true
}

// tokenList parses a sequence of non-empty, unique lowercase tokens.
func (c *collector) tokenList(field string, node *yaml.Node) ([]string, bool) {
	if node == nil {
		return nil, false
	}
	if node.Kind != yaml.SequenceNode {
		c.errorf(node.Line, field, "expected sequence, got %s", nodeKind(node))
		return nil, false
	}
	out := make([]string, 0, len(node.Content))
	seen := map[string]bool{}
	ok := true
	for i, item := range node.Content {
		itemField := fmt.Sprintf("%s[%d]", field, i)
		if item.Kind != yaml.ScalarNode || item.Tag != "!!str" || strings.TrimSpace(item.Value) == "" {
			c.errorf(item.Line, itemField, "expected non-empty string, got %s", nodeKind(item))
			ok = false
			continue
		}
		s := item.Value
		if !tokenPattern.MatchString(s) {
			c.errorf(item.Line, itemField, "invalid token %q: expected %s", s, tokenPattern)
			ok = false
			continue
		}
		if seen[s] {
			c.errorf(item.Line, itemField, "duplicate value %q", s)
			ok = false
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out, ok
}

// statusList parses a sequence of work item status names.
func (c *collector) statusList(field string, node *yaml.Node) ([]string, bool) {
	if node == nil {
		return nil, false
	}
	if node.Kind != yaml.SequenceNode {
		c.errorf(node.Line, field, "expected sequence, got %s", nodeKind(node))
		return nil, false
	}
	out := make([]string, 0, len(node.Content))
	seen := map[string]bool{}
	ok := true
	for i, item := range node.Content {
		itemField := fmt.Sprintf("%s[%d]", field, i)
		if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
			c.errorf(item.Line, itemField, "expected string, got %s", nodeKind(item))
			ok = false
			continue
		}
		s := item.Value
		if !validStatus(s) {
			c.errorf(item.Line, itemField, "unknown status %q (valid: %s)", s, statusNames())
			ok = false
			continue
		}
		if seen[s] {
			c.errorf(item.Line, itemField, "duplicate value %q", s)
			ok = false
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out, ok
}

func (c *collector) steps(node *yaml.Node, at int) []Step {
	if node == nil {
		c.errorf(at, "steps", "required")
		return nil
	}
	if node.Kind != yaml.SequenceNode {
		c.errorf(node.Line, "steps", "expected sequence, got %s", nodeKind(node))
		return nil
	}
	if len(node.Content) == 0 {
		c.errorf(node.Line, "steps", "must not be empty")
		return nil
	}
	steps := make([]Step, 0, len(node.Content))
	seen := map[string]int{}
	for i, item := range node.Content {
		prefix := fmt.Sprintf("steps[%d]", i)
		f := c.mapping(prefix, item, []string{"id", "type", "required", "status"})
		if f == nil {
			continue
		}
		var st Step
		if id, ok := c.requiredString(prefix+".id", f["id"], item.Line); ok {
			if first, dup := seen[id]; dup {
				c.errorf(f["id"].Line, prefix+".id", "duplicate step id %q (first declared at index %d)", id, first)
			} else if !tokenPattern.MatchString(id) {
				c.errorf(f["id"].Line, prefix+".id", "invalid step id %q: expected %s", id, tokenPattern)
			} else {
				seen[id] = i
				st.ID = id
			}
		}
		if typ, ok := c.requiredString(prefix+".type", f["type"], item.Line); ok {
			if !tokenPattern.MatchString(typ) {
				c.errorf(f["type"].Line, prefix+".type", "invalid step type %q: expected %s", typ, tokenPattern)
			} else {
				st.Type = typ
			}
		}
		st.Required = true
		if n := f["required"]; n != nil {
			if b, ok := c.boolField(prefix+".required", n); ok {
				st.Required = b
			}
		}
		if n := f["status"]; n != nil {
			if s, ok := c.optionalString(prefix+".status", n); ok {
				if !validStatus(s) {
					c.errorf(n.Line, prefix+".status", "unknown status %q (valid: %s)", s, statusNames())
				} else {
					st.Status = s
				}
			}
		}
		steps = append(steps, st)
	}
	return steps
}

func (c *collector) transitions(node *yaml.Node, steps []Step, stepsInvalid bool) []Transition {
	if node == nil {
		return nil
	}
	if node.Kind != yaml.SequenceNode {
		c.errorf(node.Line, "transitions", "expected sequence, got %s", nodeKind(node))
		return nil
	}
	ids := map[string]bool{}
	for _, st := range steps {
		if st.ID != "" {
			ids[st.ID] = true
		}
	}
	out := make([]Transition, 0, len(node.Content))
	seen := map[string]bool{}
	for i, item := range node.Content {
		prefix := fmt.Sprintf("transitions[%d]", i)
		f := c.mapping(prefix, item, []string{"from", "to", "when"})
		if f == nil {
			continue
		}
		var tr Transition
		if from, ok := c.requiredString(prefix+".from", f["from"], item.Line); ok {
			if !stepsInvalid && !ids[from] {
				c.errorf(f["from"].Line, prefix+".from", "unknown step %q", from)
			} else {
				tr.From = from
			}
		}
		if to, ok := c.requiredString(prefix+".to", f["to"], item.Line); ok {
			if !stepsInvalid && to != "done" && !ids[to] {
				c.errorf(f["to"].Line, prefix+".to", "unknown target %q (allowed: a step id or \"done\")", to)
			} else {
				tr.To = to
			}
		}
		if n := f["when"]; n != nil {
			if s, ok := c.optionalString(prefix+".when", n); ok {
				tr.When = s
				cond, cerr := parseCondition(s)
				if cerr != nil {
					c.errorf(n.Line, prefix+".when", "%s", cerr.Error())
				} else {
					tr.Cond = cond
				}
			}
		}
		if tr.From != "" && tr.To != "" {
			key := tr.From + "\x00" + tr.To + "\x00" + tr.When
			if seen[key] {
				c.errorf(item.Line, prefix, "duplicate transition %s -> %s", tr.From, tr.To)
				continue
			}
			seen[key] = true
		}
		out = append(out, tr)
	}
	return out
}

func (c *collector) gates(node *yaml.Node) Gates {
	var g Gates
	if node == nil {
		return g
	}
	f := c.mapping("gates", node, []string{"exempt_stages", "stages"})
	if f == nil {
		return g
	}
	if list, ok := c.statusList("gates.exempt_stages", f["exempt_stages"]); ok {
		g.ExemptStages = list
	}
	stages := f["stages"]
	if stages == nil {
		return g
	}
	entries := c.mappingEntries("gates.stages", stages)
	g.Stages = map[string]Gate{}
	for _, e := range entries {
		fieldPath := "gates.stages." + e.name
		if !validStatus(e.name) {
			c.errorf(e.line, fieldPath, "unknown status %q (valid: %s)", e.name, statusNames())
			continue
		}
		gf := c.mapping(fieldPath, e.val, []string{"require_artifacts", "require_min_artifacts", "require_comment", "require_approval"})
		if gf == nil {
			continue
		}
		var gate Gate
		if list, ok := c.tokenList(fieldPath+".require_artifacts", gf["require_artifacts"]); ok {
			gate.RequireArtifacts = list
		}
		if n, ok := c.intField(fieldPath+".require_min_artifacts", gf["require_min_artifacts"], 0, e.line, false); ok {
			gate.RequireMinArtifacts = n
		}
		if n := gf["require_comment"]; n != nil {
			if b, ok := c.boolField(fieldPath+".require_comment", n); ok {
				gate.RequireComment = b
			}
		}
		if n := gf["require_approval"]; n != nil {
			if b, ok := c.boolField(fieldPath+".require_approval", n); ok {
				gate.RequireApproval = b
			}
		}
		g.Stages[e.name] = gate
	}
	return g
}

func (c *collector) hooks(node *yaml.Node) map[string]Hook {
	if node == nil {
		return nil
	}
	entries := c.mappingEntries("hooks", node)
	if entries == nil {
		return nil
	}
	out := make(map[string]Hook, len(entries))
	for _, e := range entries {
		fieldPath := "hooks." + e.name
		if !contains(hookNames, e.name) {
			// Closed value domains fail closed: silently skipping a hook would
			// drop a declared command.
			c.errorf(e.line, fieldPath, "unknown hook name (known: %s)", strings.Join(hookNames, ", "))
			continue
		}
		f := c.mapping(fieldPath, e.val, []string{"command", "timeout_seconds"})
		if f == nil {
			continue
		}
		var hk Hook
		if s, ok := c.requiredString(fieldPath+".command", f["command"], e.line); ok {
			hk.Command = s
		}
		if n, ok := c.intField(fieldPath+".timeout_seconds", f["timeout_seconds"], 1, e.line, false); ok {
			hk.TimeoutSeconds = n
		}
		out[e.name] = hk
	}
	return out
}

func (c *collector) concurrency(node *yaml.Node) Concurrency {
	var cc Concurrency
	if node == nil {
		return cc
	}
	f := c.mapping("concurrency", node, []string{"global", "per_status"})
	if f == nil {
		return cc
	}
	if n, ok := c.intField("concurrency.global", f["global"], 1, node.Line, false); ok {
		cc.Global = n
	}
	perStatus := f["per_status"]
	if perStatus == nil {
		return cc
	}
	entries := c.mappingEntries("concurrency.per_status", perStatus)
	cc.PerStatus = map[string]int{}
	for _, e := range entries {
		fieldPath := "concurrency.per_status." + e.name
		if !validStatus(e.name) {
			c.errorf(e.line, fieldPath, "unknown status %q (valid: %s)", e.name, statusNames())
			continue
		}
		if n, ok := c.intField(fieldPath, e.val, 1, e.line, true); ok {
			cc.PerStatus[e.name] = n
		}
	}
	return cc
}

func (c *collector) limits(node *yaml.Node) Limits {
	var l Limits
	if node == nil {
		return l
	}
	f := c.mapping("limits", node, []string{"max_attempts", "run_timeout_seconds", "stall_threshold_seconds", "backoff_max_seconds"})
	if f == nil {
		return l
	}
	if n, ok := c.intField("limits.max_attempts", f["max_attempts"], 1, node.Line, false); ok {
		l.MaxAttempts = n
	}
	if n, ok := c.intField("limits.run_timeout_seconds", f["run_timeout_seconds"], 1, node.Line, false); ok {
		l.RunTimeoutSeconds = n
	}
	if n, ok := c.intField("limits.stall_threshold_seconds", f["stall_threshold_seconds"], 1, node.Line, false); ok {
		l.StallThresholdSeconds = n
	}
	if n, ok := c.intField("limits.backoff_max_seconds", f["backoff_max_seconds"], 1, node.Line, false); ok {
		l.BackoffMaxSeconds = n
	}
	return l
}

func (c *collector) qualityGate(node *yaml.Node) QualityGate {
	var q QualityGate
	if node == nil {
		return q
	}
	f := c.mapping("quality_gate", node, []string{"min_score"})
	if f == nil {
		return q
	}
	if n, ok := c.intField("quality_gate.min_score", f["min_score"], 0, node.Line, true); ok {
		if n > 100 {
			c.errorf(f["min_score"].Line, "quality_gate.min_score", "must be <= 100, got %d", n)
		} else {
			q.MinScore = n
		}
	}
	return q
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func joinField(field, name string) string {
	if field == "" {
		return name
	}
	return field + "." + name
}

// baseName returns the policy id implied by rel: the file name without the
// .md suffix.
func baseName(rel string) string {
	base := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		base = rel[i+1:]
	}
	return strings.TrimSuffix(base, ".md")
}

func validStatus(s string) bool {
	return contains(domain.AllStatuses(), s)
}

func statusNames() string { return strings.Join(domain.AllStatuses(), ", ") }

func nodeKind(n *yaml.Node) string {
	if n.Tag != "" {
		return n.Tag
	}
	switch n.Kind {
	case yaml.MappingNode:
		return "!!map"
	case yaml.SequenceNode:
		return "!!seq"
	case yaml.ScalarNode:
		return "!!scalar"
	default:
		return "node"
	}
}
