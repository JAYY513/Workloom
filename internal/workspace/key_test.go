package workspace

import (
	"regexp"
	"strings"
	"testing"
)

var hashSuffix = regexp.MustCompile(`--[0-9a-f]{16}$`)

func TestKeyKeepsSanitizedIdentifiers(t *testing.T) {
	for _, id := range []string{"WLM-142", "a.b_c-d", "9", "x", strings.Repeat("a", keyMax)} {
		if got := Key(id); got != id {
			t.Errorf("Key(%q) = %q, want it unchanged", id, got)
		}
	}
}

func TestKeyDisambiguatesChangedIdentifiers(t *testing.T) {
	cases := map[string]string{
		"WLM/142":   "WLM_142--",
		"任务-1":      "__-1--",
		"two words": "two_words--",
		"foo.":      "foo--",
		".":         "ws--",
		"..":        "ws--",
		"":          "ws--",
		".git":      ".git--",
		"CON":       "CON--",
		"con.txt":   "con.txt--",
		"a\nb":      "a_b--",
	}
	for id, wantPrefix := range cases {
		got := Key(id)
		if !strings.HasPrefix(got, wantPrefix) || !hashSuffix.MatchString(got) {
			t.Errorf("Key(%q) = %q, want prefix %q with a 16-hex hash suffix", id, got, wantPrefix)
		}
	}
}

// Two identifiers that sanitize to the same text must not share a key: that
// is the collision resistance §9.5 Invariant 3 requires.
func TestKeyDistinguishesSanitizationCollisions(t *testing.T) {
	pairs := [][2]string{
		{"WLM/142", "WLM_142"},
		{"a.b", "a_b"},
		{"a b", "a-b"},
	}
	for _, p := range pairs {
		if Key(p[0]) == Key(p[1]) {
			t.Errorf("Key(%q) == Key(%q) = %q, want distinct keys", p[0], p[1], Key(p[0]))
		}
	}
}

func TestKeyIsStableAndBounded(t *testing.T) {
	long := strings.Repeat("x", 300)
	key := Key(long)
	if key == long {
		t.Fatalf("Key(long) = the identifier itself, want a bounded key")
	}
	if len(key) > keyMax {
		t.Errorf("len(Key(long)) = %d, want <= %d", len(key), keyMax)
	}
	if !hashSuffix.MatchString(key) {
		t.Errorf("Key(long) = %q, want a hash suffix (truncation can collide)", key)
	}
	if again := Key(long); again != key {
		t.Errorf("Key is not deterministic: %q then %q", key, again)
	}
	if mixed := Key(strings.ToUpper(long)); mixed == key {
		t.Errorf("Key(%q) collides with Key(%q)", strings.ToUpper(long), long)
	}
}

// A key must never be a path segment that resolves outside its parent.
func TestKeyNeverEscapes(t *testing.T) {
	for _, id := range []string{".", "..", "...", "./x", "../x", "/abs", `C:\abs`, "..\\x"} {
		key := Key(id)
		if key == "." || key == ".." || strings.ContainsAny(key, `/\`) {
			t.Errorf("Key(%q) = %q, want a single usable directory name", id, key)
		}
	}
}
