// Declarative condition evaluation for workflow transitions (方案 §5.3,
// 实施计划 M3.6). Conditions are parsed and validated at policy load time
// against a fixed field whitelist; evaluation is a pure function over Facts.
// There are no free variables, no compound expressions and no code: an
// unknown field or operator is a policy error, not a runtime surprise.
package workflow

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"workloom/internal/domain"
)

// Condition operators.
const (
	OpEqual    = "=="
	OpNotEqual = "!="
	OpLess     = "<"
	OpGreater  = ">"
	OpIn       = "in"
	OpExists   = "exists"
)

// factKind is the value type of one whitelisted fact.
type factKind int

const (
	kindStringFact factKind = iota
	kindIntFact
	kindBoolFact
)

func (k factKind) String() string {
	switch k {
	case kindIntFact:
		return "int"
	case kindBoolFact:
		return "bool"
	default:
		return "string"
	}
}

// factSpec describes one whitelisted fact.
type factSpec struct {
	kind factKind
	// optional facts may be tested with `exists`; always-present facts make
	// `exists` meaningless and are rejected at load time.
	optional bool
}

// factWhitelist is the complete set of fields a `when` expression may use
// (§5.3: 条件字段限定在白名单内).
var factWhitelist = map[string]factSpec{
	"workitem.id":                   {kind: kindStringFact},
	"workitem.status":               {kind: kindStringFact},
	"workitem.type":                 {kind: kindStringFact},
	"workitem.priority":             {kind: kindIntFact},
	"workitem.clarification_needed": {kind: kindBoolFact},
	"workitem.approval_required":    {kind: kindBoolFact},
	"workitem.parent_id":            {kind: kindStringFact, optional: true},
}

// Condition is one parsed, load-validated transition condition.
type Condition struct {
	Field string
	Op    string
	// Literals are the compared values: exactly one for == != < >, one or
	// more for in.
	Literals []string
	// Text is the original expression, kept for reporting.
	Text string
}

// Facts is the whitelisted work item fact set conditions evaluate against.
type Facts struct {
	ID                  string
	Status              string
	Type                string
	Priority            int
	ClarificationNeeded bool
	ApprovalRequired    bool
	ParentID            string
}

// FactsOf projects a work item onto the condition fact set.
func FactsOf(wi *domain.WorkItem) Facts {
	facts := Facts{
		ID:                  wi.ID,
		Status:              wi.Status,
		Type:                wi.Type,
		Priority:            wi.Priority,
		ClarificationNeeded: wi.ClarificationNeeded,
		ApprovalRequired:    wi.ApprovalRequired,
	}
	if wi.ParentID != nil {
		facts.ParentID = *wi.ParentID
	}
	return facts
}

// Eval reports whether the condition holds for facts.
func (c *Condition) Eval(f Facts) bool {
	switch c.Field {
	case "workitem.id":
		return compareString(f.ID, c)
	case "workitem.status":
		return compareString(f.Status, c)
	case "workitem.type":
		return compareString(f.Type, c)
	case "workitem.priority":
		return compareInt(f.Priority, c)
	case "workitem.clarification_needed":
		return compareBool(f.ClarificationNeeded, c)
	case "workitem.approval_required":
		return compareBool(f.ApprovalRequired, c)
	case "workitem.parent_id":
		if c.Op == OpExists {
			return f.ParentID != ""
		}
		return compareString(f.ParentID, c)
	default:
		return false // unreachable: load-time validation rejects unknown fields
	}
}

