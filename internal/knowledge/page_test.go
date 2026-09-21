package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JAYY513/Workloom/internal/config"
)

// page assembles a page with the given front matter lines and body.
func page(front string, body string) string {
	return "---\n" + front + "\n---\n" + body
}

const goodFront = "status: stable\ntype: module\ntriggers:\n  - 存储层\n  - storage\ndescription: 可靠文本存储的定位与边界\nsource_commit: 997c5f8\nsources:\n  - internal/storage/**"

// want is one expected located problem.
type want struct {
	severity string
	line     int
	field    string
	contains string
}

func check(t *testing.T, problems []config.Problem, wants []want) {
	t.Helper()
	for _, w := range wants {
		found := false
		for _, p := range problems {
			if p.Severity == w.severity && p.Field == w.field && strings.Contains(p.Reason, w.contains) {
				if w.line > 0 && p.Line != w.line {
					continue
				}
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing problem (severity=%q field=%q line=%d contains=%q) in %v", w.severity, w.field, w.line, w.contains, problems)
		}
	}
}

func TestParseValidPage(t *testing.T) {
	p, problems := Parse("docs/repowiki/knowledge/存储/概述.md", []byte(page(goodFront, "\n# 存储层\n\n正文。\n")))
	if len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
	if p.Status != "stable" || p.Type != "module" || p.SourceCommit != "997c5f8" {
		t.Fatalf("page = %+v", p)
	}
	if len(p.Triggers) != 2 || len(p.Sources) != 1 {
		t.Fatalf("triggers = %v sources = %v", p.Triggers, p.Sources)
	}
	if p.BodyLine != 12 || !strings.HasPrefix(p.Body, "\n# 存储层") {
		t.Fatalf("body starts at line %d: %q", p.BodyLine, p.Body)
	}
	if p.Hash() == "" {
		t.Fatal("hash is empty")
	}
}

// A page without triggers would never be loaded by trigger match, and the plan
// makes that an error rather than a silent no-op (M5.1 acceptance).
func TestParseRejectsMissingTriggers(t *testing.T) {
	front := "status: current\ntype: note\ndescription: 没有触发词\nsource_commit: abc1234"
	_, problems := Parse("x.md", []byte(page(front, "\n# 标题\n")))
	check(t, problems, []want{{line: 1, field: "triggers", contains: "缺少必填字段"}})
}

func TestParseRejectsEmptyTriggers(t *testing.T) {
	front := strings.Replace(goodFront, "triggers:\n  - 存储层\n  - storage", "triggers: []", 1)
	_, problems := Parse("x.md", []byte(page(front, "\n# 标题\n")))
	check(t, problems, []want{{field: "triggers", contains: "非空列表"}})
}

func TestParseFrontMatterFailures(t *testing.T) {
	cases := []struct {
		name string
		data string
		want want
	}{
		{"no-front-matter", "# 标题\n", want{line: 1, contains: "缺少 front matter"}},
		{"unclosed", "---\n" + goodFront + "\n# 标题\n", want{line: 1, contains: "未闭合"}},
		{"not-a-mapping", "---\n- a\n- b\n---\n# 标题\n", want{line: 2, contains: "键值映射"}},
		{"empty", "---\n---\n# 标题\n", want{line: 1, contains: "front matter 为空"}},
		// yaml.v3 does not always report a line; the problem then belongs to
		// the front matter opener.
		{"bad-yaml-unlocated", "---\ndescription: `x` 的契约\nstatus: current\n---\n# 标题\n", want{line: 1, contains: "不是合法 YAML"}},
		// When it does, the reported line is the file line of the offending
		// node (block line 1 is file line 2).
		{"bad-yaml-located", "---\ndescription: [a, b\nstatus: current\n---\n# 标题\n", want{line: 2, contains: "line 1: did not find expected"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, problems := Parse("x.md", []byte(tc.data))
			check(t, problems, []want{tc.want})
		})
	}
}

func TestParseValueProblems(t *testing.T) {
	front := strings.Join([]string{
		"status: stale",
		"type: Module",
		"dimension: overview",
		"triggers:",
		"  - 存储层",
		"  - 存储层",
		"  - ' 带空白'",
		"description: \"\"",
		"source_commit: HEAD",
		"sources:",
		"  - /etc/passwd",
		"  - internal\\storage",
		"  - ../outside",
		"  - internal//storage",
		"protected: \"yes\"",
		"content_hash: abc",
		"extra_key: 1",
	}, "\n")
	_, problems := Parse("x.md", []byte(page(front, "\n# 标题\n")))
	check(t, problems, []want{
		{line: 2, field: "status", contains: "计算得出的状态"},
		{line: 3, field: "type", contains: "小写标记"},
		{field: "triggers", severity: config.SeverityWarning, contains: "重复"},
		{field: "triggers", severity: config.SeverityWarning, contains: "首尾空白"},
		{line: 9, field: "description", contains: "不能为空"},
		{line: 10, field: "source_commit", contains: "十六进制"},
		{line: 12, field: "sources", contains: "不能以 / 开头"},
		{line: 13, field: "sources", contains: "必须用 / 分隔"},
		{line: 14, field: "sources", contains: "不能包含 .. 段"},
		{line: 15, field: "sources", contains: "空路径段"},
		{line: 16, field: "protected", contains: "布尔值"},
		{line: 17, field: "content_hash", contains: "sha256"},
		{line: 18, field: "extra_key", severity: config.SeverityWarning, contains: "未知字段"},
	})
}

// The unknown-status case is separate: `stale` is rejected by name, any other
// unknown value by the allowlist.
func TestParseUnknownStatus(t *testing.T) {
	front := strings.Replace(goodFront, "status: stable", "status: fresh", 1)
	_, problems := Parse("x.md", []byte(page(front, "\n# 标题\n")))
	check(t, problems, []want{{line: 2, field: "status", contains: "未知状态"}})
}

func TestParseDuplicateKey(t *testing.T) {
	front := goodFront + "\ndescription: 第二次描述"
	_, problems := Parse("x.md", []byte(page(front, "\n# 标题\n")))
	check(t, problems, []want{{line: 11, field: "description", contains: "重复的键"}})
}

func TestParseBodyProblems(t *testing.T) {
	_, problems := Parse("x.md", []byte(page(goodFront, "")))
	check(t, problems, []want{{field: "", contains: "正文为空"}})

	_, problems = Parse("x.md", []byte(page(goodFront, "没有标题的正文\n")))
	check(t, problems, []want{{severity: config.SeverityWarning, contains: "一级标题"}})
}

func TestParseMissingSourcesIsAdvisory(t *testing.T) {
	front := "status: current\ntype: note\ntriggers:\n  - 说明\ndescription: 只有页面内声明的字段\nsource_commit: abc1234"
	_, problems := Parse("x.md", []byte(page(front, "\n# 标题\n")))
	check(t, problems, []want{{field: "sources", severity: config.SeverityWarning, contains: "state 映射"}})
}

func TestParseEmptySourceCommit(t *testing.T) {
	front := strings.Replace(goodFront, "source_commit: 997c5f8", "source_commit: \"\"", 1)
	_, problems := Parse("x.md", []byte(page(front, "\n# 标题\n")))
	check(t, problems, []want{{field: "source_commit", severity: config.SeverityWarning, contains: "未基线"}})
}

func TestParseProtectedAndGenerated(t *testing.T) {
	front := goodFront + "\nprotected: true\ngenerated: true\ngenerator: repowiki-gen\ncontent_hash: " + strings.Repeat("ab", 32)
	p, problems := Parse("x.md", []byte(page(front, "\n# 标题\n")))
	if len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
	if !p.Protected || !p.Generated || p.Generator != "repowiki-gen" {
		t.Fatalf("page = %+v", p)
	}
}

// Windows-written pages arrive with CRLF and sometimes a BOM; both must parse,
// and the BOM is reported so it gets fixed.
func TestParseCRLFAndBOM(t *testing.T) {
	data := "\ufeff" + strings.ReplaceAll(page(goodFront, "\n# 标题\n"), "\n", "\r\n")
	p, problems := Parse("x.md", []byte(data))
	if p.Status != "stable" || len(p.Triggers) != 2 {
		t.Fatalf("page = %+v (problems %v)", p, problems)
	}
	check(t, problems, []want{{severity: config.SeverityWarning, contains: "BOM"}})
}

func TestParseInvalidUTF8(t *testing.T) {
	_, problems := Parse("x.md", []byte{0xff, 0xfe, 0x00})
	check(t, problems, []want{{contains: "UTF-8"}})
}

// Scan walks directories deterministically, skips dot directories and
// non-Markdown files, and reports a missing page layer instead of failing.
func TestScan(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("pages/b.md", page(goodFront, "\n# B\n"))
	write("pages/a.md", page(goodFront, "\n# A\n"))
	write("pages/notes.txt", "not a page")
	write("pages/.hidden/c.md", page(goodFront, "\n# C\n"))
	write("pages/deep/d.md", page("status: current", "\n# D\n"))

	report, err := Scan(root, []string{"pages"})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, p := range report.Pages {
		paths = append(paths, p.Path)
	}
	wantPaths := []string{"pages/a.md", "pages/b.md", "pages/deep/d.md"}
	if strings.Join(paths, ",") != strings.Join(wantPaths, ",") {
		t.Fatalf("paths = %v, want %v", paths, wantPaths)
	}
	if report.Errors() != 4 {
		t.Fatalf("errors = %d, problems = %v", report.Errors(), report.Problems)
	}
	if report.Warnings() != 1 {
		t.Fatalf("warnings = %d, problems = %v", report.Warnings(), report.Problems)
	}
	if !strings.Contains(report.Problems[0].String(), "type") {
		t.Fatalf("first problem = %v", report.Problems[0])
	}

	report, err = Scan(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Missing || len(report.Pages) != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestExistingRoots(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notadir"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ExistingRoots(root, []string{"missing", "notadir", "a"})
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("roots = %v", got)
	}
}
