package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"workloom/internal/storage"
)

// ErrNoGit reports that .gitignore matching could not be applied because git
// is unavailable or failed. 方案 §12.5 forbids degrading to "no ignores":
// silently indexing files the project excluded would make every downstream
// claim about coverage untrue.
var ErrNoGit = errors.New("git is required to apply .gitignore")

// DefaultMaxFileSize is the index's size ceiling: a file larger than this is
// listed as excluded rather than hashed (方案 §12.2 keeps the index to what a
// reader or an agent could actually load).
const DefaultMaxFileSize = 1 << 20

// DefaultExcludedDirectories are directory names the index never descends
// into: version control, the layer's own state, tooling state, and vendored
// dependencies.
var DefaultExcludedDirectories = []string{".git", ".devsys", ".repowiki", "vendor", "node_modules"}

// SecretPatterns are the name patterns whose content is never fingerprinted.
// The list is deliberately conservative: a false positive hides a file that a
// human can whitelist by renaming, while a false negative would put a
// credential into a file that agents are encouraged to read.
var SecretPatterns = []string{
	".env", ".env.*", "*.env",
	"*.pem", "*.key", "*.p12", "*.pfx", "*.keystore", "*.jks",
	"id_rsa*", "id_dsa*", "id_ecdsa*", "id_ed25519*",
	"credentials", "credentials.*", "secrets", "secrets.*",
	"*.npmrc", ".netrc", ".htpasswd",
}

// FileEntry is one fingerprinted file.
type FileEntry struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Hash     string `json:"hash"`
	Language string `json:"language"`
}

// Exclusion names one path the scan left out and why.
type Exclusion struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// SnapshotStats summarizes a scan.
type SnapshotStats struct {
	TotalFiles int            `json:"total_files"`
	TotalSize  int64          `json:"total_size"`
	Languages  map[string]int `json:"languages"`
}

// SnapshotExcluded records what was left out; the reasons are part of the
// snapshot because "the index is smaller than the tree" must always be
// explainable (实施计划 M5.2 acceptance).
type SnapshotExcluded struct {
	Directories []string    `json:"directories"`
	Files       []Exclusion `json:"files"`
	IgnoreFiles []string    `json:"ignore_files"`
	Rules       []string    `json:"rules"`
}

// Snapshot is the index layer's fingerprint of the working tree
// (.devsys/knowledge/snapshot.json). It is derived state: deleting it costs
// one rescan.
type Snapshot struct {
	SchemaVersion int              `json:"schema_version"`
	GeneratedAt   time.Time        `json:"generated_at"`
	Files         []FileEntry      `json:"files"`
	Stats         SnapshotStats    `json:"stats"`
	Excluded      SnapshotExcluded `json:"excluded"`
}

// SnapshotSchemaVersion is the only snapshot version this build writes.
const SnapshotSchemaVersion = 1

// CustomIgnoreFile is the project's own ignore list for the index.
const CustomIgnoreFile = Dir + "/ignore"

// ScanOptions configures a scan.
type ScanOptions struct {
	// MaxFileSize overrides DefaultMaxFileSize when positive.
	MaxFileSize int64
	// Now overrides the clock (tests).
	Now func() time.Time
	// SkipGitIgnore leaves .gitignore matching to the caller: without a git
	// repository the scan cannot honour it, and §12.5 forbids pretending
	// otherwise, so the caller decides whether that is an error.
	SkipGitIgnore bool
}

