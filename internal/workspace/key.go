package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	// keyMax bounds a workspace directory name. Kept well below the 255-byte
	// filesystem limit so a nested workspace path stays usable on Windows.
	keyMax = 64
	// keyHash is how many hex characters of the identifier hash a
	// disambiguated key carries: 16 hex = 64 bits of entropy,
	// the floor kept for collision resistance.
	keyHash = 16
	// keySeparator joins the sanitized identifier and its hash suffix.
	keySeparator = "--"
)

// reservedKeyNames are names a directory may not carry: the Windows device
// names (reserved with and without an extension) and ".git", which would make
// the workspace itself look like repository metadata.
var reservedKeyNames = func() map[string]bool {
	names := []string{"con", "prn", "aux", "nul", ".git"}
	for i := 1; i <= 9; i++ {
		names = append(names, "com"+string(rune('0'+i)), "lpt"+string(rune('0'+i)))
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}()

// Key derives the workspace directory name for an identifier (方案 §4.8).
//
// Every character outside [A-Za-z0-9._-] becomes '_'. When sanitization
// changes the identifier — or when the result would not be a usable directory
// name ("", ".", "..", a trailing dot, a reserved device name, ".git",
// longer than keyMax) — a stable hash of the original identifier is appended,
// so identifiers that sanitize to the same text keep distinct keys.
// Identifiers that survive sanitization unchanged keep their deterministic
// key, with no hash.
func Key(identifier string) string {
	sanitized := sanitize(identifier)
	if sanitized == identifier && usableKey(sanitized) {
		return sanitized
	}
	return disambiguated(sanitized, identifier)
}

// sanitize maps every rune outside the allowed set to '_' (one '_' per rune,
// so multi-byte characters do not multiply).
func sanitize(identifier string) string {
	var b strings.Builder
	b.Grow(len(identifier))
	for _, r := range identifier {
		if allowedRune(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func allowedRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '.', r == '_', r == '-':
		return true
	}
	return false
}

// usableKey reports whether s can name a workspace directory as it stands.
func usableKey(s string) bool {
	if s == "" || s == "." || s == ".." || s != strings.TrimRight(s, ".") || len(s) > keyMax {
		return false
	}
	// Windows reserves the device name with and without an extension, so
	// "con.txt" is as reserved as "con" is.
	lowered := strings.ToLower(s)
	if reservedKeyNames[lowered] {
		return false
	}
	if i := strings.IndexByte(lowered, '.'); i >= 0 {
		return !reservedKeyNames[lowered[:i]]
	}
	return true
}

// disambiguated renders the key for an identifier whose sanitized form is not
// usable on its own: the sanitized text (trimmed to fit) plus a hash of the
// original identifier.
func disambiguated(sanitized, identifier string) string {
	sum := sha256.Sum256([]byte(identifier))
	tail := keySeparator + hex.EncodeToString(sum[:keyHash/2])
	main := strings.TrimRight(sanitized, ".")
	if max := keyMax - len(tail); len(main) > max {
		main = strings.TrimRight(main[:max], ".")
	}
	if main == "" || main == "." || main == ".." {
		main = "ws"
	}
	return main + tail
}
