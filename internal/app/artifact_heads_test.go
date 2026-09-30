package app

import (
	"testing"

	"github.com/JAYY513/Workloom/internal/domain"
)

func TestArtifactHeadsKeepsUnsupersededOnly(t *testing.T) {
	prev := "artifact-1"
	items := []*domain.Artifact{
		{ID: "artifact-1", Version: 1},
		{ID: "artifact-2", Version: 2, PreviousID: &prev},
		{ID: "artifact-3", Version: 1},
	}
	heads := ArtifactHeads(items)
	if len(heads) != 2 || heads[0].ID != "artifact-2" || heads[1].ID != "artifact-3" {
		t.Fatalf("heads = %+v", heads)
	}
	if len(items) != 3 {
		t.Fatal("filter must not rewrite the immutable list")
	}
}
