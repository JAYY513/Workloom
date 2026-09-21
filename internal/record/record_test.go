package record

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/storage"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	cmd := exec.Command("git", "init", "--quiet")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(root, ".devsys", "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	return New(root)
}

func TestCreateAssignsPerKindSequences(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	for i := 0; i < 2; i++ {
		id, err := s.CreateDecision(ctx, &domain.Decision{SchemaVersion: domain.SchemaVersion, ProjectID: "demo", Title: "决定", CreatedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		if id != "decision-1" && id != "decision-2" {
			t.Fatalf("id = %s", id)
		}
		fid, err := s.CreateFinding(ctx, &domain.Finding{SchemaVersion: domain.SchemaVersion, ProjectID: "demo", Type: "bug", Title: "发现"})
		if err != nil {
			t.Fatal(err)
		}
		if fid != "finding-1" && fid != "finding-2" {
			t.Fatalf("finding id = %s", fid)
		}
	}
	d, err := s.GetDecision(ctx, "decision-1")
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "决定" {
		t.Fatalf("title = %q", d.Title)
	}
	fs, err := s.ListFindings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 2 || fs[0].ID != "finding-1" || fs[1].ID != "finding-2" {
		t.Fatalf("findings = %d", len(fs))
	}
}

func TestArtifactThreeVersionsTraverseHistory(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	now := time.Now().UTC()
	a := &domain.Artifact{SchemaVersion: domain.SchemaVersion, ProjectID: "demo", Type: "architecture", Name: "架构说明", Status: "current", CreatedAt: now, UpdatedAt: now}
	v1, err := s.CreateArtifact(ctx, a)
	if err != nil {
		t.Fatal(err)
	}

	var v2ID string
	next := &domain.Artifact{}
	next.Name = "架构说明 v2"
	v2ID, err = s.NewArtifactVersion(ctx, v1, next)
	if err != nil {
		t.Fatal(err)
	}

	var v3ID string
	v3 := &domain.Artifact{}
	v3.Name = "架构说明 v3"
	v3ID, err = s.NewArtifactVersion(ctx, v2ID, v3base())
	if err != nil {
		t.Fatal(err)
	}

	hist, err := s.ArtifactHistory(ctx, v3ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 3 {
		t.Fatalf("history = %d versions", len(hist))
	}
	if hist[0].ID != v3ID || hist[1].ID != v2ID || hist[2].ID != v1 {
		t.Fatalf("chain = %s -> %s -> %s", hist[0].ID, hist[1].ID, hist[2].ID)
	}
	for i, ver := range hist {
		if ver.Version != len(hist)-i {
			t.Fatalf("version %d = %d", i, ver.Version)
		}
	}
	// 旧版本文件未被改动（不可变历史）。
	oldest, err := s.GetArtifact(ctx, v1)
	if err != nil {
		t.Fatal(err)
	}
	if oldest.Version != 1 || oldest.PreviousID != nil {
		t.Fatalf("oldest = v%d prev=%v", oldest.Version, oldest.PreviousID)
	}
}

func v3base() *domain.Artifact { return &domain.Artifact{Name: "架构说明 v3"} }

func TestBrokenHistoryLinkReported(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	a := &domain.Artifact{SchemaVersion: domain.SchemaVersion, ProjectID: "demo", Type: "doc", Name: "孤儿"}
	id, err := s.CreateArtifact(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	bad := "artifact-999"
	a.PreviousID = &bad
	if err := s.rewrite(ctx, id, a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ArtifactHistory(ctx, id); err == nil {
		t.Fatal("broken previous link accepted")
	}
}

// rewrite replaces a record's bytes inside a transaction (test-only bypass
// of the public API to simulate a hand-edited broken link).
func (s *Store) rewrite(ctx context.Context, id string, a *domain.Artifact) error {
	st, err := s.store()
	if err != nil {
		return err
	}
	return st.Write(ctx, func(tx *storage.Tx) error {
		return tx.PutYAML("artifacts/"+id+".yaml", a, storage.ExpectAny())
	})
}
