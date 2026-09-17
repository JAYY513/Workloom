package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenRequiresInitializedProject(t *testing.T) {
	root := t.TempDir()
	if _, err := Open(root, Options{}); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("error = %v, want ErrNotInitialized", err)
	}
	if err := os.MkdirAll(filepath.Join(root, DevsysDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, Options{}); err != nil {
		t.Fatalf("open initialized project: %v", err)
	}
}

func TestResolveRejectsUnsafePaths(t *testing.T) {
	s, _ := newTestStore(t)
	bad := []string{
		"",
		"../outside.yaml",
		"a/../../outside.yaml",
		"/absolute.yaml",
		`back\slash.yaml`,
		"C:/drive.yaml",
		"local/lock",
		"local/txn",
		"local/txn/20260101T000000.000000000Z/x",
	}
	for _, rel := range bad {
		if _, err := s.resolve(rel); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("resolve(%q) error = %v, want ErrUnsafePath", rel, err)
		}
	}
	good := map[string]string{
		"state/current.yaml":   filepath.Join(s.devsys, "state", "current.yaml"),
		"events/2026-09.jsonl": filepath.Join(s.devsys, "events", "2026-09.jsonl"),
		"a/b/../c.yaml":        filepath.Join(s.devsys, "a", "c.yaml"),
		"local/logs/run.txt":   filepath.Join(s.devsys, "local", "logs", "run.txt"),
	}
	for rel, want := range good {
		got, err := s.resolve(rel)
		if err != nil || got != want {
			t.Errorf("resolve(%q) = %q, %v; want %q", rel, got, err, want)
		}
	}
}

func TestUnsafePathsAreRejectedBeforeAnyWrite(t *testing.T) {
	s, root := newTestStore(t)
	err := s.Write(context.Background(), func(tx *Tx) error {
		return tx.Put("../escape.yaml", []byte("x"), ExpectAny())
	})
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("error = %v, want ErrUnsafePath", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "escape.yaml")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Error("unsafe write reached the file system")
	}
}

func TestPutCreatesAndReplacesFiles(t *testing.T) {
	s, root := newTestStore(t)
	ctx := context.Background()

	// A missing file, staged with ExpectAbsent.
	if err := s.Write(ctx, func(tx *Tx) error {
		return tx.Put(stateRel, []byte(newStateV2), ExpectAbsent())
	}); err != nil {
		t.Fatal(err)
	}
	if got := string(readDevsysFile(t, root, stateRel)); got != newStateV2 {
		t.Errorf("state = %q, want %q", got, newStateV2)
	}

	// An empty existing file is a normal target.
	emptyRel := "state/empty.yaml"
	seedDevsysFile(t, root, emptyRel, nil)
	if err := s.Write(ctx, func(tx *Tx) error {
		return tx.Put(emptyRel, []byte("filled\n"), ExpectHash(HashBytes(nil)))
	}); err != nil {
		t.Fatal(err)
	}
	if got := string(readDevsysFile(t, root, emptyRel)); got != "filled\n" {
		t.Errorf("empty file = %q", got)
	}

	// ExpectAbsent on an existing file conflicts.
	if err := s.Write(ctx, func(tx *Tx) error {
		return tx.Put(stateRel, []byte("again\n"), ExpectAbsent())
	}); !errors.Is(err, ErrConflict) {
		t.Errorf("error = %v, want ErrConflict", err)
	}

	// A stale version conflicts and leaves the file untouched.
	stale := ExpectHash(HashBytes([]byte("something else")))
	if err := s.Write(ctx, func(tx *Tx) error {
		return tx.Put(stateRel, []byte("stale\n"), stale)
	}); !errors.Is(err, ErrConflict) {
		t.Errorf("error = %v, want ErrConflict", err)
	}
	if got := string(readDevsysFile(t, root, stateRel)); got != newStateV2 {
		t.Errorf("conflicting write changed the file: %q", got)
	}
}

func TestPutYAMLIsStable(t *testing.T) {
	s, root := newTestStore(t)
	ctx := context.Background()
	type doc struct {
		SchemaVersion int    `yaml:"schema_version"`
		Summary       string `yaml:"summary"`
	}
	if err := s.Write(ctx, func(tx *Tx) error {
		return tx.PutYAML("state/current.yaml", doc{SchemaVersion: 1, Summary: "first"}, ExpectAbsent())
	}); err != nil {
		t.Fatal(err)
	}
	first := readDevsysFile(t, root, "state/current.yaml")
	var back doc
	if err := DecodeYAML(first, &back); err != nil {
		t.Fatalf("decode written file: %v", err)
	}
	if back.SchemaVersion != 1 || back.Summary != "first" {
		t.Fatalf("round trip = %+v", back)
	}
	// Writing the same value again must produce byte-identical content.
	if err := s.Write(ctx, func(tx *Tx) error {
		return tx.PutYAML("state/current.yaml", doc{SchemaVersion: 1, Summary: "first"},
			ExpectHash(HashBytes(first)))
	}); err != nil {
		t.Fatal(err)
	}
	if again := readDevsysFile(t, root, "state/current.yaml"); string(again) != string(first) {
		t.Errorf("second write differs:\n%s\n---\n%s", first, again)
	}
}

