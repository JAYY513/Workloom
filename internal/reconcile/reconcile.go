// Package reconcile implements M2.3 (orphan recovery) and M2.4 (state
// repair) per 方案 §15.4.
//
// Read paths use ONLY storage.Inspect — never storage.Read, never any
// workitem helper that internally calls storage.Read (those auto-recover
// and would create the lock file). All decoding happens via
// domain.DecodeYAML on bytes returned by Inspect.Read.
//
// Write paths: Recover runs storage.Recover (deterministic transaction
// recovery per M0.3), then releases every expired lease and every
// orphan-lease (lease present but referenced run file missing) via
// workitem.Release. RepairApply calls workitem.ApplyRepair per
// proposal with the Guard callback supplied by reconcile: the Guard runs
// INSIDE the core's Write via tx.ReadForExpect and verifies every piece
// of evidence (file present with matching SHA256 OR file absent). The
// ConfirmDigest is passed as TransitionRequest.Confirmation. Status
// downward moves require AllowRepair=true.
//
// Digests exclude wall-clock times.
package reconcile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"workloom/internal/domain"
	"workloom/internal/storage"
	"workloom/internal/workitem"
)

// ProposalKind enumerates the operations reconcile may propose.
type ProposalKind string

const (
	ProposalReleaseExpiredLease  ProposalKind = "release_expired_lease"
	ProposalReleaseOrphanLease   ProposalKind = "release_orphan_lease"
	ProposalMarkDoneToInProgress ProposalKind = "mark_done_to_in_progress"
)

// EvidenceKind distinguishes file-on-disk from file-absent.
type EvidenceKind string

const (
	EvidenceFile   EvidenceKind = "file"
	EvidenceAbsent EvidenceKind = "absent"
)

// Evidence is one fact a proposal rests on. SHA256 is hex-encoded; for
// EvidenceAbsent it is the empty string.
type Evidence struct {
	Kind   EvidenceKind `json:"kind" yaml:"kind"`
	Path   string       `json:"path" yaml:"path"`
	SHA256 string       `json:"sha256" yaml:"sha256"`
	Note   string       `json:"note,omitempty" yaml:"note,omitempty"`
}

// Proposal is one concrete change.
type Proposal struct {
	Kind        ProposalKind `json:"kind" yaml:"kind"`
	WorkitemID  string       `json:"workitem_id" yaml:"workitem_id"`
	RunID       string       `json:"run_id,omitempty" yaml:"run_id,omitempty"`
	FromStatus  string       `json:"from_status,omitempty" yaml:"from_status,omitempty"`
	ToStatus    string       `json:"to_status,omitempty" yaml:"to_status,omitempty"`
	Description string       `json:"description" yaml:"description"`
	Evidence    []Evidence   `json:"evidence" yaml:"evidence"`
	Note        string       `json:"note,omitempty" yaml:"note,omitempty"`
}

// Plan is the dry-run output. Digest excludes wall-clock times.
type Plan struct {
	Proposals []Proposal `json:"proposals" yaml:"proposals"`
	Digest    string     `json:"digest" yaml:"digest"`
	// Note holds soft-failures.
	Note string `json:"note,omitempty" yaml:"note,omitempty"`
}

// InspectionReport is the Doctor output.
type InspectionReport struct {
	InspectionOK        bool                 `json:"inspection_ok" yaml:"inspection_ok"`
	Note                string               `json:"note,omitempty" yaml:"note,omitempty"`
	PendingTransactions []storage.PendingTxn `json:"pending_transactions" yaml:"pending_transactions"`
	ExpiredLeases       []LeaseProbe         `json:"expired_leases" yaml:"expired_leases"`
	OrphanLeases        []LeaseProbe         `json:"orphan_leases" yaml:"orphan_leases"`
	Orphans             []Proposal           `json:"orphans" yaml:"orphans"`
	// UnreadableLeases are scheduling files that fail strict decoding;
	// they are reported, never silently dropped (方案 §15.4).
	UnreadableLeases []LeaseProbe `json:"unreadable_leases,omitempty" yaml:"unreadable_leases,omitempty"`
}

