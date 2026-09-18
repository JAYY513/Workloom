// Record propagation on completion (实施计划 M3.3, 方案 §4.7).
//
// When a work item enters done, its Decision and Finding records — those
// whose RelatedWorkItems name it — are propagated as references
// (decision://<id>, finding://<id>) to the parent and the direct siblings.
// Propagation runs inside the completion transaction: targets are read and
// rewritten under the same optimistic guard, their status is untouched, and
// a target that already carries every reference is skipped, so repeated
// completions stay idempotent. No links means no broadcast.
package workitem

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"workloom/internal/domain"
	"workloom/internal/storage"
)

// propagateRecordsTx appends the completing work item's record references to
// its parent and direct siblings inside the completion transaction and
// returns the propagation events for the caller to stage in the same
// transaction (one append per shard). Any error rolls the completion back.
func propagateRecordsTx(tx *storage.Tx, devsysDir string, wi *domain.WorkItem, now time.Time, actor string) ([]*domain.Event, error) {
	if wi.ParentID == nil || *wi.ParentID == "" {
		return nil, nil
	}
	if *wi.ParentID == wi.ID {
		return nil, fmt.Errorf("workitem: %s has a parent link to itself", wi.ID)
	}
	refs, err := relatedRecordRefs(tx, devsysDir, wi.ID)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, nil
	}
	targets, err := propagationTargets(tx, devsysDir, wi)
	if err != nil {
		return nil, err
	}
	var out []*domain.Event
	for _, id := range targets {
		ev, err := propagateToTarget(tx, id, wi, refs, now, actor)
		if err != nil {
			return nil, err
		}
		if ev != nil {
			out = append(out, ev)
		}
	}
	return out, nil
}

// relatedRecordRefs scans decisions/ and findings/ for records linked to
// workitemID (RelatedWorkItems) and returns their references in record
// order.
func relatedRecordRefs(tx *storage.Tx, devsysDir, workitemID string) ([]domain.Reference, error) {
	var refs []domain.Reference
	for _, dir := range []string{"decisions", "findings"} {
		entries, err := os.ReadDir(filepath.Join(devsysDir, dir))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("workitem: scan %s: %w", dir, err)
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			rel := dir + "/" + name
			raw, exists, err := tx.ReadForExpect(rel)
			if err != nil {
				return nil, err
			}
			if !exists {
				continue
			}
			id := strings.TrimSuffix(name, ".yaml")
			ref, ok, err := recordRef(raw, dir, id, workitemID)
			if err != nil {
				return nil, err
			}
			if ok {
				refs = append(refs, ref)
			}
		}
	}
	return refs, nil
}

// recordRef decodes one record file and reports whether it is linked to
// workitemID.
func recordRef(raw []byte, dir, id, workitemID string) (domain.Reference, bool, error) {
	var related []string
	switch dir {
	case "decisions":
		var d domain.Decision
		if err := domain.DecodeYAML(raw, &d); err != nil {
			return domain.Reference{}, false, fmt.Errorf("workitem: decode decision %s: %w", id, err)
		}
		related = d.RelatedWorkItems
	case "findings":
		var f domain.Finding
		if err := domain.DecodeYAML(raw, &f); err != nil {
			return domain.Reference{}, false, fmt.Errorf("workitem: decode finding %s: %w", id, err)
		}
		related = f.RelatedWorkItems
	}
	for _, w := range related {
		if w == workitemID {
			return domain.Reference{Type: strings.TrimSuffix(dir, "s"), ID: id}, true, nil
		}
	}
	return domain.Reference{}, false, nil
}

// propagationTargets returns the parent followed by the direct siblings of
// wi, ordered by id.
func propagationTargets(tx *storage.Tx, devsysDir string, wi *domain.WorkItem) ([]string, error) {
	parent := *wi.ParentID
	entries, err := os.ReadDir(filepath.Join(devsysDir, dirRel))
	if err != nil {
		if os.IsNotExist(err) {
			return []string{parent}, nil
		}
		return nil, fmt.Errorf("workitem: scan workitems: %w", err)
	}
	var siblings []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		id := strings.TrimSuffix(name, ".yaml")
		if id == wi.ID || id == parent || !ValidID(id) {
			continue
		}
		raw, exists, err := tx.ReadForExpect(dirRel + "/" + id + ".yaml")
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		var other domain.WorkItem
		if err := domain.DecodeYAML(raw, &other); err != nil {
			return nil, fmt.Errorf("workitem: decode %s: %w", id, err)
		}
		if other.ParentID != nil && *other.ParentID == parent {
			siblings = append(siblings, id)
		}
	}
	sort.Strings(siblings)
	return append([]string{parent}, siblings...), nil
}

// propagateToTarget appends the missing references to one target, writes it
// under the optimistic guard and returns the propagation event. Targets that
// already carry every reference are skipped entirely (no write, no event).
func propagateToTarget(tx *storage.Tx, id string, wi *domain.WorkItem, refs []domain.Reference, now time.Time, actor string) (*domain.Event, error) {
	rel := dirRel + "/" + id + ".yaml"
	raw, exists, err := tx.ReadForExpect(rel)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil // dangling parent/sibling link: nothing to write
	}
	var target domain.WorkItem
	if err := domain.DecodeYAML(raw, &target); err != nil {
		return nil, fmt.Errorf("workitem: decode %s: %w", id, err)
	}
	have := map[string]bool{}
	for _, r := range target.ContextRefs {
		have[r] = true
	}
	var added []domain.Reference
	for _, ref := range refs {
		s := ref.Type + "://" + ref.ID
		if have[s] {
			continue
		}
		have[s] = true
		target.ContextRefs = append(target.ContextRefs, s)
		added = append(added, ref)
	}
	if len(added) == 0 {
		return nil, nil
	}
	target.UpdatedAt = now
	if err := tx.PutYAML(rel, &target, storage.ExpectHash(storage.HashBytes(raw))); err != nil {
		return nil, err
	}
	related := []domain.Reference{{Type: "workitem", ID: wi.ID}}
	related = append(related, added...)
	ev := &domain.Event{
		SchemaVersion: domain.SchemaVersion,
		ProjectID:     wi.ProjectID,
		Type:          "record_propagated",
		Subject:       domain.Reference{Type: "workitem", ID: id},
		Time:          now.UTC(),
		Actor:         actor,
		Content:       fmt.Sprintf("records from %s: %s", wi.ID, refStrings(added)),
		Related:       related,
	}
	return ev, nil
}

func refStrings(refs []domain.Reference) string {
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		parts = append(parts, r.Type+"://"+r.ID)
	}
	return strings.Join(parts, ", ")
}
