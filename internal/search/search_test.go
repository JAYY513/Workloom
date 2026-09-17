package search

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSearchFindsAndExcludes(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("workitems/WLM-1.yaml", "id: WLM-1\ntitle: 密码协议\n")
	write("workitems/WLM-2.yaml", "id: WLM-2\ntitle: 无关任务\n")
	write("events/2026-09.jsonl", "{\"content\":\"提到 密码 的评论\"}\n")
	write("local/lock", "password=secret")
	write(".cache/snapshot.json", "cached password")
	write("specs/deep/note.md", strings.Repeat("filler\n", 40)+"密码 in the middle\n")

	matches, total, err := Search(dir, "密码")
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total = %d (%+v)", total, matches)
	}
	var paths []string
	for _, m := range matches {
		paths = append(paths, m.Path)
		if strings.Contains(m.Path, "local/") || strings.Contains(m.Path, ".cache/") {
			t.Fatalf("scanned excluded path: %s", m.Path)
		}
	}
	want := []string{"events/2026-09.jsonl", "specs/deep/note.md", "workitems/WLM-1.yaml"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v", paths)
	}
	// The long file's hit is reported with its real line number (41).
	if matches[1].Line != 41 {
		t.Fatalf("line = %d", matches[1].Line)
	}
}

func TestSearchEmptyQueryAndCaseFolding(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("Mixed CASE value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, total, err := Search(dir, ""); err != nil || total != 0 {
		t.Fatalf("empty query = %d, %v", total, err)
	}
	matches, total, err := Search(dir, "mixed case")
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || matches[0].Line != 1 {
		t.Fatalf("case-insensitive hit = %+v", matches)
	}
}

func TestSearchTruncatesLongLines(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("字", 300) + "needle" + strings.Repeat("字", 300)
	if err := os.WriteFile(filepath.Join(dir, "long.yaml"), []byte(long+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	matches, total, err := Search(dir, "needle")
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("total = %d", total)
	}
	if len(matches[0].Text) > 201 || !strings.HasSuffix(matches[0].Text, "…") {
		t.Fatalf("text length = %d, tail = %q", len(matches[0].Text), tail(matches[0].Text))
	}
	if !utf8.ValidString(matches[0].Text) {
		t.Fatal("truncated text is not valid UTF-8")
	}
}

func tail(s string) string {
	if len(s) > 8 {
		return s[len(s)-8:]
	}
	return s
}

func TestSearchThousandFilesResponsive(t *testing.T) {
	dir := thousandSearchFiles(t)
	start := time.Now()
	matches, total, err := Search(dir, "needle")
	elapsed := time.Since(start)
	checkThousandSearch(t, matches, total, err)
	// Elapsed time is diagnostic only; host load and filesystem behavior
	// are not correctness contracts.
	t.Logf("1000 files scanned in %v", elapsed)
}

// FreshFiles measures the first Search after creating each fixture; it does
// not flush OS caches. RepeatedFiles reuses a fixture after one untimed scan.
// Setup, result validation, and cleanup are excluded from benchmark timing.
func BenchmarkSearchThousandFiles(b *testing.B) {
	b.Run("FreshFiles", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			dir := thousandSearchFiles(b)
			b.StartTimer()
			matches, total, err := Search(dir, "needle")
			b.StopTimer()
			checkThousandSearch(b, matches, total, err)
			if err := os.RemoveAll(dir); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("RepeatedFiles", func(b *testing.B) {
		dir := thousandSearchFiles(b)
		matches, total, err := Search(dir, "needle")
		checkThousandSearch(b, matches, total, err)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			matches, total, err := Search(dir, "needle")
			b.StopTimer()
			checkThousandSearch(b, matches, total, err)
			b.StartTimer()
		}
	})
}

func thousandSearchFiles(tb testing.TB) string {
	tb.Helper()
	dir := tb.TempDir()
	for i := 0; i < 1000; i++ {
		rel := filepath.Join(dir, "bulk", strings.Repeat("d", 1+i%3), "f"+string(rune('a'+i%26))+itoa(i)+".yaml")
		if err := os.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
			tb.Fatal(err)
		}
		content := "id: item-" + itoa(i) + "\nstatus: planned\n"
		if i == 500 {
			content += "title: 藏在千文件里的needle\n"
		}
		if err := os.WriteFile(rel, []byte(content), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	return dir
}

func checkThousandSearch(tb testing.TB, matches []Match, total int, err error) {
	tb.Helper()
	if err != nil {
		tb.Fatal(err)
	}
	if total != 1 || len(matches) != 1 {
		tb.Fatalf("hits = %d/%d", total, len(matches))
	}
	want := Match{Path: "bulk/ddd/fg500.yaml", Line: 3, Text: "title: 藏在千文件里的needle"}
	if matches[0] != want {
		tb.Fatalf("match = %+v, want %+v", matches[0], want)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
