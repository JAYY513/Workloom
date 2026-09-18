package knowledge

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Match reports whether a slash-separated project-relative path matches a
// pattern. Patterns follow the ignore-file conventions the layer documents:
//
//   - `*` matches inside one path segment, `?` matches one character inside a
//     segment, `**` crosses segments;
//   - a pattern without `/` matches at any depth (`*.key` matches
//     `a/b/secret.key`);
//   - a pattern with `/` is anchored at the project root;
//   - a trailing `/` makes the pattern cover the directory's whole subtree.
//
// The same shape is used for the custom ignore file, for page `sources`
// patterns, and for the file index, so one implementation defines it.
func Match(pattern, path string) bool {
	return matchRegexp(pattern).MatchString(path)
}

// matchCacheSize bounds the compiled-pattern cache. Patterns come from
// project files (ignore lists, page sources) and the set is small in practice;
// the cap only exists so a caller that feeds unbounded patterns cannot grow a
// package-level map without limit.
const matchCacheSize = 4096

var (
	matchMu    sync.Mutex
	matchCache = map[string]*regexp.Regexp{}
)

func matchRegexp(pattern string) *regexp.Regexp {
	matchMu.Lock()
	defer matchMu.Unlock()
	if re, ok := matchCache[pattern]; ok {
		return re
	}
	re := regexp.MustCompile(compilePattern(pattern))
	if len(matchCache) >= matchCacheSize {
		matchCache = map[string]*regexp.Regexp{}
	}
	matchCache[pattern] = re
	return re
}

// compilePattern translates one pattern into an anchored regular expression.
func compilePattern(pattern string) string {
	p := strings.TrimSpace(pattern)
	dir := strings.HasSuffix(p, "/")
	p = strings.TrimSuffix(p, "/")
	rooted := strings.HasPrefix(p, "/")
	p = strings.TrimPrefix(p, "/")
	expr := patternToRegexp(p)
	if !rooted && !strings.Contains(p, "/") {
		// A bare name matches at any depth, as in .gitignore.
		expr = "(?:.*/)?" + expr
	}
	if dir {
		expr += "(?:/.*)?"
	}
	return "^" + expr + "$"
}

// patternToRegexp converts the wildcard part; path separators stay literal.
func patternToRegexp(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				// `**/` spans zero or more complete segments, so `internal/**/x`
				// matches `internal/x` too; a trailing `**` spans anything.
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:[^/]*/)*")
				} else {
					b.WriteString(".*")
				}
				continue
			}
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '[', ']':
			// Character classes are not part of the contract; treat literally.
			b.WriteString(regexp.QuoteMeta(string(c)))
		default:
			if strings.ContainsRune(`.+()|{}^$\\`, rune(c)) {
				b.WriteByte('\\')
			}
			b.WriteByte(c)
		}
	}
	return b.String()
}

// IgnoreFile holds the compiled patterns of one ignore file.
type IgnoreFile struct {
	// Path is where the file came from, relative to the project root; empty
	// for an inline rule set.
	Path     string
	patterns []string
}

// ParseIgnoreFile reads a custom ignore file: one pattern per line, `#`
// comments and blank lines skipped. Negation is not supported — an ignore
// file that cannot express `!` is easier to reason about, and the project's
// `.gitignore` already covers the cases that need it.
func ParseIgnoreFile(rel string, data []byte) (*IgnoreFile, []string) {
	var patterns []string
	var problems []string
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "!") {
			problems = append(problems, rel+":"+strconv.Itoa(i+1)+": `!` 取反不受支持，已忽略")
			continue
		}
		patterns = append(patterns, line)
	}
	return &IgnoreFile{Path: rel, patterns: patterns}, problems
}

// Match reports whether the path is ignored by this file.
func (f *IgnoreFile) Match(path string) bool {
	if f == nil {
		return false
	}
	for _, pattern := range f.patterns {
		if Match(pattern, path) {
			return true
		}
	}
	return false
}

// Patterns returns the patterns in file order.
func (f *IgnoreFile) Patterns() []string {
	if f == nil {
		return nil
	}
	out := make([]string, len(f.patterns))
	copy(out, f.patterns)
	return out
}