// BuildSnapshot walks the project and fingerprints every file that survives
// the exclusion rules. Errors are explicit: a missing git repository or a
// failing git command is reported, never silently treated as "no ignores".
func BuildSnapshot(root string, opts ScanOptions) (*Snapshot, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	maxSize := opts.MaxFileSize
	if maxSize <= 0 {
		maxSize = DefaultMaxFileSize
	}
	ignore, ignoreProblems := loadIgnoreFile(root)
	excluded := SnapshotExcluded{
		Directories: append([]string{}, DefaultExcludedDirectories...),
		Rules: []string{
			"目录：" + strings.Join(DefaultExcludedDirectories, "、"),
			"密钥名模式：" + strings.Join(SecretPatterns, "、"),
			fmt.Sprintf("单文件 > %d 字节", maxSize),
			".gitignore（git check-ignore；只对未跟踪文件生效，已提交的文件即使命中忽略规则也会入索引）",
		},
	}
	if ignore != nil {
		excluded.IgnoreFiles = append(excluded.IgnoreFiles, ignore.Path)
		excluded.Rules = append(excluded.Rules, "自定义忽略文件："+ignore.Path)
	}
	for _, problem := range ignoreProblems {
		excluded.Rules = append(excluded.Rules, "警告："+problem)
	}

	candidates, dirExclusions, err := collectCandidates(root)
	if err != nil {
		return nil, err
	}
	excluded.Files = append(excluded.Files, dirExclusions...)

	ignored, err := gitIgnored(root, candidates, opts.SkipGitIgnore)
	if err != nil {
		return nil, err
	}

	snapshot := &Snapshot{
		SchemaVersion: SnapshotSchemaVersion,
		GeneratedAt:   now().UTC(),
		Files:         []FileEntry{},
		Stats:         SnapshotStats{Languages: map[string]int{}},
		Excluded:      excluded,
	}
	for _, rel := range candidates {
		reason := ""
		switch {
		// A credential is the reason worth naming, even when the path is also
		// ignored: an audit of secret coverage must not be hidden by "it was
		// in .gitignore anyway".
		case matchesAny(SecretPatterns, rel):
			reason = secretReason(ignored[rel], ignore.Match(rel))
		case ignored[rel]:
			reason = ".gitignore"
		case ignore.Match(rel):
			reason = "自定义忽略文件 " + ignore.Path
		default:
			entry, skip, err := scanFile(root, rel, maxSize)
			if err != nil {
				return nil, err
			}
			if skip != "" {
				reason = skip
				break
			}
			snapshot.Files = append(snapshot.Files, entry)
			snapshot.Stats.TotalFiles++
			snapshot.Stats.TotalSize += entry.Size
			snapshot.Stats.Languages[entry.Language]++
		}
		if reason != "" {
			snapshot.Excluded.Files = append(snapshot.Excluded.Files, Exclusion{Path: rel, Reason: reason})
		}
	}
	// Both lists are ordered explicitly: the snapshot's usefulness in diffs and
	// tests rests on it, so it must not depend on walk order.
	sort.Slice(snapshot.Files, func(i, j int) bool { return snapshot.Files[i].Path < snapshot.Files[j].Path })
	sort.Slice(snapshot.Excluded.Files, func(i, j int) bool {
		return snapshot.Excluded.Files[i].Path < snapshot.Excluded.Files[j].Path
	})
	return snapshot, nil
}

// secretReason names the credential rule, noting an overlapping ignore rule.
func secretReason(ignored, customIgnored bool) string {
	switch {
	case ignored:
		return "密钥名模式（同时被 .gitignore 排除）"
	case customIgnored:
		return "密钥名模式（同时被自定义忽略文件排除）"
	default:
		return "密钥名模式"
	}
}

// scanFile fingerprints one candidate. A path that vanished between the walk
// and this read is reported as a skip, not as a failure: a scan runs against a
// live tree, and one racing deletion must not cost the whole snapshot.
func scanFile(root, rel string, maxSize int64) (FileEntry, string, error) {
	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return FileEntry{}, "扫描期间被删除", nil
	case err != nil:
		return FileEntry{}, "", fmt.Errorf("stat %s: %w", rel, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return FileEntry{}, "符号链接", nil
	}
	if info.Size() > maxSize {
		return FileEntry{}, fmt.Sprintf("超过 %d 字节上限", maxSize), nil
	}
	entry, err := fingerprint(root, rel)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return FileEntry{}, "扫描期间被删除", nil
	case err != nil:
		return FileEntry{}, "", err
	}
	return entry, "", nil
}

// WriteSnapshot stores the snapshot atomically at .devsys/knowledge/
// snapshot.json.
func WriteSnapshot(root string, snapshot *Snapshot) (string, error) {
	rel := SnapshotFile
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode snapshot: %w", err)
	}
	data = append(data, '\n')
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := storage.AtomicWrite(abs, data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", rel, err)
	}
	return rel, nil
}

// LoadSnapshot reads a snapshot, refusing a version this build does not know.
func LoadSnapshot(root string) (*Snapshot, error) {
	abs := filepath.Join(root, filepath.FromSlash(SnapshotFile))
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("%s: %w", SnapshotFile, err)
	}
	if snapshot.SchemaVersion != SnapshotSchemaVersion {
		return nil, fmt.Errorf("%s: schema_version %d is not supported (this build writes %d)",
			SnapshotFile, snapshot.SchemaVersion, SnapshotSchemaVersion)
	}
	return &snapshot, nil
}

