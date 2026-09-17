package minyaml

import "testing"

func TestQuoteUnquoteRoundTrip(t *testing.T) {
	cases := []string{
		"simple",
		"with space",
		"it's",
		"a'b'c",
		"中文路径/温箱",
		`C:\Users\x`,
		"a: b",
		"#not-a-comment",
		"'",
	}
	for _, c := range cases {
		q, err := QuoteScalar(c)
		if err != nil {
			t.Fatalf("QuoteScalar(%q): %v", c, err)
		}
		got, err := UnquoteScalar(q)
		if err != nil {
			t.Fatalf("UnquoteScalar(%q): %v", q, err)
		}
		if got != c {
			t.Errorf("round trip %q -> %q -> %q", c, q, got)
		}
	}
}

func TestQuoteRejectsControlChars(t *testing.T) {
	for _, c := range []string{"a\nb", "a\tb", "\x00", "\x7f"} {
		if _, err := QuoteScalar(c); err == nil {
			t.Errorf("QuoteScalar(%q): expected error", c)
		}
	}
}

func TestUnquotePlainAndErrors(t *testing.T) {
	got, err := UnquoteScalar("plain-value")
	if err != nil || got != "plain-value" {
		t.Errorf("plain scalar: %q, %v", got, err)
	}
	for _, bad := range []string{"", "'unterminated", "un'expected"} {
		if _, err := UnquoteScalar(bad); err == nil {
			t.Errorf("UnquoteScalar(%q): expected error", bad)
		}
	}
}