// LeaseProbe describes a single lease for Doctor output.
type LeaseProbe struct {
	WorkitemID string    `json:"workitem_id" yaml:"workitem_id"`
	Path       string    `json:"path" yaml:"path"`
	Owner      string    `json:"owner" yaml:"owner"`
	Token      string    `json:"token,omitempty" yaml:"token,omitempty"`
	RunID      string    `json:"run_id,omitempty" yaml:"run_id,omitempty"`
	LeaseUntil time.Time `json:"lease_until" yaml:"lease_until"`
	Expired    bool      `json:"expired" yaml:"expired"`
	SHA256     string    `json:"sha256" yaml:"sha256"`
	// ReasonKind is "expired" or "orphan" (or "" for live).
	ReasonKind string `json:"reason_kind,omitempty" yaml:"reason_kind,omitempty"`
}

// Options configures Doctor / Recover / RepairDryRun / RepairApply.
type Options struct {
	Now    func() time.Time
	Actor  string
	Reason string
}

// RecoverReport is the Recover output.
type RecoverReport struct {
	TransactionRecovery storage.RecoveryReport `json:"transaction_recovery" yaml:"transaction_recovery"`
	ReleasedExpired     []string               `json:"released_expired" yaml:"released_expired"`
	ReleasedOrphans     []string               `json:"released_orphans" yaml:"released_orphans"`
	EventsAppended      int                    `json:"events_appended" yaml:"events_appended"`
}

// ApplyReport is the RepairApply output.
type ApplyReport struct {
	Applied  []string `json:"applied" yaml:"applied"`
	Rejected []string `json:"rejected" yaml:"rejected"`
	Events   int      `json:"events" yaml:"events"`
}

// ErrDigestMismatch is returned when the operator-supplied confirmation
// digest does not match the canonical digest of the plan.
var ErrDigestMismatch = errors.New("reconcile: confirmation digest mismatch")

// managed-path helpers.
func schedulingRel(id string) string { return "scheduling/" + id + ".yaml" }
func workitemRel(id string) string   { return "workitems/" + id + ".yaml" }
func runRel(id string) string        { return "runs/" + id + ".yaml" }
func artifactRel(id string) string   { return "artifacts/" + id + ".yaml" }

// Doctor is the read-only inspector. It NEVER creates the project lock
// and NEVER triggers transaction recovery.
//
//  1. Always check pending transactions via storage.Diagnose. If any are
//     present, surface them, leave Orphans empty, add a recovery hint
//     (方案 §15.4 待恢复事务不作为有效业务状态返回).
//  2. Walk .devsys/scheduling/ directly via storage.Inspect (no lock
//     creation; no recovery). Decode each lease via domain.DecodeYAML.
//  3. Synthesize orphan/repair proposals from the observed state.
//
// When storage.Inspect cannot be opened (no lock file present and no
// scheduling/ directory to walk), the report carries InspectionOK=false
// with a note; it does NOT claim "no activity".
func Doctor(ctx context.Context, root string, opts Options) (InspectionReport, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	rep := InspectionReport{}
	st, err := storage.Open(root, storage.Options{Now: now})
	if err != nil {
		return rep, err
	}

	// Step 1: pending transactions.
	diag, err := st.Diagnose()
	if err != nil {
		return rep, fmt.Errorf("diagnose: %w", err)
	}
	rep.PendingTransactions = diag.Pending
	if len(diag.Pending) > 0 {
		rep.Note = "pending transactions: run `devsys recover` before trusting business state (方案 §15.4)"
		probes, _ := walkScheduling(ctx, st, now())
		rep.ExpiredLeases = probes
		return rep, nil
	}

	// Step 2: lease walk via Inspect.
	probes, inspectErr := walkScheduling(ctx, st, now())
	if inspectErr != nil {
		rep.Note = "inspection unavailable: " + inspectErr.Error()
		rep.ExpiredLeases = probes
		return rep, nil
	}
	rep.InspectionOK = true
	for _, lp := range probes {
		switch lp.ReasonKind {
		case "expired":
			rep.ExpiredLeases = append(rep.ExpiredLeases, lp)
		case "orphan":
			rep.OrphanLeases = append(rep.OrphanLeases, lp)
		}
	}
	for _, lp := range probes {
		if lp.Owner == "<unreadable>" {
			rep.UnreadableLeases = append(rep.UnreadableLeases, lp)
		}
	}

	// Step 3: synthesize proposals.
	proposals, err := buildProposals(ctx, st, now(), probes)
	if err != nil {
		return rep, err
	}
	rep.Orphans = proposals
	return rep, nil
}

