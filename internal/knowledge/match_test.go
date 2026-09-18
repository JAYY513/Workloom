package knowledge

import (
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		// Segment wildcards stay inside one segment.
		{"internal/*.go", "internal/app.go", true},
		{"internal/*.go", "internal/sub/app.go", false},
		{"*.key", "certs/server.key", true},
		{"*.key", "server.key", true},
		{"*.key", "server.pem", false},
		{"a?c.md", "abc.md", true},
		{"a?c.md", "ac.md", false},
		// `**` crosses segments, including zero of them.
		{"internal/**", "internal/cli/records.go", true},
		{"internal/**/x.md", "internal/x.md", true},
		{"internal/**/x.md", "internal/cli/deep/x.md", true},
		{"internal/**/x.md", "internal/cli/y.md", false},
		// A leading slash anchors; a bare name floats.
		{"/docs/*.md", "docs/a.md", true},
		{"/docs/*.md", "sub/docs/a.md", false},
		{"docs/*.md", "sub/docs/a.md", false},
		{"notes.md", "any/where/notes.md", true},
		// A trailing slash covers the subtree.
		{"docs/private/", "docs/private/x/y.md", true},
		{"docs/private/", "docs/private.md", false},
		// Literal metacharacters are not interpreted.
		{"a.b+c", "a.b+c", true},
		{"a.b+c", "axbxc", false},
	}
	for _, tc := range cases {
		if got := Match(tc.pattern, tc.path); got != tc.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestParseIgnoreFile(t *testing.T) {
	data := "# 注释\n\ninternal/tmp/**\n!internal/tmp/keep.go\n"
	file, problems := ParseIgnoreFile("ignore", []byte(data))
	if len(problems) != 1 || !strings.Contains(problems[0], "取反不受支持") {
		t.Fatalf("problems = %v", problems)
	}
	if file.Path != "ignore" || len(file.Patterns()) != 1 {
		t.Fatalf("file = %+v", file)
	}
	if !file.Match("internal/tmp/x.go") || !file.Match("internal/tmp/deep/y.go") || file.Match("internal/app.go") {
		t.Fatalf("matching wrong: %v", file.Patterns())
	}
	// Negation is documented as unsupported: the pattern is skipped and the
	// problem is reported — the negated path stays ignored rather than
	// silently half-honoured.
	if !file.Match("internal/tmp/keep.go") {
		t.Fatal("negated pattern was applied")
	}
}
