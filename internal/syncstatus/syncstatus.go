// Package syncstatus implements the read-only inspection behind
// `workloom sync status` (实施计划 M8.1, 方案 §14.4).
//
// It answers one question: is this device ready to hand the project to
// another device? Every check is read-only — no git fetch, no lock
// creation, no recovery, no writes of any kind. A handoff that reports
// ready still requires the operator to have stopped the old device first:
// sync status inspects state, it cannot prove another machine stopped
// writing (方案 §14.4).
package syncstatus

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/JAYY513/Workloom/internal/knowledge"
	"github.com/JAYY513/Workloom/internal/reconcile"
	"github.com/JAYY513/Workloom/internal/storage"
)

// Blocker codes are stable for script consumption.
const (
	CodePendingTransactions  = "pending-transactions"
	CodeUnmergedPaths        = "unmerged-paths"
	CodeMergeInProgress      = "merge-in-progress"
	CodeActiveLeases         = "active-leases"
	CodeExpiredLeases        = "expired-leases"
	CodeOrphanLeases         = "orphan-leases"
	CodeUnreadableLeases     = "unreadable-leases"
	CodeUncommittedDevsys    = "uncommitted-devsys"
	CodeDivergedFromUpstream = "diverged-from-upstream"
	CodeNoUpstream           = "no-upstream"
)

// Blocker is one reason the handoff is not ready.
type Blocker struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// Lease names one scheduling lease the report considered.
type Lease struct {
	WorkitemID string `json:"workitem_id"`
	Owner      string `json:"owner"`
	RunID      string `json:"run_id,omitempty"`
	Kind       string `json:"kind"`
}

