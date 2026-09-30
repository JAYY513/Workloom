package domain

import "testing"

func TestArtifactBelongsTo(t *testing.T) {
	a := &Artifact{WorkItemID: "WLM-1", RelatedWorkItems: []string{"WLM-2"}}
	if !a.BelongsTo("WLM-1") || !a.BelongsTo("WLM-2") {
		t.Fatal("primary ownership and association must both count")
	}
	if a.BelongsTo("WLM-3") || a.BelongsTo("") || (*Artifact)(nil).BelongsTo("WLM-1") {
		t.Fatal("foreign, empty, and nil must not match")
	}
	relatedOnly := &Artifact{RelatedWorkItems: []string{"WLM-4"}}
	if !relatedOnly.BelongsTo("WLM-4") || relatedOnly.BelongsTo("WLM-1") {
		t.Fatal("association-only artifact matched the wrong work item")
	}
}