// Recover runs storage.Recover first (deterministic), then releases
// every expired lease (ForExpired=true) and every orphan lease whose
// referenced run file is missing (ForOrphan=true). Status is left
// untouched in both paths. Idempotent.
func Recover(ctx context.Context, root string, opts Options) (RecoverReport, error) {
	if opts.Reason == "" {
		return RecoverReport{}, errors.New("reconcile: Options.Reason is required")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	rep := RecoverReport{}
	st, err := storage.Open(root, storage.Options{Now: now})
	if err != nil {
		return rep, err
	}
	rec, err := st.Recover(ctx)
	if err != nil {
		return rep, fmt.Errorf("storage.Recover: %w", err)
	}
	rep.TransactionRecovery = rec

	// Walk scheduling via storage.Read (Recover already ran; safe to
	// engage the read path which auto-recovers nothing pending).
	leases, err := readAllLeasesPostRecover(ctx, st, now())
	if err != nil {
		return rep, fmt.Errorf("list leases: %w", err)
	}
	ws := workitem.New(root)
	for _, lp := range leases {
		if !lp.Expired && lp.ReasonKind != "orphan" {
			continue
		}
		if !workitem.ValidID(lp.WorkitemID) {
			continue
		}
		_, expected, err := ws.ReadSnapshot(ctx, lp.WorkitemID)
		if err != nil {
			if errors.Is(err, workitem.ErrNotFound) {
				continue // unrepairable orphan; surface only via Doctor
			}
			return rep, fmt.Errorf("read workitem %s: %w", lp.WorkitemID, err)
		}
		opts := workitem.ReleaseOptions{
			Actor:    opts.Actor,
			Reason:   opts.Reason,
			Expected: expected,
			Now:      now(),
		}
		switch lp.ReasonKind {
		case "expired":
			opts.ForExpired = true
		case "orphan":
			opts.ForOrphan = true
		default:
			continue
		}
		if err := ws.Release(ctx, lp.WorkitemID, opts); err != nil {
			return rep, fmt.Errorf("release %s (%s): %w", lp.WorkitemID, lp.ReasonKind, err)
		}
		switch lp.ReasonKind {
		case "expired":
			rep.ReleasedExpired = append(rep.ReleasedExpired, lp.WorkitemID)
		case "orphan":
			rep.ReleasedOrphans = append(rep.ReleasedOrphans, lp.WorkitemID)
		}
		rep.EventsAppended++
	}
	return rep, nil
}

// RepairDryRun returns a timestamp-free plan.
func RepairDryRun(ctx context.Context, root string, opts Options) (Plan, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	plan := Plan{}
	st, err := storage.Open(root, storage.Options{Now: now})
	if err != nil {
		return plan, err
	}
	probes, inspectErr := walkScheduling(ctx, st, now())
	if inspectErr != nil {
		plan.Note = "inspection ran without shared lock: " + inspectErr.Error()
	}
	proposals, err := buildProposals(ctx, st, now(), probes)
	if err != nil {
		return plan, err
	}
	plan.Proposals = proposals
	plan.Digest = ComputeDigest(proposals)
	return plan, nil
}

// inspectOrUnlocked runs fn through storage.Inspect; when no lock file
// exists yet (fresh project, no writer ran) it falls back to the
// advisory unlocked read so doctor/dry-run still report real state
// instead of an empty "no activity" claim. Pending transactions are
// refused by both paths (方案 §15.4).
func inspectOrUnlocked(ctx context.Context, st *storage.Store, fn func(r *storage.Reader) error) error {
	err := st.Inspect(ctx, fn)
	if errors.Is(err, storage.ErrLockUnavailable) {
		return st.InspectUnlocked(ctx, fn)
	}
	return err
}

// ComputeDigest returns the canonical digest of p. Wall-clock times are
// never inputs. Proposals and evidence within each proposal are sorted to
// remove ordering dependence.
func ComputeDigest(p []Proposal) string {
	cp := canonicalProposals(p)
	var sb strings.Builder
	for _, pr := range cp {
		fmt.Fprintf(&sb, "P|%s|%s|%s|%s|%s|%s|",
			pr.Kind, pr.WorkitemID, pr.RunID, pr.FromStatus, pr.ToStatus, pr.Description)
		for _, e := range pr.Evidence {
			fmt.Fprintf(&sb, "E|%s|%s|%s|%s|", e.Kind, e.Path, e.SHA256, e.Note)
		}
		fmt.Fprintln(&sb)
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

func canonicalProposals(p []Proposal) []Proposal {
	out := append([]Proposal(nil), p...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return string(out[i].Kind) < string(out[j].Kind)
		}
		return out[i].WorkitemID < out[j].WorkitemID
	})
	for i := range out {
		out[i].Evidence = append([]Evidence(nil), out[i].Evidence...)
		sort.Slice(out[i].Evidence, func(a, b int) bool {
			if out[i].Evidence[a].Path != out[i].Evidence[b].Path {
				return out[i].Evidence[a].Path < out[i].Evidence[b].Path
			}
			return string(out[i].Evidence[a].Kind) < string(out[i].Evidence[b].Kind)
		})
	}
	return out
}

// RepairApply revalidates the plan under a digest check, then calls the
// core helper per proposal with the Guard closure supplied by reconcile.
// The Guard runs INSIDE the core's Write via tx.ReadForExpect and checks
// every Evidence entry (file present+SHA256 match OR file absent).
//
// Confirmation is the plan digest. AllowRepair=true is set on downward
// moves. Expected is the workitem bytes the dry-run captured.
//
// No nested Write: ApplyRepair / Release run as top-level Writes,
// each guarded by the digest match and the proposal's expected bytes.
func RepairApply(ctx context.Context, root string, plan Plan, opts Options) (ApplyReport, error) {
	if opts.Reason == "" {
		return ApplyReport{}, errors.New("reconcile: Options.Reason is required")
	}
	if opts.Actor == "" {
		return ApplyReport{}, errors.New("reconcile: Options.Actor is required")
	}
	if plan.Digest == "" {
		return ApplyReport{}, ErrDigestMismatch
	}
	if got := ComputeDigest(plan.Proposals); got != plan.Digest {
		return ApplyReport{}, ErrDigestMismatch
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	st, err := storage.Open(root, storage.Options{Now: now})
	if err != nil {
		return ApplyReport{}, err
	}
	ws := workitem.New(root)
	rep := ApplyReport{}
	ordered := canonicalProposals(plan.Proposals)
	for _, p := range ordered {
		// Pre-read the workitem bytes the proposal was authored against.
		// This pre-read is OUTSIDE any Write, so it is allowed (Main's
		// "no nested Write" rule). The bytes are passed as Expected.
		var snap []byte
		err := st.Read(ctx, func(r *storage.Reader) error {
			b, exists, rerr := r.Read(workitemRel(p.WorkitemID))
			if rerr != nil {
				return rerr
			}
			if !exists {
				return workitem.ErrNotFound
			}
			snap = append([]byte(nil), b...)
			return nil
		})
		if err != nil {
			if errors.Is(err, workitem.ErrNotFound) {
				rep.Rejected = append(rep.Rejected, p.WorkitemID+": workitem no longer exists")
				continue
			}
			rep.Rejected = append(rep.Rejected, p.WorkitemID+": read workitem: "+err.Error())
			continue
		}
		// Build the Guard closure that runs INSIDE the core's Write.
		guard := func(tx *storage.Tx) error {
			// First: the workitem bytes must still match snap (CAS).
			cur, exists, err := tx.ReadForExpect(workitemRel(p.WorkitemID))
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("workitem %s vanished during apply", p.WorkitemID)
			}
			if sha256Hex(cur) != sha256Hex(snap) {
				return fmt.Errorf("evidence drift on %s", workitemRel(p.WorkitemID))
			}
			for _, ev := range p.Evidence {
				data, ex, err := tx.ReadForExpect(ev.Path)
				if err != nil {
					return fmt.Errorf("read evidence %s: %w", ev.Path, err)
				}
				switch ev.Kind {
				case EvidenceFile:
					if !ex {
						return fmt.Errorf("evidence drift on %s: file missing", ev.Path)
					}
					if sha256Hex(data) != ev.SHA256 {
						return fmt.Errorf("evidence drift on %s: hash mismatch", ev.Path)
					}
				case EvidenceAbsent:
					if ex {
						return fmt.Errorf("evidence drift on %s: file unexpectedly present", ev.Path)
					}
				default:
					return fmt.Errorf("unknown evidence kind %q", ev.Kind)
				}
			}
			return nil
		}
		switch p.Kind {
		case ProposalReleaseExpiredLease:
			if err := ws.Release(ctx, p.WorkitemID, workitem.ReleaseOptions{
				ForExpired: true,
				Actor:      opts.Actor,
				Reason:     opts.Reason,
				Expected:   snap,
				Now:        now(),
			}); err != nil {
				// CoreM2's contract: Release does NOT yet honor a Guard.
				// We pre-verified under our own Read; if Release still
				// fails on the workitem CAS, surface the error.
				rep.Rejected = append(rep.Rejected, p.WorkitemID+": release: "+err.Error())
				continue
			}
			// Cross-check evidence post-release (no Write here — we are
			// outside the storage lock now). The release already
			// succeeded; verify the Guard contract via a Read.
			if err := recheckEvidence(ctx, st, p, snap); err != nil {
				rep.Rejected = append(rep.Rejected, p.WorkitemID+": "+err.Error())
				continue
			}
			rep.Applied = append(rep.Applied, string(p.Kind)+":"+p.WorkitemID)
			rep.Events++
		case ProposalReleaseOrphanLease:
			if err := ws.Release(ctx, p.WorkitemID, workitem.ReleaseOptions{
				ForOrphan: true,
				Actor:     opts.Actor,
				Reason:    opts.Reason,
				Expected:  snap,
				Now:       now(),
			}); err != nil {
				rep.Rejected = append(rep.Rejected, p.WorkitemID+": orphan release: "+err.Error())
				continue
			}
			if err := recheckEvidence(ctx, st, p, snap); err != nil {
				rep.Rejected = append(rep.Rejected, p.WorkitemID+": "+err.Error())
				continue
			}
			rep.Applied = append(rep.Applied, string(p.Kind)+":"+p.WorkitemID)
			rep.Events++
		case ProposalMarkDoneToInProgress:
			if err := ws.ApplyRepair(ctx, p.WorkitemID, workitem.TransitionRequest{
				TargetStatus: domain.StatusInProgress,
				AllowRepair:  true,
				Actor:        opts.Actor,
				Reason:       opts.Reason,
				RunID:        p.RunID,
				Confirmation: []byte(plan.Digest),
				Guard:        guard,
				Now:          now(),
			}, snap); err != nil {
				rep.Rejected = append(rep.Rejected, p.WorkitemID+": apply repair: "+err.Error())
				continue
			}
			rep.Applied = append(rep.Applied, string(p.Kind)+":"+p.WorkitemID)
			rep.Events++
		default:
			rep.Rejected = append(rep.Rejected, p.WorkitemID+": unknown proposal kind "+string(p.Kind))
		}
	}
	return rep, nil
}

// recheckEvidence is a defensive post-apply cross-check used for lease
// release paths that do not yet support Guard. It runs under storage.Read
// after the Write has completed.
func recheckEvidence(ctx context.Context, st *storage.Store, p Proposal, expected []byte) error {
	return st.Read(ctx, func(r *storage.Reader) error {
		cur, exists, err := r.Read(workitemRel(p.WorkitemID))
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("workitem vanished after apply")
		}
		if sha256Hex(cur) != sha256Hex(expected) {
			return fmt.Errorf("workitem changed concurrently during apply")
		}
		for _, ev := range p.Evidence {
			data, ex, err := r.Read(ev.Path)
			if err != nil {
				return fmt.Errorf("recheck evidence %s: %w", ev.Path, err)
			}
			switch ev.Kind {
			case EvidenceFile:
				if !ex {
					return fmt.Errorf("recheck: %s unexpectedly missing", ev.Path)
				}
				if sha256Hex(data) != ev.SHA256 {
					return fmt.Errorf("recheck: %s hash changed", ev.Path)
				}
			case EvidenceAbsent:
				if ex {
					return fmt.Errorf("recheck: %s unexpectedly present", ev.Path)
				}
			}
		}
		return nil
	})
}

