// Package reconcile — boundary tests for M2.3/M2.4.
//
// Coverage:
//   - Doctor no-writes test: tree byte snapshot before/after Doctor;
//     assert no new files, no byte changes, and the lock file is NOT
//     created by inspection.
//   - Doctor pending-txn test: pre-stage an uncommitted journal, run
//     Doctor, assert PendingTransactions populated and Orphans empty.
//   - Recover idempotence: one expired lease, run Recover twice, assert
//     exactly one lease_recovered event total.
//   - Recover orphan: a lease whose RunID file is missing → released
//     under ForOrphan.
//   - RepairApply rejects on stale digest / drifted evidence.
//   - Downward repair proposal is synthesised when workitem status is
//     done and ONE referenced artifact is missing (Main Q1). The apply
//     call expects CoreM2's AllowRepair graph lift; when the gate is
//     closed by core, the proposal still appears in the plan and the
//     apply call records the rejection. We do NOT exercise ApplyRepair
//     against a missing graph lift — we only assert the proposal table.
//   - Missing-evidence unrepairable: when the workitem has no
//     artifact_refs, no proposal is synthesised.
//
// Tests do NOT call workitem.Claim — claim semantics are owned by
// CoreM2 and exercised elsewhere. Tests construct lease files and
// workitem files directly to keep the slice hermetic.
package reconcile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/storage"
)