func TestAppendJSONLCreatesAndAppends(t *testing.T) {
	s, root := newTestStore(t)
	ctx := context.Background()
	record := map[string]string{"event": "created"}

	if err := s.Write(ctx, func(tx *Tx) error {
		return tx.AppendJSONL(eventsRel, record)
	}); err != nil {
		t.Fatal(err)
	}
	// Appending to an existing (and then torn) file.
	if err := s.Write(ctx, func(tx *Tx) error {
		return tx.AppendJSONL(eventsRel, map[string]string{"event": "updated"})
	}); err != nil {
		t.Fatal(err)
	}
	got := string(readDevsysFile(t, root, eventsRel))
	want := "{\"event\":\"created\"}\n{\"event\":\"updated\"}\n"
	if got != want {
		t.Errorf("events = %q, want %q", got, want)
	}

	// A torn tail blocks the next append instead of corrupting the file.
	seedDevsysFile(t, root, "events/torn.jsonl", []byte("{\"event\":\"half\"}\n{\"event\":"))
	err := s.Write(ctx, func(tx *Tx) error {
		return tx.AppendJSONL("events/torn.jsonl", map[string]string{"event": "next"})
	})
	if !errors.Is(err, ErrIncompleteTail) {
		t.Fatalf("error = %v, want ErrIncompleteTail", err)
	}
	if got := string(readDevsysFile(t, root, "events/torn.jsonl")); got != "{\"event\":\"half\"}\n{\"event\":" {
		t.Errorf("torn file was modified: %q", got)
	}
}

func TestReadMissingFileAndExpect(t *testing.T) {
	s, root := newTestStore(t)
	ctx := context.Background()
	seedDevsysFile(t, root, stateRel, []byte(seedStateV1))

	err := s.Read(ctx, func(r *Reader) error {
		data, exists, err := r.Read(stateRel)
		if err != nil || !exists || string(data) != seedStateV1 {
			t.Errorf("read = %q, %v, %v", data, exists, err)
		}
		if _, exists, err := r.Read("state/nope.yaml"); err != nil || exists {
			t.Errorf("missing file = exists %v, err %v", exists, err)
		}
		exp, err := r.Expect("state/nope.yaml")
		if err != nil || exp.String() != "absent" {
			t.Errorf("expect missing = %v, %v", exp, err)
		}
		exp, err = r.Expect(stateRel)
		if err != nil || exp != ExpectHash(HashBytes([]byte(seedStateV1))) {
			t.Errorf("expect existing = %v, %v", exp, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReaderWaitsForWriterAndTimesOut(t *testing.T) {
	s, root := newTestStore(t)
	release := make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- s.Write(context.Background(), func(tx *Tx) error {
			select {
			case <-release:
			case <-time.After(10 * time.Second):
			}
			return nil
		})
	}()
	waitForLockHolder(t, root)

	blocked, err := Open(root, Options{LockTimeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	err = blocked.Read(context.Background(), func(r *Reader) error { return nil })
	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("read error = %v, want ErrLockTimeout", err)
	}

	close(release)
	if err := <-writerDone; err != nil {
		t.Fatal(err)
	}
	if err := blocked.Read(context.Background(), func(r *Reader) error { return nil }); err != nil {
		t.Fatalf("read after the writer finished: %v", err)
	}
}

func TestReadHonoursContextCancellation(t *testing.T) {
	s, root := newTestStore(t)
	release := make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- s.Write(context.Background(), func(tx *Tx) error {
			<-release
			return nil
		})
	}()
	waitForLockHolder(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blocked, err := Open(root, Options{LockTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err := blocked.Read(ctx, func(r *Reader) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Errorf("read error = %v, want context.Canceled", err)
	}
	close(release)
	if err := <-writerDone; err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentAppendsDoNotInterleave(t *testing.T) {
	_, root := newTestStore(t)
	const writers = 8
	start := make(chan struct{})
	errs := make(chan error, writers)
	for i := range writers {
		go func(i int) {
			s, err := Open(root, Options{LockTimeout: 30 * time.Second})
			if err != nil {
				errs <- err
				return
			}
			<-start
			errs <- s.Write(context.Background(), func(tx *Tx) error {
				return tx.AppendJSONL(eventsRel, map[string]string{"event": fmt.Sprintf("w%d", i)})
			})
		}(i)
	}
	close(start)
	for range writers {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent append: %v", err)
		}
	}
	data := string(readDevsysFile(t, root, eventsRel))
	lines := strings.Split(strings.TrimSuffix(data, "\n"), "\n")
	if len(lines) != writers {
		t.Fatalf("events = %d lines, want %d:\n%s", len(lines), writers, data)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "{\"event\":\"w") || strings.Count(line, "event") != 1 {
			t.Errorf("interleaved event line: %q", line)
		}
	}
}
