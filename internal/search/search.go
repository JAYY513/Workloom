// Package search scans .devsys/ text files for a keyword without any
// database or index (方案 §14.1, 实施计划 M1.6). The scan is a plain
// read-only walk; it skips storage-internal material (.cache/, local/) and
// never takes the project lock. File reads fan out across workers because a
// thousand-file project must answer in interactive time; results keep the
// deterministic order a single-threaded walk would produce.
package search

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

// Excluded directories are never scanned.
var excluded = map[string]bool{".cache": true, "local": true, "archive": true}

// Match is one located keyword hit.
type Match struct {
	// Path is relative to .devsys/, slash-separated.
	Path string `json:"path"`
	// Line is 1-based.
	Line int `json:"line"`
	// Text is the matched line, truncated to a bounded length.
	Text string `json:"text"`
}

// maxLineBytes bounds how much of a matched line is reported.
const maxLineBytes = 200

// maxHitsPerFile stops one huge file from flooding the result.
const maxHitsPerFile = 5

// maxWorkers bounds the read fan-out.
const maxWorkers = 16

// Search returns keyword hits across the project's managed state, ordered by
// path and then line. query is matched as a case-insensitive substring.
func Search(devsysDir, query string) ([]Match, int, error) {
	needle := strings.ToLower(query)
	if needle == "" {
		return nil, 0, nil
	}
	var files []string
	err := filepath.WalkDir(devsysDir, func(abs string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if abs != devsysDir && excluded[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			files = append(files, abs)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	sort.Strings(files)

	// Each worker scans whole files independently; per-file results land in
	// their slot so flattening stays deterministic.
	type result struct {
		matches []Match
		total   int
	}
	results := make([]result, len(files))
	var firstErr atomic.Pointer[error]
	workers := maxWorkers
	if workers > len(files) {
		workers = len(files)
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if firstErr.Load() != nil {
					return
				}
				i := int(next.Add(1)) - 1
				if i >= len(files) {
					return
				}
				ms, total, err := scanFile(files[i], needle)
				if err != nil {
					firstErr.CompareAndSwap(nil, &err)
					return
				}
				results[i] = result{matches: ms, total: total}
			}
		}()
	}
	wg.Wait()
	if err := firstErr.Load(); err != nil {
		return nil, 0, *err
	}

	var (
		matches []Match
		total   int
	)
	for i := range files {
		rel, err := filepath.Rel(devsysDir, files[i])
		if err != nil {
			return nil, 0, err
		}
		rel = filepath.ToSlash(rel)
		total += results[i].total
		for _, m := range results[i].matches {
			m.Path = rel
			matches = append(matches, m)
		}
	}
	return matches, total, nil
}

// scanFile reads one file and reports its keyword hits.
func scanFile(abs, needle string) ([]Match, int, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, 0, err
	}
	var (
		matches  []Match
		total    int
		hits     int
		line     = 1
		segStart = 0
	)
	for i := 0; i <= len(data); i++ {
		if i != len(data) && data[i] != '\n' {
			continue
		}
		seg := data[segStart:i]
		segStart = i + 1
		if len(seg) > 0 && seg[len(seg)-1] == '\r' {
			seg = seg[:len(seg)-1]
		}
		if strings.Contains(strings.ToLower(string(seg)), needle) {
			total++
			if hits < maxHitsPerFile {
				matches = append(matches, Match{Line: line, Text: truncate(string(seg))})
				hits++
			}
		}
		line++
	}
	return matches, total, nil
}

// truncate cuts a line to maxLineBytes without splitting a UTF-8 rune.
func truncate(line string) string {
	if len(line) <= maxLineBytes {
		return line
	}
	cut := maxLineBytes
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}
	return line[:cut] + "…"
}