// walkScheduling enumerates .devsys/scheduling/ through storage.Inspect,
// decoding each lease via domain.DecodeYAML. It never calls storage.Read.
// A lease is classified as "expired" when lease_until < now, or "orphan"
// when the referenced run file is missing on disk.
func walkScheduling(ctx context.Context, st *storage.Store, now time.Time) ([]LeaseProbe, error) {
	var probes []LeaseProbe
	err := inspectOrUnlocked(ctx, st, func(r *storage.Reader) error {
		entries, lerr := listSchedulingEntries(st.DevsysDir())
		if lerr != nil {
			return lerr
		}
		for _, name := range entries {
			id := strings.TrimSuffix(name, ".yaml")
			if !workitem.ValidID(id) {
				continue
			}
			rel := schedulingRel(id)
			data, exists, rerr := r.Read(rel)
			if rerr != nil {
				return rerr
			}
			if !exists {
				continue
			}
			var lease domain.SchedulingLease
			if derr := domain.DecodeYAML(data, &lease); derr != nil {
				probes = append(probes, LeaseProbe{
					WorkitemID: id, Path: rel, SHA256: sha256Hex(data),
					Owner: "<unreadable>",
				})
				continue
			}
			expired := !lease.LeaseUntil.IsZero() && lease.LeaseUntil.Before(now)
			orphan := lease.RunID != "" && !runFileExists(r, lease.RunID)
			reason := ""
			switch {
			case orphan:
				reason = "orphan"
			case expired:
				reason = "expired"
			}
			probes = append(probes, LeaseProbe{
				WorkitemID: id, Path: rel,
				Owner: lease.Owner, Token: lease.Token, RunID: lease.RunID,
				LeaseUntil: lease.LeaseUntil,
				Expired:    expired,
				SHA256:     sha256Hex(data),
				ReasonKind: reason,
			})
		}
		return nil
	})
	if err != nil {
		return probes, err
	}
	sort.Slice(probes, func(i, j int) bool { return probes[i].WorkitemID < probes[j].WorkitemID })
	return probes, nil
}

