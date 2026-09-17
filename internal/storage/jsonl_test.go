package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectJSONLStates(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		content  string
		existing bool
		want     TailStatus
	}{
		{name: "missing", want: TailStatus{Size: 0, Complete: true}},
		{name: "empty", existing: true, want: TailStatus{Size: 0, Complete: true}},
		{name: "complete", existing: true, content: "{\"a\":1}\n", want: TailStatus{Size: 8, Complete: true}},
		{name: "torn", existing: true, content: "{\"a\":1}\n{\"b\"", want: TailStatus{Size: 12, Complete: false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".jsonl")
			if tc.existing {
				if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := InspectJSONL(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("InspectJSONL = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestScanJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	content := "{\"a\":1}\n{\"b\":2}\r\n\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var lines []string
	if err := ScanJSONL(path, func(line []byte) error {
		lines = append(lines, string(line))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{`{"a":1}`, `{"b":2}`, ""}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}

	if err := ScanJSONL(filepath.Join(t.TempDir(), "missing.jsonl"), func([]byte) error {
		t.Error("callback must not run for a missing file")
		return nil
	}); err != nil {
		t.Fatalf("missing file: %v", err)
	}
}

func TestScanJSONLTornTailIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "torn.jsonl")
	if err := os.WriteFile(path, []byte("{\"a\":1}\n{\"b\""), 0o644); err != nil {
		t.Fatal(err)
	}
	seen := 0
	err := ScanJSONL(path, func([]byte) error { seen++; return nil })
	if !errors.Is(err, ErrIncompleteTail) {
		t.Fatalf("error = %v, want ErrIncompleteTail", err)
	}
	if seen != 1 {
		t.Errorf("callback saw %d lines, want only the complete one", seen)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error should name the torn line: %v", err)
	}
}

func TestScanJSONLPropagatesCallbackErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("{\"a\":1}\n{\"a\":2}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("stop here")
	count := 0
	err := ScanJSONL(path, func([]byte) error {
		count++
		if count == 2 {
			return boom
		}
		return nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the callback error", err)
	}
}

func TestAppendJSONLGuards(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	if err := appendJSONL(path, []byte("no terminator"), 0); err == nil {
		t.Error("payload without a line terminator must be rejected")
	}
	if err := appendJSONL(path, []byte("{\"a\":1}\n"), 5); err == nil {
		t.Error("size mismatch must be rejected")
	}
	if err := appendJSONL(path, []byte("{\"a\":1}\n"), 0); err != nil {
		t.Fatalf("append to missing file: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "{\"a\":1}\n" {
		t.Fatalf("content = %q", got)
	}

	torn := filepath.Join(dir, "torn.jsonl")
	if err := os.WriteFile(torn, []byte("{\"a\":1}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendJSONL(torn, []byte("{\"b\":2}\n"), 7); !errors.Is(err, ErrIncompleteTail) {
		t.Fatalf("error = %v, want ErrIncompleteTail", err)
	}
}