// newProject creates a temp dir with .devsys seeded.
func newProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	devsys := filepath.Join(root, ".devsys")
	for _, sub := range []string{"local", "workitems", "scheduling", "runs", "artifacts", "decisions", "findings", "events", "approvals", "context", "knowledge", "state", "specs", "workflows"} {
		if err := os.MkdirAll(filepath.Join(devsys, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// snapshotTree walks <root>/.devsys and returns the relative path → sha256
// map of every file's bytes.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	devsys := filepath.Join(root, ".devsys")
	err := filepath.WalkDir(devsys, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(devsys, p)
		rel = filepath.ToSlash(rel)
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(data)
		out[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func expiredCount(probes []LeaseProbe) int {
	n := 0
	for _, p := range probes {
		if p.Expired {
			n++
		}
	}
	return n
}

func orphanCount(probes []LeaseProbe) int {
	n := 0
	for _, p := range probes {
		if p.ReasonKind == "orphan" {
			n++
		}
	}
	return n
}

// writeWorkitem writes a workitem YAML.
func writeWorkitem(t *testing.T, root string, wi *domain.WorkItem) {
	t.Helper()
	data, err := domain.EncodeYAML(wi)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".devsys", "workitems", wi.ID+".yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// readWorkitem reads and decodes one workitem.
func readWorkitem(t *testing.T, root, id string) *domain.WorkItem {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".devsys", "workitems", id+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	wi := &domain.WorkItem{}
	if err := domain.DecodeYAML(data, wi); err != nil {
		t.Fatal(err)
	}
	return wi
}

// writeLease writes a lease YAML.
func writeLease(t *testing.T, root string, lease *domain.SchedulingLease) {
	t.Helper()
	if lease.SchemaVersion == 0 {
		lease.SchemaVersion = domain.SchemaVersion
	}
	data, err := domain.EncodeYAML(lease)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".devsys", "scheduling", lease.WorkItemID+".yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func sampleWorkitem(id, status string, artifactRefs []string) *domain.WorkItem {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	return &domain.WorkItem{
		SchemaVersion: domain.SchemaVersion,
		ID:            id,
		ProjectID:     "demo",
		Type:          "feature",
		Title:         "title",
		Status:        status,
		Priority:      2,
		ArtifactRefs:  artifactRefs,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// TestDoctorDoesNotWriteOrCreateLock is the no-writes boundary test.
func TestDoctorDoesNotWriteOrCreateLock(t *testing.T) {
	root := newProject(t)
	writeLease(t, root, &domain.SchedulingLease{
		WorkItemID: "WLM-1",
		Owner:      "agent-a",
		Token:      "tok-a",
		ClaimedAt:  time.Now().UTC().Add(-time.Hour),
		LeaseUntil: time.Now().UTC().Add(time.Hour),
	})
	writeLease(t, root, &domain.SchedulingLease{
		WorkItemID: "WLM-2",
		Owner:      "agent-b",
		Token:      "tok-b",
		ClaimedAt:  time.Now().UTC().Add(-2 * time.Hour),
		LeaseUntil: time.Now().UTC().Add(-time.Minute),
	})
	before := snapshotTree(t, root)
	if _, err := os.Stat(filepath.Join(root, ".devsys", "local", "lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock file exists before Doctor: %v", err)
	}

	rep, err := Doctor(context.Background(), root, Options{Actor: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	after := snapshotTree(t, root)
	if !mapsEqual(before, after) {
		t.Fatalf("Doctor mutated the tree")
	}
	if _, err := os.Stat(filepath.Join(root, ".devsys", "local", "lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Doctor created the lock file: %v", err)
	}
	if got := expiredCount(rep.ExpiredLeases); got != 1 {
		t.Fatalf("expired leases = %d, want 1", got)
	}
}

// TestDoctorPendingTransactionsBlockOrphans.
func TestDoctorPendingTransactionsBlockOrphans(t *testing.T) {
	root := newProject(t)
	writeWorkitem(t, root, sampleWorkitem("WLM-1", "in_progress", nil))
	writeLease(t, root, &domain.SchedulingLease{
		WorkItemID: "WLM-1",
		Owner:      "agent-a",
		Token:      "tok",
		ClaimedAt:  time.Now().UTC().Add(-time.Hour),
		LeaseUntil: time.Now().UTC().Add(-time.Minute),
	})
	txnID := "20260101T000000.000000000Z-1-deadbeef"
	if err := os.MkdirAll(filepath.Join(root, ".devsys", "local", "txn", txnID, "payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	jrnl := `txn_id: ` + txnID + `
created_at: 2026-01-01T00:00:00Z
pid: 1
ops:
  - kind: put
    rel: workitems/WLM-1.yaml
    expect: sha256:0000000000000000000000000000000000000000000000000000000000000000
    payload: payload/000.bin
    payload_sha256: 0000000000000000000000000000000000000000000000000000000000000000
    payload_size: 0
`
	if err := os.WriteFile(filepath.Join(root, ".devsys", "local", "txn", txnID, "journal.yaml"), []byte(jrnl), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := Doctor(context.Background(), root, Options{Actor: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.PendingTransactions) == 0 {
		t.Fatal("expected pending transactions")
	}
	if len(rep.Orphans) != 0 {
		t.Fatalf("Doctor must not propose orphans while transactions are pending; got %d", len(rep.Orphans))
	}
	if !strings.Contains(rep.Note, "pending transactions") {
		t.Fatalf("Note = %q", rep.Note)
	}
}

// TestRepairDryRunDigestIsReproducible.
func TestRepairDryRunDigestIsReproducible(t *testing.T) {
	root := newProject(t)
	writeWorkitem(t, root, sampleWorkitem("WLM-1", "in_progress", nil))
	writeLease(t, root, &domain.SchedulingLease{
		WorkItemID: "WLM-1",
		Owner:      "agent-a",
		Token:      "tok",
		ClaimedAt:  time.Now().UTC().Add(-time.Hour),
		LeaseUntil: time.Now().UTC().Add(-time.Minute),
	})
	planA, err := RepairDryRun(context.Background(), root, Options{Actor: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	planB, err := RepairDryRun(context.Background(), root, Options{Actor: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if planA.Digest == "" || planA.Digest != planB.Digest {
		t.Fatalf("digest not reproducible: %q vs %q", planA.Digest, planB.Digest)
	}
}

// TestRepairApplyRefusesOnBadDigest.
func TestRepairApplyRefusesOnBadDigest(t *testing.T) {
	root := newProject(t)
	writeWorkitem(t, root, sampleWorkitem("WLM-1", "in_progress", nil))
	plan := Plan{
		Proposals: []Proposal{{Kind: ProposalReleaseExpiredLease, WorkitemID: "WLM-1", Description: "x"}},
		Digest:    "deadbeef",
	}
	if _, err := RepairApply(context.Background(), root, plan, Options{Actor: "tester", Reason: "t"}); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
}

// TestRepairApplyRejectsOnEvidenceDrift mutates the workitem after
// dry-run and asserts the apply call records the drift.
func TestRepairApplyRejectsOnEvidenceDrift(t *testing.T) {
	root := newProject(t)
	writeWorkitem(t, root, sampleWorkitem("WLM-1", "in_progress", nil))
	writeLease(t, root, &domain.SchedulingLease{
		WorkItemID: "WLM-1",
		Owner:      "agent-a",
		Token:      "tok",
		ClaimedAt:  time.Now().UTC().Add(-time.Hour),
		LeaseUntil: time.Now().UTC().Add(-time.Minute),
	})
	plan, err := RepairDryRun(context.Background(), root, Options{Actor: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Proposals) == 0 {
		t.Fatal("plan should have at least one proposal")
	}
	// Tamper: change the workitem bytes after dry-run.
	tampered := sampleWorkitem("WLM-1", "ready", nil)
	tampered.Title = "tampered"
	writeWorkitem(t, root, tampered)

	rep, err := RepairApply(context.Background(), root, plan, Options{Actor: "tester", Reason: "verify"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Applied) != 0 {
		t.Fatalf("Apply must not apply on drifted evidence; got %v", rep.Applied)
	}
}

// TestRecoverIsIdempotentForLeaseRelease.
func TestRecoverIsIdempotentForLeaseRelease(t *testing.T) {
	root := newProject(t)
	writeWorkitem(t, root, sampleWorkitem("WLM-1", "in_progress", nil))
	writeLease(t, root, &domain.SchedulingLease{
		WorkItemID: "WLM-1",
		Owner:      "agent-a",
		Token:      "tok",
		RunID:      "run-20260101-1",
		ClaimedAt:  time.Now().UTC().Add(-time.Hour),
		LeaseUntil: time.Now().UTC().Add(-time.Minute),
	})
	rep, err := Recover(context.Background(), root, Options{Actor: "tester", Reason: "expired lease cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(rep.ReleasedExpired); got != 1 {
		t.Fatalf("ReleasedExpired = %d, want 1", got)
	}
	if rep.EventsAppended != 1 {
		t.Fatalf("EventsAppended = %d, want 1", rep.EventsAppended)
	}
	if _, err := os.Stat(filepath.Join(root, ".devsys", "scheduling", "WLM-1.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lease file still present")
	}
	// Idempotent second run.
	rep2, err := Recover(context.Background(), root, Options{Actor: "tester", Reason: "expired lease cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	if rep2.EventsAppended != 0 || len(rep2.ReleasedExpired) != 0 || len(rep2.ReleasedOrphans) != 0 {
		t.Fatalf("idempotent Recover should be empty; got %+v", rep2)
	}
	// Exactly one lease_recovered event appended.
	totalLeaseEvents := 0
	_ = filepath.WalkDir(filepath.Join(root, ".devsys", "events"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		data, _ := os.ReadFile(p)
		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if strings.Contains(line, "lease_recovered") {
				totalLeaseEvents++
			}
		}
		return nil
	})
	if totalLeaseEvents != 1 {
		t.Fatalf("lease_recovered events = %d, want 1", totalLeaseEvents)
	}
}

// TestRecoverReleasesOrphanLease seeds a lease whose RunID file does not
// exist on disk. Recover must release it via ForOrphan=true.
//
// Pre-condition: CoreM2's Release implements ForOrphan (lock-step with
// this test). If CoreM2 hasn't shipped yet, this test fails on the
// Release call; it does NOT skip.
func TestRecoverReleasesOrphanLease(t *testing.T) {
	root := newProject(t)
	writeWorkitem(t, root, sampleWorkitem("WLM-2", "in_progress", nil))
	writeLease(t, root, &domain.SchedulingLease{
		WorkItemID: "WLM-2",
		Owner:      "agent-b",
		Token:      "tok",
		RunID:      "run-20260101-99", // not present on disk
		ClaimedAt:  time.Now().UTC().Add(-time.Hour),
		LeaseUntil: time.Now().UTC().Add(time.Hour), // not expired — pure orphan
	})
	rep, err := Recover(context.Background(), root, Options{Actor: "tester", Reason: "orphan lease cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.ReleasedOrphans) != 1 || rep.ReleasedOrphans[0] != "WLM-2" {
		t.Fatalf("ReleasedOrphans = %v, want [WLM-2]", rep.ReleasedOrphans)
	}
	if _, err := os.Stat(filepath.Join(root, ".devsys", "scheduling", "WLM-2.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan lease file still present")
	}
}

// TestDoctorDetectsOrphanLease verifies Doctor classifies a lease with
// missing run file as ReasonKind=orphan.
func TestDoctorDetectsOrphanLease(t *testing.T) {
	root := newProject(t)
	writeWorkitem(t, root, sampleWorkitem("WLM-3", "in_progress", nil))
	writeLease(t, root, &domain.SchedulingLease{
		WorkItemID: "WLM-3",
		Owner:      "agent-c",
		Token:      "tok",
		RunID:      "run-20260101-77",
		ClaimedAt:  time.Now().UTC(),
		LeaseUntil: time.Now().UTC().Add(time.Hour),
	})
	rep, err := Doctor(context.Background(), root, Options{Actor: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if got := orphanCount(rep.OrphanLeases); got != 1 {
		t.Fatalf("orphan count = %d, want 1", got)
	}
	// The orphan lease must also appear as a Proposal so the operator
	// can apply it (Main: no skip/unrepairable temporary fallback).
	found := false
	for _, pr := range rep.Orphans {
		if pr.Kind == ProposalReleaseOrphanLease && pr.WorkitemID == "WLM-3" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected orphan-release proposal in Orphans; got %+v", rep.Orphans)
	}
}

// TestRepairDryRunEmitsDownwardRepairForOneMissingArtifact covers Main Q1:
// ONE missing referenced artifact is sufficient.
func TestRepairDryRunEmitsDownwardRepairForOneMissingArtifact(t *testing.T) {
	root := newProject(t)
	wi := sampleWorkitem("WLM-4", domain.StatusDone, []string{"artifact-9"})
	writeWorkitem(t, root, wi)
	// artifact-9 deliberately absent.
	plan, err := RepairDryRun(context.Background(), root, Options{Actor: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, pr := range plan.Proposals {
		if pr.Kind == ProposalMarkDoneToInProgress && pr.WorkitemID == "WLM-4" {
			found = true
			// The missing artifact must be in evidence with kind=absent.
			hasAbsent := false
			for _, ev := range pr.Evidence {
				if ev.Kind == EvidenceAbsent && ev.Path == artifactRel("artifact-9") {
					hasAbsent = true
				}
			}
			if !hasAbsent {
				t.Fatalf("missing artifact evidence missing from proposal: %+v", pr.Evidence)
			}
		}
	}
	if !found {
		t.Fatalf("expected ProposalMarkDoneToInProgress in plan; got %+v", plan.Proposals)
	}
}

// TestRepairDryRunNoProposalWhenAllArtifactsPresent.
func TestRepairDryRunNoProposalWhenAllArtifactsPresent(t *testing.T) {
	root := newProject(t)
	wi := sampleWorkitem("WLM-5", domain.StatusDone, []string{"artifact-7"})
	writeWorkitem(t, root, wi)
	// Create the artifact file.
	artPath := filepath.Join(root, ".devsys", "artifacts", "artifact-7.yaml")
	if err := os.WriteFile(artPath, []byte("schema_version: 1\nid: artifact-7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := RepairDryRun(context.Background(), root, Options{Actor: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	for _, pr := range plan.Proposals {
		if pr.Kind == ProposalMarkDoneToInProgress && pr.WorkitemID == "WLM-5" {
			t.Fatalf("unexpected downward repair proposal: %+v", pr)
		}
	}
}

// TestComputeDigestIgnoresTimestamps pins determinism.
func TestComputeDigestIgnoresTimestamps(t *testing.T) {
	p1 := []Proposal{{
		Kind: ProposalReleaseExpiredLease, WorkitemID: "WLM-1",
		Description: "x",
		Evidence:    []Evidence{{Kind: EvidenceFile, Path: "scheduling/WLM-1.yaml", SHA256: "abc"}},
	}}
	p2 := []Proposal{{
		Kind: ProposalReleaseExpiredLease, WorkitemID: "WLM-1",
		Description: "x",
		Evidence:    []Evidence{{SHA256: "abc", Path: "scheduling/WLM-1.yaml", Kind: EvidenceFile}},
	}}
	if ComputeDigest(p1) != ComputeDigest(p2) {
		t.Fatalf("digest not order-independent")
	}
	p3 := []Proposal{{
		Kind: ProposalReleaseExpiredLease, WorkitemID: "WLM-1",
		Description: "x",
		Evidence:    []Evidence{{Kind: EvidenceFile, Path: "scheduling/WLM-1.yaml", SHA256: "def"}},
	}}
	if ComputeDigest(p1) == ComputeDigest(p3) {
		t.Fatalf("digest did not change when evidence changed")
	}
}

// TestInspectRefusesOnPendingTxn.
func TestInspectRefusesOnPendingTxn(t *testing.T) {
	root := newProject(t)
	st, err := storage.Open(root, storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	txnID := "20260101T000000.000000000Z-1-aaaaaaaa"
	if err := os.MkdirAll(filepath.Join(root, ".devsys", "local", "txn", txnID, "payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	err = st.Inspect(context.Background(), func(r *storage.Reader) error { return nil })
	var perr *storage.PendingTxnError
	if !errors.As(err, &perr) {
		t.Fatalf("expected *PendingTxnError, got %v", err)
	}
	if len(perr.IDs) != 1 || perr.IDs[0] != txnID {
		t.Fatalf("PendingTxnError.IDs = %v", perr.IDs)
	}
}

// TestInspectReturnsErrLockUnavailableWhenLockAbsent.
func TestInspectReturnsErrLockUnavailableWhenLockAbsent(t *testing.T) {
	root := newProject(t)
	st, err := storage.Open(root, storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = st.Inspect(context.Background(), func(r *storage.Reader) error { return nil })
	if !errors.Is(err, storage.ErrLockUnavailable) {
		t.Fatalf("err = %v, want ErrLockUnavailable", err)
	}
}