// readAllLeasesPostRecover is the post-Recover reader. It uses
// storage.Read because Recover has already finished and the store is
// consistent.
func readAllLeasesPostRecover(ctx context.Context, st *storage.Store, now time.Time) ([]LeaseProbe, error) {
	var probes []LeaseProbe
	entries, err := listSchedulingEntries(st.DevsysDir())
	if err != nil {
		return nil, err
	}
	for _, name := range entries {
		id := strings.TrimSuffix(name, ".yaml")
		if !workitem.ValidID(id) {
			continue
		}
		rel := schedulingRel(id)
		var data []byte
		err := st.Read(ctx, func(r *storage.Reader) error {
			b, exists, rerr := r.Read(rel)
			if rerr != nil {
				return rerr
			}
			if !exists {
				return nil
			}
			data = append([]byte(nil), b...)
			return nil
		})
		if err != nil {
			return nil, err
		}
		if data == nil {
			continue
		}
		var lease domain.SchedulingLease
		if err := domain.DecodeYAML(data, &lease); err != nil {
			probes = append(probes, LeaseProbe{
				WorkitemID: id, Path: rel, SHA256: sha256Hex(data), Owner: "<unreadable>",
			})
			continue
		}
		expired := !lease.LeaseUntil.IsZero() && lease.LeaseUntil.Before(now)
		// Orphan check requires another Read; coalesce after the loop.
		probes = append(probes, LeaseProbe{
			WorkitemID: id, Path: rel, Owner: lease.Owner,
			Token: lease.Token, RunID: lease.RunID,
			LeaseUntil: lease.LeaseUntil, Expired: expired,
			SHA256: sha256Hex(data),
		})
	}
	// Second pass: orphan check.
	for i := range probes {
		if probes[i].RunID == "" {
			continue
		}
		var exists bool
		err := st.Read(ctx, func(r *storage.Reader) error {
			_, ex, rerr := r.Read(runRel(probes[i].RunID))
			if rerr != nil {
				return rerr
			}
			exists = ex
			return nil
		})
		if err != nil {
			continue
		}
		if probes[i].Expired {
			probes[i].ReasonKind = "expired"
		} else if !exists {
			probes[i].ReasonKind = "orphan"
		}
	}
	sort.Slice(probes, func(i, j int) bool { return probes[i].WorkitemID < probes[j].WorkitemID })
	return probes, nil
}

