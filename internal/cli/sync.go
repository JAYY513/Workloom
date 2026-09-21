package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"workloom/internal/storage"
	"workloom/internal/syncstatus"
)

// runSync routes the sync family (方案 §14.4): M8.1 ships the read-only
// `status` inspection only. Handoff execution (push/pull/merge) stays with
// git itself; devsys only judges readiness.
func runSync(stdout io.Writer, opts options, rest []string) error {
	if familyUsage(stdout, rest, "`devsys sync` needs a subcommand: status") {
		return nil
	}
	switch rest[0] {
	case "status":
		return runSyncStatus(stdout, opts, rest[1:])
	default:
		return errUsage("unknown `devsys sync` subcommand %q", rest[0])
	}
}

// runSyncStatus implements `devsys sync status`: the read-only handoff
// verdict (M8.1). It writes nothing — no fetch, no lock creation, no
// recovery — and reports blocked-vs-ready through stdout with exit 0: a
// blocked handoff is the inspected state, not a command failure (the same
// verdict shape `doctor` and `next` use).
func runSyncStatus(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("sync status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 {
		return errUsage("`devsys sync status` takes no arguments")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	// requireProjectRoot already rejects a missing .devsys/; the
	// ErrNotInitialized branch below only fires when .devsys/ exists but
	// storage.Open cannot treat it as a project (not a directory).
	st, err := syncstatus.Check(context.Background(), svc.Root)
	if err != nil {
		if errors.Is(err, storage.ErrNotInitialized) {
			return errPrecondition("sync status: %v", err)
		}
		// Anything else is an environment failure (git missing, outside a
		// repository): the project cannot be inspected from here.
		return errPrecondition("sync status: %v", trimSyncErr(err))
	}
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK bool `json:"ok"`
			syncstatus.Status
		}{OK: true, Status: st})
	}
	if !opts.quiet {
		renderSyncStatus(stdout, st)
	}
	return nil
}

// trimSyncErr keeps git's stderr one line: the runner's message already
// carries the failing subcommand.
func trimSyncErr(err error) error {
	msg := strings.TrimSpace(err.Error())
	if i := strings.Index(msg, "\n"); i >= 0 {
		msg = msg[:i]
	}
	return errors.New(msg)
}

// renderSyncStatus prints the verdict as one section per check, the shape
// the other read commands use. The JSON form is the contract.
func renderSyncStatus(stdout io.Writer, st syncstatus.Status) {
	head := st.Commit
	if len(head) > 12 {
		head = head[:12]
	}
	if st.Commit == "" {
		head = "(none)"
	}
	if st.Upstream != "" {
		fmt.Fprintf(stdout, "git: %s@%s -> %s (+%d/-%d)\n", st.Branch, head, st.Upstream, st.Ahead, st.Behind)
	} else {
		fmt.Fprintf(stdout, "git: %s@%s (no upstream)\n", st.Branch, head)
	}
	if len(st.Unmerged) > 0 {
		fmt.Fprintf(stdout, "unmerged: %s\n", strings.Join(st.Unmerged, ", "))
	}
	if len(st.MergeWork) > 0 {
		fmt.Fprintf(stdout, "merge: unfinished (%s)\n", strings.Join(st.MergeWork, ", "))
	}
	if len(st.UncommittedDevsys) > 0 {
		fmt.Fprintf(stdout, "uncommitted .devsys/: %s\n", strings.Join(st.UncommittedDevsys, ", "))
	}
	if len(st.UncommittedOther) > 0 {
		fmt.Fprintf(stdout, "uncommitted other: %s\n", strings.Join(st.UncommittedOther, ", "))
	}
	if st.LeasesDeferred {
		fmt.Fprintln(stdout, "leases: audit deferred (pending transactions)")
	} else {
		for _, l := range st.Leases {
			if l.RunID != "" {
				fmt.Fprintf(stdout, "lease: %s (%s)  owner=%s  run=%s\n", l.WorkitemID, l.Kind, l.Owner, l.RunID)
			} else {
				fmt.Fprintf(stdout, "lease: %s (%s)  owner=%s\n", l.WorkitemID, l.Kind, l.Owner)
			}
		}
	}
	if st.HandoffReady {
		fmt.Fprintln(stdout, "handoff: ready — old device already stopped? `git push`, then the new device runs `git fetch` + `devsys sync status`")
	} else {
		for _, b := range st.Blockers {
			fmt.Fprintf(stdout, "blocked [%s]: %s\n", b.Code, b.Detail)
		}
		fmt.Fprintln(stdout, "handoff: NOT ready")
	}
}