// Status is the handoff verdict.
type Status struct {
	Branch   string `json:"branch"`
	Commit   string `json:"commit"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	// Unmerged lists conflicted paths git could not reconcile.
	Unmerged []string `json:"unmerged"`
	// MergeWork lists in-progress merge machinery (MERGE_HEAD, ...).
	MergeWork []string `json:"merge_work,omitempty"`
	// UncommittedDevsys tracks .devsys/ paths with uncommitted changes;
	// UncommittedOther tracks everything else (reported, not blocking).
	UncommittedDevsys []string `json:"uncommitted_devsys"`
	UncommittedOther  []string `json:"uncommitted_other"`
	// Pending names transactions awaiting recovery (方案 §15.4).
	Pending []string `json:"pending_transactions,omitempty"`
	// LeasesDeferred is true when pending transactions forced the lease
	// audit to be skipped: the snapshot would be partial, so no lease
	// claim — active or clear — is made.
	LeasesDeferred bool      `json:"leases_deferred,omitempty"`
	Leases         []Lease   `json:"leases,omitempty"`
	HandoffReady   bool      `json:"handoff_ready"`
	Blockers       []Blocker `json:"blockers"`
	// Note carries soft-failures that are reported, never hidden.
	Note string `json:"note,omitempty"`
}

// Check inspects root and returns the handoff verdict. It writes nothing:
// git is only ever read, storage is only ever diagnosed or inspected, and
// no fetch touches the network.
func Check(ctx context.Context, root string) (Status, error) {
	var st Status
	st.Blockers = []Blocker{}
	block := func(code, detail string) {
		st.Blockers = append(st.Blockers, Blocker{Code: code, Detail: detail})
	}

	commit, branch, err := knowledge.Head(root)
	if err != nil {
		return Status{}, err
	}
	st.Commit, st.Branch = commit, branch

	upstream, err := knowledge.Upstream(root)
	switch {
	case err == nil:
		st.Upstream = upstream
		ahead, behind, err := knowledge.AheadBehind(root)
		if err != nil {
			return Status{}, err
		}
		st.Ahead, st.Behind = ahead, behind
		if ahead > 0 || behind > 0 {
			block(CodeDivergedFromUpstream,
				"local and upstream have diverged: run `git fetch`, then push or pull before handing off")
		}
	case errors.Is(err, knowledge.ErrNoUpstream):
		// No upstream means there is nothing to hand off through.
		block(CodeNoUpstream, "the current branch tracks no upstream: set one before handing off")
	default:
		return Status{}, err
	}

	entries, err := knowledge.PorcelainStatus(root)
	if err != nil {
		return Status{}, err
	}
	var (
		unmerged, devsys, other []string
	)
	for _, e := range entries {
		if e.Unmerged {
			unmerged = append(unmerged, e.Path)
			continue
		}
		rel := strings.TrimPrefix(e.Path, "./")
		if rel == ".devsys" || strings.HasPrefix(rel, ".devsys/") {
			devsys = append(devsys, e.Path)
		} else {
			other = append(other, e.Path)
		}
	}
	st.Unmerged = sortedOrEmpty(unmerged)
	st.UncommittedDevsys = sortedOrEmpty(devsys)
	st.UncommittedOther = sortedOrEmpty(other)
	if len(unmerged) > 0 {
		block(CodeUnmergedPaths, "unmerged paths need a human or `workloom repair`: "+strings.Join(st.Unmerged, ", "))
	}
	if len(devsys) > 0 {
		block(CodeUncommittedDevsys, "uncommitted .devsys/ changes: commit them with a `chore(devsys):` prefix before handing off")
	}

	mergeWork, err := knowledge.MergeHeads(root)
	if err != nil {
		return Status{}, err
	}
	st.MergeWork = mergeWork
	if len(mergeWork) > 0 {
		block(CodeMergeInProgress, "unfinished merge machinery ("+strings.Join(mergeWork, ", ")+"): finish or abort the merge first")
	}

	stor, err := storage.Open(root, storage.Options{})
	if err != nil {
		return Status{}, err
	}
	diag, err := stor.Diagnose()
	if err != nil {
		return Status{}, err
	}
	for _, p := range diag.Pending {
		st.Pending = append(st.Pending, p.ID)
	}
	sort.Strings(st.Pending)
	if len(st.Pending) > 0 {
		block(CodePendingTransactions,
			"pending transactions ("+strings.Join(st.Pending, ", ")+"): run `workloom recover --actor <a> --reason <r>` before handing off")
		// A partial lease snapshot would be worse than none: the pending
		// material may still apply, so defer the lease audit entirely.
		st.LeasesDeferred = true
		st.Note = "lease audit deferred: pending transactions make the lease snapshot partial"
	} else {
		probes, err := reconcile.LeaseAudit(ctx, root, reconcile.Options{})
		if err != nil {
			return Status{}, err
		}
		for _, lp := range probes {
			switch {
			case lp.Owner == "<unreadable>":
				st.Leases = append(st.Leases, Lease{WorkitemID: lp.WorkitemID, Owner: lp.Owner, Kind: "unreadable"})
				block(CodeUnreadableLeases, "unreadable lease "+lp.WorkitemID+" ("+lp.Path+"): fix or remove it before handing off")
			case lp.ReasonKind == "orphan":
				st.Leases = append(st.Leases, Lease{WorkitemID: lp.WorkitemID, Owner: lp.Owner, RunID: lp.RunID, Kind: "orphan"})
				block(CodeOrphanLeases, "orphan lease on "+lp.WorkitemID+" (owner="+lp.Owner+" run="+lp.RunID+" missing): run `workloom recover` on the old device first")
			case lp.Expired || lp.ReasonKind == "expired":
				st.Leases = append(st.Leases, Lease{WorkitemID: lp.WorkitemID, Owner: lp.Owner, RunID: lp.RunID, Kind: "expired"})
				block(CodeExpiredLeases, "expired lease on "+lp.WorkitemID+" (owner="+lp.Owner+"): the old device never released it")
			default:
				st.Leases = append(st.Leases, Lease{
					WorkitemID: lp.WorkitemID, Owner: lp.Owner, RunID: lp.RunID, Kind: "active",
				})
				block(CodeActiveLeases, "active lease on "+lp.WorkitemID+" (owner="+lp.Owner+"): release it on the old device before handing off")
			}
		}
	}

	st.HandoffReady = len(st.Blockers) == 0
	return st, nil
}

func sortedOrEmpty(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