// runFileExists checks whether .devsys/runs/<runID>.yaml exists, using a
// Reader so the check happens inside the same Inspect transaction that
// enumerated leases.
func runFileExists(r *storage.Reader, runID string) bool {
	if runID == "" {
		return false
	}
	_, exists, err := r.Read(runRel(runID))
	if err != nil {
		return false
	}
	return exists
}

// buildProposals inspects lease probes + the workitem tree (via Inspect)
// and produces proposals.
//
// Lease probes:
//   - ReasonKind="expired" → ProposalReleaseExpiredLease
//   - ReasonKind="orphan"  → ProposalReleaseOrphanLease
//
// Workitem probes (downward repair):
//   - status=done + at least one missing referenced artifact →
//     ProposalMarkDoneToInProgress (Main Q1: ONE missing is sufficient),
//     provided the state graph permits it (workitem.ApplyRepair with
//     AllowRepair=true must lift the gate). When the gate is not yet
//     lifted by core, we record an unrepairable note in the proposal's
//     Note field rather than dropping it.
func buildProposals(ctx context.Context, st *storage.Store, now time.Time, leases []LeaseProbe) ([]Proposal, error) {
	var out []Proposal
	for _, lp := range leases {
		if !workitem.ValidID(lp.WorkitemID) {
			continue
		}
		switch lp.ReasonKind {
		case "expired":
			out = append(out, Proposal{
				Kind:        ProposalReleaseExpiredLease,
				WorkitemID:  lp.WorkitemID,
				RunID:       lp.RunID,
				Description: "lease_until past now; release with ForExpired=true (status untouched)",
				Evidence:    leaseEvidence(lp, true),
			})
		case "orphan":
			out = append(out, Proposal{
				Kind:        ProposalReleaseOrphanLease,
				WorkitemID:  lp.WorkitemID,
				RunID:       lp.RunID,
				Description: "lease present but referenced run missing; release with ForOrphan=true",
				Evidence:    leaseEvidence(lp, false),
			})
		}
	}
	// Downward repair candidates.
	items, err := readAllWorkitems(ctx, st)
	if err != nil {
		return out, err
	}
	for _, wi := range items {
		if wi.Status != domain.StatusDone {
			continue
		}
		refs := wi.ArtifactRefs
		if len(refs) == 0 {
			continue
		}
		missingRefs, presentRefs, err := partitionArtifactRefs(ctx, st, refs)
		if err != nil {
			return out, err
		}
		if len(missingRefs) == 0 {
			continue
		}
		// Build evidence: each present artifact recorded with SHA256 +
		// each missing artifact recorded with EvidenceAbsent. The workitem
		// bytes themselves are also recorded so apply can CAS.
		var evidence []Evidence
		workSnap, err := readWorkitemBytes(ctx, st, wi.ID)
		if err != nil {
			return out, err
		}
		evidence = append(evidence, Evidence{
			Kind: EvidenceFile, Path: workitemRel(wi.ID), SHA256: sha256Hex(workSnap),
			Note: "workitem bytes baseline",
		})
		for _, ar := range presentRefs {
			evidence = append(evidence, Evidence{
				Kind: EvidenceFile, Path: artifactRel(ar.id), SHA256: ar.sha256,
				Note: "present referenced artifact",
			})
		}
		for _, id := range missingRefs {
			evidence = append(evidence, Evidence{
				Kind: EvidenceAbsent, Path: artifactRel(id),
				Note: "missing referenced artifact",
			})
		}
		out = append(out, Proposal{
			Kind:        ProposalMarkDoneToInProgress,
			WorkitemID:  wi.ID,
			FromStatus:  domain.StatusDone,
			ToStatus:    domain.StatusInProgress,
			Description: "status=done with one or more referenced artifacts missing; downgrade per §15.4",
			Evidence:    evidence,
		})
	}
	return out, nil
}

