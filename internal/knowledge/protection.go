package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// HashFile returns the content hash the layer records and compares: the sha256
// of the file's bytes, lowercase hex. This is deliberately the whole file,
// front matter included, and the reference generator records the same value —
// a bundle's recorded hashes can therefore be compared as they are.
func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// HashBytes hashes content the caller already holds.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Skip is one page a refresh leaves alone, and why (方案 §12.5).
type Skip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
	// Recorded is the hash the generator wrote down; Actual is what is on disk
	// now. They differ exactly when a page was edited by hand.
	Recorded string `json:"recorded_hash,omitempty"`
	Actual   string `json:"actual_hash,omitempty"`
	// Overridden is true when --force regenerated the page anyway: the skip is
	// then a report, not an outcome.
	Overridden bool `json:"overridden,omitempty"`
}

// Skips lists the pages a refresh must not regenerate:
//
//   - `protected: true` is a permanent lock;
//   - a page whose recorded `content_hash` differs from the file on disk was
//     edited by hand — its content is not the generator's to replace.
//
// Both are overridden only by `--force`, and an override is still reported.
// A page with no recorded hash is not a skip: nothing was recorded, so there is
// nothing to compare and no claim to protect.
func Skips(root string, pages []*Page, state *State, force bool) ([]Skip, error) {
	var skips []Skip
	for _, page := range pages {
		recorded := page.ContentHash
		if mapped, ok := state.Mapping(page.Path); ok && recorded == "" {
			recorded = mapped.ContentHash
		}
		actual := ""
		var err error
		if recorded != "" {
			// Only hash when there is something to compare against: hashing
			// every page of a large bundle to discard the result would be work
			// nobody asked for.
			actual, err = HashFile(filepath.Join(root, filepath.FromSlash(page.Path)))
			if err != nil {
				return nil, fmt.Errorf("hash %s: %w", page.Path, err)
			}
		}
		var reason string
		switch {
		case page.Protected:
			reason = "protected: true：页面被锁定，不重新生成"
		case recorded != "" && recorded != actual:
			reason = "手工修改：内容哈希与记录不一致，不覆盖"
		default:
			continue
		}
		skips = append(skips, Skip{
			Path:       page.Path,
			Reason:     reason,
			Recorded:   recorded,
			Actual:     actual,
			Overridden: force,
		})
	}
	return skips, nil
}
