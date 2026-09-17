package project

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Slug derives a devsys project identifier from a directory name: lowercase,
// [a-z0-9-] only, runs of other characters collapse to a single '-', leading
// and trailing '-' trimmed. Names that contain no usable characters (for
// example a fully non-ASCII directory name) fall back to a deterministic
// "project-<hash>" form so identifiers stay stable across machines.
func Slug(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	if s := strings.Trim(b.String(), "-"); s != "" {
		return s
	}
	sum := sha256.Sum256([]byte(name))
	return fmt.Sprintf("project-%x", sum[:4])
}
