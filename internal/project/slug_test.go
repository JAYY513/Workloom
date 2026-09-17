package project

import (
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"TempMonitorSystem", "tempmonitorsystem"},
		{"temp-monitor-system", "temp-monitor-system"},
		{"My Project (v2)", "my-project-v2"},
		{"a__b", "a-b"},
		{"-leading-and-trailing-", "leading-and-trailing"},
		{"UPPER_CASE_NAME", "upper-case-name"},
	}
	for _, c := range cases {
		if got := Slug(c.in); got != c.want {
			t.Errorf("Slug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSlugFallbackIsDeterministicAndDistinct(t *testing.T) {
	a, b := Slug("温箱监控"), Slug("温箱监控")
	if a != b {
		t.Fatalf("not deterministic: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "project-") {
		t.Errorf("fallback = %q, want project-<hash>", a)
	}
	if a == Slug("另一项目") {
		t.Errorf("distinct names must not collide: %q", a)
	}
}