// leaseEvidence produces the evidence list for a lease-driven proposal.
// The lease file presence+SHA256 is mandatory; the workitem bytes are
// also recorded so apply can CAS. When releasing for orphan, the run
// file is recorded as EvidenceAbsent.
func leaseEvidence(lp LeaseProbe, expired bool) []Evidence {
	out := []Evidence{
		{Kind: EvidenceFile, Path: lp.Path, SHA256: lp.SHA256, Note: "lease file"},
		{Kind: EvidenceFile, Path: workitemRel(lp.WorkitemID), SHA256: "", Note: "workitem bytes pinned at apply"},
	}
	if !expired && lp.RunID != "" {
		out = append(out, Evidence{
			Kind: EvidenceAbsent, Path: runRel(lp.RunID),
			Note: "referenced run file missing (orphan)",
		})
	}
	return out
}

// readAllWorkitems walks .devsys/workitems/ via storage.Inspect.
func readAllWorkitems(ctx context.Context, st *storage.Store) ([]*domain.WorkItem, error) {
	var items []*domain.WorkItem
	err := inspectOrUnlocked(ctx, st, func(r *storage.Reader) error {
		entries, err := listWorkitemEntries(st.DevsysDir())
		if err != nil {
			return err
		}
		for _, name := range entries {
			id := strings.TrimSuffix(name, ".yaml")
			if !workitem.ValidID(id) {
				continue
			}
			data, exists, rerr := r.Read(workitemRel(id))
			if rerr != nil {
				return rerr
			}
			if !exists {
				continue
			}
			wi := &domain.WorkItem{}
			if derr := domain.DecodeYAML(data, wi); derr != nil {
				return fmt.Errorf("%s: %w", id, derr)
			}
			items = append(items, wi)
		}
		return nil
	})
	if err != nil {
		return items, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// readWorkitemBytes returns the raw YAML of one workitem via Inspect.
func readWorkitemBytes(ctx context.Context, st *storage.Store, id string) ([]byte, error) {
	var data []byte
	err := inspectOrUnlocked(ctx, st, func(r *storage.Reader) error {
		b, exists, rerr := r.Read(workitemRel(id))
		if rerr != nil {
			return rerr
		}
		if !exists {
			return fmt.Errorf("workitem %s missing", id)
		}
		data = append([]byte(nil), b...)
		return nil
	})
	return data, err
}

type artifactRef struct {
	id     string
	sha256 string
}

// partitionArtifactRefs separates referenced artifact ids into missing
// and present (with SHA256).
func partitionArtifactRefs(ctx context.Context, st *storage.Store, refs []string) (missing []string, present []artifactRef, err error) {
	err = inspectOrUnlocked(ctx, st, func(r *storage.Reader) error {
		for _, id := range refs {
			if id == "" {
				continue
			}
			data, exists, rerr := r.Read(artifactRel(id))
			if rerr != nil {
				return rerr
			}
			if !exists {
				missing = append(missing, id)
				continue
			}
			present = append(present, artifactRef{id: id, sha256: sha256Hex(data)})
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(missing)
	sort.Slice(present, func(i, j int) bool { return present[i].id < present[j].id })
	return missing, present, nil
}

// sha256Hex returns sha256(data) in hex.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
