package events

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet")
	if err := os.MkdirAll(filepath.Join(root, ".devsys", "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	return New(root)
}

func event(at time.Time, typ string, subject domain.Reference) *domain.Event {
	return &domain.Event{
		SchemaVersion: domain.SchemaVersion, ProjectID: "demo",
		Type: typ, Subject: subject, Time: at, Actor: "tester",
		Related: []domain.Reference{{Type: "workitem", ID: "WLM-1"}},
		Content: "内容<&>",
	}
}

func TestAppendFiltersByTimeAndSubject(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	wiA := domain.Reference{Type: "workitem", ID: "WLM-1"}
	wiB := domain.Reference{Type: "workitem", ID: "WLM-2"}
	for i := range 50 {
		ev := event(base.Add(time.Duration(i)*time.Second), "comment", wiA)
		if err := s.Append(ctx, ev); err != nil {
			t.Fatal(err)
		}
		ev = event(base.Add(time.Duration(i)*time.Second), "status", wiB)
		if err := s.Append(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Read(ctx, Filter{Type: "comment"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 50 {
		t.Fatalf("comment count = %d", len(got))
	}
	for i, ev := range got {
		if want := base.Add(time.Duration(i) * time.Second); !ev.Time.Equal(want) {
			t.Fatalf("event %d time = %v, want %v", i, ev.Time, want)
		}
	}
	got, err = s.Read(ctx, Filter{From: base.Add(10 * time.Second), To: base.Add(19 * time.Second), Subject: &wiB})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 {
		t.Fatalf("window = %d events", len(got))
	}
	for _, ev := range got {
		if ev.Subject != wiB {
			t.Fatalf("subject = %+v", ev.Subject)
		}
	}
}

func TestShardSplitsAcrossMonths(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	sep := event(time.Date(2026, 9, 30, 23, 59, 59, 0, time.UTC), "comment", domain.Reference{Type: "workitem", ID: "WLM-1"})
	oct := event(time.Date(2026, 10, 1, 0, 0, 1, 0, time.UTC), "comment", domain.Reference{Type: "workitem", ID: "WLM-1"})
	for _, ev := range []*domain.Event{sep, oct} {
		if err := s.Append(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	for _, shard := range []string{"2026-09.jsonl", "2026-10.jsonl"} {
		if _, err := os.Stat(filepath.Join(s.root, ".devsys", "events", shard)); err != nil {
			t.Fatalf("shard %s: %v", shard, err)
		}
	}
	got, err := s.Read(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Time.Before(got[1].Time) {
		t.Fatalf("cross-month order broken: %d events", len(got))
	}
	// A December event written "now" lands in the December shard by its own time.
	dec := event(time.Date(2026, 12, 24, 8, 0, 0, 0, time.UTC), "comment", domain.Reference{Type: "workitem", ID: "WLM-1"})
	if err := s.Append(ctx, dec); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.root, ".devsys", "events", "2026-12.jsonl")); err != nil {
		t.Fatalf("december shard: %v", err)
	}
}

func TestCommentThreadsRestoreReplies(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	subject := domain.Reference{Type: "workitem", ID: "WLM-7"}
	base := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	root1 := event(base, "comment", subject)
	root2 := event(base.Add(time.Second), "comment", subject)
	for _, ev := range []*domain.Event{root1, root2} {
		if err := s.Append(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	reply1 := event(base.Add(2*time.Second), "comment", subject)
	reply1.ReplyTo = &root1.ID
	if err := s.Append(ctx, reply1); err != nil {
		t.Fatal(err)
	}
	reply2 := event(base.Add(3*time.Second), "comment", subject)
	reply2.ReplyTo = &root1.ID
	if err := s.Append(ctx, reply2); err != nil {
		t.Fatal(err)
	}

	threads, err := s.Comments(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 2 {
		t.Fatalf("threads = %d", len(threads))
	}
	if threads[0].Comment.ID != root1.ID || len(threads[0].Replies) != 2 {
		t.Fatalf("thread 0 = %d replies", len(threads[0].Replies))
	}
	if threads[1].Comment.ID != root2.ID || len(threads[1].Replies) != 0 {
		t.Fatalf("thread 1 has %d replies", len(threads[1].Replies))
	}
	if threads[0].Replies[0].ID != reply1.ID || threads[0].Replies[1].ID != reply2.ID {
		t.Fatal("reply order broken")
	}
}

func TestReplyValidationRejectsBrokenRelations(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	subject := domain.Reference{Type: "workitem", ID: "WLM-7"}
	base := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	root := event(base, "comment", subject)
	if err := s.Append(ctx, root); err != nil {
		t.Fatal(err)
	}

	// reply_to on a non-comment event is rejected at append time.
	wrong := event(base.Add(time.Second), "status", subject)
	wrong.ReplyTo = &root.ID
	if err := s.Append(ctx, wrong); err == nil {
		t.Fatal("non-comment reply accepted")
	}

	// A dangling reply_to parses but fails thread reconstruction.
	orphan := event(base.Add(2*time.Second), "comment", subject)
	missing := "ev-missing"
	orphan.ReplyTo = &missing
	if err := s.Append(ctx, orphan); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Comments(ctx, subject); err == nil {
		t.Fatal("dangling reply accepted")
	}
}

func TestUnknownAndTornStateAreReported(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	shard := filepath.Join(s.root, ".devsys", "events", "2026-09.jsonl")
	if err := s.Append(ctx, event(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), "comment", domain.Reference{Type: "workitem", ID: "WLM-1"})); err != nil {
		t.Fatal(err)
	}
	raw, err := readAll(shard)
	if err != nil {
		t.Fatal(err)
	}
	// A line without the terminator must block further appends (M0.3 协议).
	if err := writeFile(shard, raw[:len(raw)-1]); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, event(time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC), "comment", domain.Reference{Type: "workitem", ID: "WLM-1"})); err == nil {
		t.Fatal("append into torn tail accepted")
	}
}

func readAll(path string) ([]byte, error) { return os.ReadFile(path) }

func writeFile(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }
