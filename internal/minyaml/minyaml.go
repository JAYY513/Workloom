// Package minyaml implements the deliberately tiny YAML subset used by M0.2
// for fixed-shape files: single-quoted string scalars plus plain scalars, and
// a line-oriented reader for the two shapes devsys writes itself (project
// placeholders and the user-level registry).
//
// It exists so this step stays dependency-free on offline machines; the
// storage primitives introduced in M0.3 (stable-key serialization) replace it.
package minyaml

import (
	"fmt"
	"strings"
)

// QuoteScalar renders s as a single-quoted YAML scalar; single quotes inside
// s are doubled. Control characters cannot be represented in a single-quoted
// scalar and are rejected.
func QuoteScalar(s string) (string, error) {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("value contains control character %q", r)
		}
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'", nil
}

// UnquoteScalar decodes a scalar produced by QuoteScalar. Plain (unquoted)
// scalars are returned as-is so hand-edited registries stay readable.
func UnquoteScalar(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty scalar")
	}
	if s[0] == '\'' {
		if len(s) < 2 || s[len(s)-1] != '\'' {
			return "", fmt.Errorf("unterminated single-quoted scalar")
		}
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
	}
	if strings.Contains(s, "'") {
		return "", fmt.Errorf("unexpected quote in plain scalar")
	}
	return s, nil
}
