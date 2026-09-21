package record

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/storage"
)

func TestDecisionUpdateIsVersionGuarded(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	now := time.Now().UTC()
	d := &domain.Decision{ProjectID: "p", Title: "t", Decision: "d", Status: "proposed", CreatedBy: "me", CreatedAt: now}
	id, err := store.CreateDecision(ctx, d)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, raw, err := store.ReadDecision(ctx, id)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	cur, _, err := store.ReadDecision(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	cur.Status = "approved"
	cur.ApprovedBy = "reviewer"
	if err := store.UpdateDecision(ctx, cur, raw); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _, err := store.ReadDecision(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "approved" || got.ApprovedBy != "reviewer" {
		t.Fatalf("decision = %+v", got)
	}
	// The stale bytes are no longer authoritative.
	cur.Status = "rejected"
	if err := store.UpdateDecision(ctx, cur, raw); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale update error = %v, want conflict", err)
	}
}

func TestAppendArtifactVersionLinksPrevious(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	now := time.Now().UTC()
	a := &domain.Artifact{ProjectID: "p", Type: "document", Name: "design.md", Status: "draft", Version: 1, CreatedAt: now, UpdatedAt: now}
	id, err := store.CreateArtifact(ctx, a)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, currentRaw, err := store.ReadArtifact(ctx, id)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	newID, err := store.AppendArtifactVersion(ctx, id, currentRaw, func(next *domain.Artifact) {
		next.Status = "approved"
		next.Path = "docs/design.md"
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if newID == id {
		t.Fatal("append reused the previous id")
	}
	next, _, err := store.ReadArtifact(ctx, newID)
	if err != nil {
		t.Fatal(err)
	}
	if next.Version != 2 || next.Status != "approved" || next.Path != "docs/design.md" {
		t.Fatalf("new version = %+v", next)
	}
	if next.PreviousID == nil || *next.PreviousID != id {
		t.Fatalf("previous_id = %v, want %s", next.PreviousID, id)
	}
	history, err := store.ArtifactHistory(ctx, newID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Version != 2 || history[1].Version != 1 {
		t.Fatalf("history = %+v", history)
	}
	// The previous version is untouched.
	prev, _, err := store.ReadArtifact(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if prev.Version != 1 || prev.Status != "draft" {
		t.Fatalf("previous version changed: %+v", prev)
	}
}

func TestAppendArtifactVersionRefusesStaleAndSuperseded(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	now := time.Now().UTC()
	a := &domain.Artifact{ProjectID: "p", Type: "document", Name: "design.md", Status: "draft", Version: 1, CreatedAt: now, UpdatedAt: now}
	id, err := store.CreateArtifact(ctx, a)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, raw, err := store.ReadArtifact(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendArtifactVersion(ctx, id, append([]byte(nil), raw...), nil); err != nil {
		t.Fatalf("first append: %v", err)
	}
	// A second append from the same parent would fork the chain.
	if _, err := store.AppendArtifactVersion(ctx, id, raw, nil); !errors.Is(err, ErrSuperseded) {
		t.Fatalf("second append error = %v, want ErrSuperseded", err)
	}
	// A stale snapshot never authorizes an append.
	if _, err := store.AppendArtifactVersion(ctx, id, []byte("stale bytes"), nil); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("stale append error = %v, want conflict", err)
	}
}
