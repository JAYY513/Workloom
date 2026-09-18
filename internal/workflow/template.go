package workflow

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// RenderErrorKind classifies a template or environment-reference failure
// (实施计划 M3.2: 渲染失败的错误分类). Callers branch on Kind with errors.As.
type RenderErrorKind string

const (
	// RenderUnknownVariables: the template references names that were not
	// supplied. Rendering never substitutes an empty string for them.
	RenderUnknownVariables RenderErrorKind = "unknown_variable"
	// RenderSyntax: the template or environment reference is structurally
	// invalid.
	RenderSyntax RenderErrorKind = "template_syntax"
	// RenderEnvUndefined: an environment reference did not resolve.
	RenderEnvUndefined RenderErrorKind = "env_undefined"
)

// RenderError is the structured, classified failure of Render or ExpandEnv.
type RenderError struct {
	Kind RenderErrorKind `json:"kind"`
	// Variables are the missing names (unknown_variable, env_undefined),
	// unique and sorted.
	Variables []string `json:"variables,omitempty"`
	// Line is the 1-based line inside the rendered template (template_syntax,
	// 0 when not applicable).
	Line   int    `json:"line,omitempty"`
	Reason string `json:"reason"`
}

func (e *RenderError) Error() string {
	parts := []string{"render: " + string(e.Kind)}
	if e.Line > 0 {
		parts = append(parts, fmt.Sprintf("line %d", e.Line))
	}
	if len(e.Variables) > 0 {
		parts = append(parts, strings.Join(e.Variables, ", "))
	}
	if e.Reason != "" {
		parts = append(parts, e.Reason)
	}
	return strings.Join(parts, ": ")
}

var templateNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)

// Render expands the policy's prompt template with vars (实施计划 M3.2).
func (p *Policy) Render(vars map[string]string) (string, error) {
	return Render(p.Body, vars)
}

// Render expands {{name}} references in body. Whitespace inside the braces is
// allowed ({{ workitem.id }}); names are dot-separated identifiers. Every
// reference must resolve: missing names fail with a classified error listing
// them, and never render as empty strings. Malformed brace structure is a
// syntax error. On failure the partial output is discarded.
func Render(body string, vars map[string]string) (string, error) {
	segs, rerr := scanTemplate(body)
	if rerr != nil {
		return "", rerr
	}
	var missing []string
	var b strings.Builder
	b.Grow(len(body))
	for _, seg := range segs {
		if seg.name == "" {
			b.WriteString(seg.text)
			continue
		}
		v, ok := vars[seg.name]
		if !ok {
			missing = append(missing, seg.name)
			continue
		}
		b.WriteString(v)
	}
	if len(missing) > 0 {
		return "", &RenderError{
			Kind: RenderUnknownVariables, Variables: uniqueSorted(missing),
			Reason: "template variables are not defined",
		}
	}
	return b.String(), nil
}

// templateSegment is one literal run or one {{name}} reference.
type templateSegment struct {
	text string // literal text when name is empty
	name string
}

// scanTemplate splits body into literals and references. Braces are never
// escaped: every {{ starts a reference and a stray }} is a syntax error.
func scanTemplate(body string) ([]templateSegment, *RenderError) {
	var segs []templateSegment
	offset := 0
	for {
		rest := body[offset:]
		open := strings.Index(rest, "{{")
		closeOnly := strings.Index(rest, "}}")
		if open < 0 {
			if closeOnly >= 0 {
				return nil, &RenderError{Kind: RenderSyntax, Line: lineAt(body, offset+closeOnly),
					Reason: "unmatched }}"}
			}
			if rest != "" {
				segs = append(segs, templateSegment{text: rest})
			}
			return segs, nil
		}
		if closeOnly >= 0 && closeOnly < open {
			return nil, &RenderError{Kind: RenderSyntax, Line: lineAt(body, offset+closeOnly),
				Reason: "unmatched }}"}
		}
		if open > 0 {
			segs = append(segs, templateSegment{text: rest[:open]})
		}
		openIdx := offset + open
		end := strings.Index(body[openIdx+2:], "}}")
		if end < 0 {
			return nil, &RenderError{Kind: RenderSyntax, Line: lineAt(body, openIdx),
				Reason: "unclosed {{"}
		}
		inner := body[openIdx+2 : openIdx+2+end]
		name := strings.TrimSpace(inner)
		if !templateNamePattern.MatchString(name) {
			return nil, &RenderError{Kind: RenderSyntax, Line: lineAt(body, openIdx),
				Reason: fmt.Sprintf("invalid variable reference %q", inner)}
		}
		segs = append(segs, templateSegment{name: name})
		offset = openIdx + 2 + end + 2
	}
}

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ExpandEnv resolves $NAME and ${NAME} references in raw at use time. Every
// reference must resolve; undefined names fail with a classified error listing
// them — a missing secret is never silently expanded to "". $$ is a literal
// dollar, and a $ before a non-name character (e.g. "$5") stays literal.
// Callers must not persist the resolved values (方案 §5.3: 密钥不落盘).
func ExpandEnv(raw string, lookup func(string) (string, bool)) (string, error) {
	var b strings.Builder
	b.Grow(len(raw))
	var missing []string
	i := 0
	for i < len(raw) {
		c := raw[i]
		if c != '$' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 < len(raw) && raw[i+1] == '$' {
			b.WriteByte('$')
			i += 2
			continue
		}
		if i+1 < len(raw) && raw[i+1] == '{' {
			end := strings.IndexByte(raw[i+2:], '}')
			if end < 0 {
				return "", &RenderError{Kind: RenderSyntax, Reason: "unclosed ${"}
			}
			name := raw[i+2 : i+2+end]
			if !envNamePattern.MatchString(name) {
				return "", &RenderError{Kind: RenderSyntax,
					Reason: fmt.Sprintf("invalid environment reference %q", name)}
			}
			if v, ok := lookup(name); ok {
				b.WriteString(v)
			} else {
				missing = append(missing, name)
			}
			i += 2 + end + 1
			continue
		}
		if j := i + 1; j < len(raw) && envNameStart(raw[j]) {
			k := j
			for k < len(raw) && envNameChar(raw[k]) {
				k++
			}
			name := raw[j:k]
			if v, ok := lookup(name); ok {
				b.WriteString(v)
			} else {
				missing = append(missing, name)
			}
			i = k
			continue
		}
		b.WriteByte('$')
		i++
	}
	if len(missing) > 0 {
		return "", &RenderError{
			Kind: RenderEnvUndefined, Variables: uniqueSorted(missing),
			Reason: "environment variables are not defined",
		}
	}
	return b.String(), nil
}

func envNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func envNameChar(c byte) bool {
	return envNameStart(c) || (c >= '0' && c <= '9')
}

// lineAt returns the 1-based line of byte offset idx within s.
func lineAt(s string, idx int) int {
	return 1 + strings.Count(s[:idx], "\n")
}

func uniqueSorted(names []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