func compareString(v string, c *Condition) bool {
	switch c.Op {
	case OpEqual:
		return v == c.Literals[0]
	case OpNotEqual:
		return v != c.Literals[0]
	case OpIn:
		for _, lit := range c.Literals {
			if v == lit {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func compareInt(v int, c *Condition) bool {
	lit, err := strconv.Atoi(c.Literals[0])
	if err != nil {
		return false // unreachable: load-time validation parses literals
	}
	switch c.Op {
	case OpEqual:
		return v == lit
	case OpNotEqual:
		return v != lit
	case OpLess:
		return v < lit
	case OpGreater:
		return v > lit
	default:
		return false
	}
}

func compareBool(v bool, c *Condition) bool {
	lit := c.Literals[0] == "true"
	switch c.Op {
	case OpEqual:
		return v == lit
	case OpNotEqual:
		return v != lit
	default:
		return false
	}
}

// parseCondition parses and validates one `when` expression. The returned
// error is a policy error: unknown fields or operators, compound syntax and
// type mismatches are rejected here, never at evaluation time.
func parseCondition(text string) (*Condition, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, fmt.Errorf("empty condition")
	}
	if strings.Contains(trimmed, "&&") || strings.Contains(trimmed, "||") ||
		strings.ContainsAny(trimmed, "()") {
		return nil, fmt.Errorf("compound expressions are not supported; conditions are a single comparison")
	}
	parts := strings.Fields(trimmed)
	if len(parts) < 2 {
		return nil, fmt.Errorf("malformed condition %q: expected `<field> <op> <literal>`", trimmed)
	}
	name, op := parts[0], parts[1]
	spec, ok := factWhitelist[name]
	if !ok {
		return nil, fmt.Errorf("unknown field %q (whitelist: %s)", name, whitelistNames())
	}

	// `field exists` (two tokens).
	if op == OpExists {
		if len(parts) != 2 {
			return nil, fmt.Errorf("malformed condition %q: expected `<field> <op> <literal>`", trimmed)
		}
		if !spec.optional {
			return nil, fmt.Errorf("exists is only supported for optional fields (got %q)", name)
		}
		return &Condition{Field: name, Op: OpExists, Text: trimmed}, nil
	}
	if len(parts) < 3 {
		return nil, fmt.Errorf("malformed condition %q: expected `<field> <op> <literal>`", trimmed)
	}
	switch op {
	case OpEqual, OpNotEqual:
		value, quoted, err := stripOuterQuotes(parts[2])
		if err != nil {
			return nil, err
		}
		if err := validateLiteral(name, spec, value, quoted); err != nil {
			return nil, err
		}
		return &Condition{Field: name, Op: op, Literals: []string{value}, Text: trimmed}, nil
	case OpLess, OpGreater:
		if spec.kind != kindIntFact {
			return nil, fmt.Errorf("operator %s is only allowed on integer fields (field %q is %s)", op, name, spec.kind)
		}
		value, quoted, err := stripOuterQuotes(parts[2])
		if err != nil {
			return nil, err
		}
		if err := validateLiteral(name, spec, value, quoted); err != nil {
			return nil, err
		}
		return &Condition{Field: name, Op: op, Literals: []string{value}, Text: trimmed}, nil
	case OpIn:
		if spec.kind != kindStringFact {
			return nil, fmt.Errorf("operator in is only allowed on string fields (field %q is %s)", name, spec.kind)
		}
		// The literal list may contain spaces after commas, so re-join the
		// remaining tokens before splitting on commas.
		joined := strings.Join(parts[2:], " ")
		members := strings.Split(joined, ",")
		seen := map[string]bool{}
		literals := make([]string, 0, len(members))
		for _, member := range members {
			value, _, err := stripOuterQuotes(strings.TrimSpace(member))
			if err != nil {
				return nil, err
			}
			if value == "" {
				return nil, fmt.Errorf("operator in has an empty literal")
			}
			if seen[value] {
				return nil, fmt.Errorf("operator in has a duplicate literal %q", value)
			}
			seen[value] = true
			literals = append(literals, value)
		}
		return &Condition{Field: name, Op: op, Literals: literals, Text: trimmed}, nil
	default:
		return nil, fmt.Errorf("unknown operator %q (supported: == != < > in exists)", op)
	}
}

// stripOuterQuotes removes one matching pair of single or double quotes;
// quoted reports whether a pair was removed. An unbalanced quote is a policy
// error, so a mistyped literal never matches silently.
func stripOuterQuotes(literal string) (value string, quoted bool, err error) {
	if literal == "" {
		return literal, false, nil
	}
	switch literal[0] {
	case '\'', '"':
		if len(literal) < 2 || literal[len(literal)-1] != literal[0] {
			return "", false, fmt.Errorf("unbalanced quote in literal %q", literal)
		}
		return literal[1 : len(literal)-1], true, nil
	}
	if last := literal[len(literal)-1]; last == '\'' || last == '"' {
		return "", false, fmt.Errorf("unbalanced quote in literal %q", literal)
	}
	return literal, false, nil
}

func validateLiteral(name string, spec factSpec, value string, quoted bool) error {
	switch spec.kind {
	case kindBoolFact:
		if value != "true" && value != "false" {
			return fmt.Errorf("field %q is bool; expected literal true or false, got %q", name, value)
		}
	case kindIntFact:
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("field %q is int; expected integer literal, got %q", name, value)
		}
	default:
		// A quoted empty string is a valid explicit literal (e.g. a missing
		// parent); an unquoted empty token is impossible.
		if !quoted && strings.TrimSpace(value) == "" {
			return fmt.Errorf("field %q needs a non-empty literal", name)
		}
	}
	return nil
}

func whitelistNames() string {
	names := make([]string, 0, len(factWhitelist))
	for name := range factWhitelist {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