// loadIgnoreFile reads the optional custom ignore file. A missing file is not
// an error: most projects rely on .gitignore alone.
func loadIgnoreFile(root string) (*IgnoreFile, []string) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(CustomIgnoreFile)))
	if err != nil {
		return nil, nil
	}
	return ParseIgnoreFile(CustomIgnoreFile, data)
}

// collectCandidates lists the files the scan will consider, sorted, plus the
// exclusions the walk itself decided (directories it did not descend into).
func collectCandidates(root string) ([]string, []Exclusion, error) {
	var candidates []string
	var exclusions []Exclusion
	err := filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if containsFold(DefaultExcludedDirectories, name) {
				exclusions = append(exclusions, Exclusion{Path: rel + "/", Reason: "默认排除目录"})
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() && entry.Type()&os.ModeSymlink == 0 {
			exclusions = append(exclusions, Exclusion{Path: rel, Reason: "非常规文件"})
			return nil
		}
		candidates = append(candidates, rel)
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("scan %s: %w", root, err)
	}
	sort.Strings(candidates)
	return candidates, exclusions, nil
}

// gitIgnored asks git which candidates are ignored. Git owns the semantics —
// re-implementing .gitignore would be a second, subtly different truth. A
// missing repository or a failing git is an error: §12.5 forbids degrading to
// "no ignores" silently.
func gitIgnored(root string, candidates []string, skip bool) (map[string]bool, error) {
	ignored := map[string]bool{}
	if skip || len(candidates) == 0 {
		return ignored, nil
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoGit, err)
	}
	cmd := exec.Command("git", "-C", root, "check-ignore", "--stdin", "-z")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("git check-ignore: %w", err)
	}
	var out strings.Builder
	cmd.Stdout = &out
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git check-ignore: %w", err)
	}
	for _, rel := range candidates {
		if _, err := io.WriteString(stdin, rel+"\x00"); err != nil {
			_ = stdin.Close()
			_ = cmd.Wait()
			return nil, fmt.Errorf("git check-ignore: %w", err)
		}
	}
	if err := stdin.Close(); err != nil {
		_ = cmd.Wait()
		return nil, fmt.Errorf("git check-ignore: %w", err)
	}
	err = cmd.Wait()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return nil, fmt.Errorf("%w: %v: %s", ErrNoGit, err, strings.TrimSpace(stderr.String()))
		}
	}
	for _, line := range strings.Split(out.String(), "\x00") {
		if line != "" {
			ignored[filepath.ToSlash(line)] = true
		}
	}
	return ignored, nil
}

// fingerprint hashes one file's bytes and classifies its language. The size it
// reports is the number of bytes it hashed, so size and hash always describe
// the same content even if the file changes while the scan runs.
func fingerprint(root, rel string) (FileEntry, error) {
	abs := filepath.Join(root, filepath.FromSlash(rel))
	file, err := os.Open(abs)
	if err != nil {
		return FileEntry{}, fmt.Errorf("open %s: %w", rel, err)
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return FileEntry{}, fmt.Errorf("hash %s: %w", rel, err)
	}
	return FileEntry{
		Path:     rel,
		Size:     size,
		Hash:     hex.EncodeToString(hash.Sum(nil)),
		Language: languageOf(rel),
	}, nil
}

// languageOf classifies by extension, using the same vocabulary the layer's
// reference generator uses so snapshots stay comparable.
func languageOf(rel string) string {
	base := path.Base(rel)
	switch base {
	case "Makefile", "Dockerfile", "LICENSE", "NOTICE":
		return "other"
	case "go.mod", "go.sum":
		return "go"
	}
	switch strings.ToLower(path.Ext(base)) {
	case ".go":
		return "go"
	case ".md", ".markdown":
		return "markdown"
	case ".yaml", ".yml":
		return "yaml"
	case ".json", ".jsonl":
		return "json"
	case ".sh", ".bash":
		return "shell"
	case ".ps1", ".psm1":
		return "powershell"
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".html", ".css":
		return "web"
	case ".sql":
		return "sql"
	case ".toml":
		return "toml"
	case ".proto":
		return "proto"
	default:
		return "other"
	}
}

// containsFold is the case-insensitive membership test the directory rules
// need: on a case-insensitive filesystem `.Git` and `.git` are one directory,
// and walking into a hundred thousand vendored files because of a capital
// letter is not a defensible outcome.
func containsFold(list []string, want string) bool {
	for _, item := range list {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}

func matchesAny(patterns []string, rel string) bool {
	for _, pattern := range patterns {
		if Match(pattern, rel) {
			return true
		}
	}
	return false
}
