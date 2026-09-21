package version

import "testing"

func TestResolveVersion(t *testing.T) {
	cases := []struct {
		name   string
		binary string
		main   string
		want   string
	}{
		{"ldflags release wins", "v0.1.7", "v0.1.7", "v0.1.7"},
		{"go install tag", devMarker, "v0.1.7", "v0.1.7"},
		{"plain repo build keeps marker", devMarker, "(devel)", devMarker},
		{"missing build info keeps marker", devMarker, "", devMarker},
		{"dev marker with pseudo-version", devMarker, "v0.1.7-0.20260921023000-abc123", "v0.1.7-0.20260921023000-abc123"},
	}
	for _, tc := range cases {
		if got := resolveVersion(tc.binary, tc.main); got != tc.want {
			t.Errorf("%s: resolveVersion(%q, %q) = %q, want %q", tc.name, tc.binary, tc.main, got, tc.want)
		}
	}
}

func TestStringShapes(t *testing.T) {
	// The development build from a git checkout: marker + revision.
	if s := String(); s == "" {
		t.Fatal("String() empty")
	}
}
