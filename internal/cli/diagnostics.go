package cli

// Read-only diagnostics and recovery commands. These handlers predate the
// shared application service (M4.2) and stay CLI-only: doctor/recover/repair
// are operator tools, not agent tools (方案 §8.2 has no equivalents).

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"workloom/internal/config"
	"workloom/internal/domain"
	"workloom/internal/project"
	"workloom/internal/reconcile"
	"workloom/internal/search"
)

func runSearch(stdout io.Writer, opts options, rest []string) error {
	if len(rest) != 1 {
		return errUsage("`devsys search` needs exactly one keyword")
	}
	cwd, err := resolveRoot()
	if err != nil {
		return errInternal("resolve working directory: %v", err)
	}
	devsys := filepath.Join(cwd, project.DevsysDirName)
	if _, err := os.Stat(devsys); errors.Is(err, fs.ErrNotExist) {
		return errPrecondition("no %s/ in %s: run `devsys init` first", project.DevsysDirName, cwd)
	} else if err != nil {
		return errInternal("inspect %s: %v", devsys, err)
	}
	matches, total, err := search.Search(devsys, rest[0])
	if err != nil {
		return errInternal("search: %v", err)
	}
	if opts.json {
		out := struct {
			OK      bool           `json:"ok"`
			Root    string         `json:"root"`
			Query   string         `json:"query"`
			Matches []search.Match `json:"matches"`
			Total   int            `json:"total"`
		}{OK: true, Root: cwd, Query: rest[0], Matches: matches, Total: total}
		if out.Matches == nil {
			out.Matches = []search.Match{}
		}
		return json.NewEncoder(stdout).Encode(out)
	}
	if !opts.quiet {
		for _, m := range matches {
			fmt.Fprintf(stdout, "%s:%d: %s\n", m.Path, m.Line, m.Text)
		}
		fmt.Fprintf(stdout, "%d match(es) in .devsys/ for %q\n", total, rest[0])
	}
	return nil
}

// runConfigCheck implements `devsys config check`: the read-only diagnostic

func runConfigCheck(stdout io.Writer, opts options, rest []string) error {
	if familyUsage(stdout, rest, "`devsys config` needs a subcommand (try `devsys config check`)") {
		return nil
	}
	if rest[0] != "check" {
		return errUsage("unknown `devsys config` subcommand %q (try `devsys config check`)", rest[0])
	}
	if len(rest) > 1 {
		return errUsage("`devsys config check` takes no arguments (got %q)", rest[1])
	}
	cwd, err := resolveRoot()
	if err != nil {
		return errInternal("resolve working directory: %v", err)
	}
	devsys := filepath.Join(cwd, project.DevsysDirName)
	if _, err := os.Stat(devsys); errors.Is(err, fs.ErrNotExist) {
		return errPrecondition("no %s/ in %s: run `devsys init` first", project.DevsysDirName, cwd)
	} else if err != nil {
		return errInternal("inspect %s: %v", devsys, err)
	}

	md, problems := config.Diagnose(cwd)
	if len(problems) > 0 {
		return errInvalid(problems)
	}

	if opts.json {
		out := struct {
			OK      bool            `json:"ok"`
			Root    string          `json:"root"`
			Checked []string        `json:"checked"`
			Project *domain.Project `json:"project,omitempty"`
			Config  *config.Config  `json:"config,omitempty"`
		}{OK: true, Root: cwd, Checked: config.ManagedFiles(), Project: md.Project, Config: md.Config}
		return json.NewEncoder(stdout).Encode(out)
	}
	if !opts.quiet {
		fmt.Fprintf(stdout, "config ok: %d metadata files checked (%s)\n",
			len(config.ManagedFiles()), strings.Join(config.ManagedFiles(), ", "))
		if md.Project != nil {
			fmt.Fprintf(stdout, "project: %s (%s)\n", md.Project.Name, md.Project.ID)
		}
	}
	return nil
}

// policySummary is the JSON view of one valid policy in `workflow check`.

func runDoctor(stdout io.Writer, opts options, rest []string) error {
	if len(rest) != 0 {
		return errUsage("`devsys doctor` takes no arguments")
	}
	root, err := resolveRoot()
	if err != nil {
		return errInternal("resolve working directory: %v", err)
	}
	rep, err := reconcile.Doctor(context.Background(), root, reconcile.Options{})
	if err != nil {
		return errInternal("doctor: %v", err)
	}
	// The report — including invalid_files — goes on stdout in both modes;
	// the exit code alone carries "untrusted managed state" (方案 §14.1).
	if opts.json {
		if err := json.NewEncoder(stdout).Encode(rep); err != nil {
			return err
		}
		if len(rep.InvalidFiles) > 0 {
			return exitWithCode(CodeInvalid)
		}
		return nil
	}
	if !opts.quiet {
		if !rep.InspectionOK {
			fmt.Fprintf(stdout, "inspection limited: %s\n", rep.Note)
		}
		for _, p := range rep.PendingTransactions {
			fmt.Fprintf(stdout, "PENDING  %s\n", p.ID)
		}
		if len(rep.PendingTransactions) > 0 {
			fmt.Fprintln(stdout, "run `devsys recover` before trusting business state")
		}
		for _, l := range rep.ExpiredLeases {
			fmt.Fprintf(stdout, "EXPIRED  %s  owner=%s  lease_until=%s\n", l.WorkitemID, l.Owner, l.LeaseUntil.Format(time.RFC3339))
		}
		for _, l := range rep.OrphanLeases {
			fmt.Fprintf(stdout, "ORPHAN   %s  owner=%s  run=%s missing\n", l.WorkitemID, l.Owner, l.RunID)
		}
		for _, f := range rep.InvalidFiles {
			fmt.Fprintf(stdout, "INVALID  %s  %s\n", f.Path, f.Err)
		}
		for _, p := range rep.Orphans {
			fmt.Fprintf(stdout, "PROPOSE  %-26s %s  %s\n", p.Kind, p.WorkitemID, p.Description)
			for _, ev := range p.Evidence {
				if ev.Kind == reconcile.EvidenceAbsent {
					fmt.Fprintf(stdout, "         absent  %s\n", ev.Path)
				} else {
					fmt.Fprintf(stdout, "         file    %s  %s\n", ev.Path, shortHash(ev.SHA256))
				}
			}
		}
	}
	if len(rep.InvalidFiles) > 0 {
		// The report is on stdout; the code carries "untrusted managed
		// state" (方案 §14.1) without repeating the file list on stderr.
		return exitWithCode(CodeInvalid)
	}
	return nil
}

// runRecover implements `devsys recover`: deterministic transaction
// recovery followed by policy-driven orphan release (M2.3). Actor and
// reason are mandatory so every release carries an audit trail.

func runRecover(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("recover", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	actor := fs.String("actor", "", "operator")
	reason := fs.String("reason", "", "recovery reason")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *actor == "" || *reason == "" {
		return errUsage("recover --actor <actor> --reason <reason>")
	}
	root, err := resolveRoot()
	if err != nil {
		return errInternal("resolve working directory: %v", err)
	}
	rep, err := reconcile.Recover(context.Background(), root, reconcile.Options{Actor: *actor, Reason: *reason})
	if err != nil {
		return errInternal("recover: %v", err)
	}
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK  bool                    `json:"ok"`
			Rep reconcile.RecoverReport `json:"recover"`
		}{true, rep})
	}
	if !opts.quiet {
		fmt.Fprintf(stdout, "transactions recovered: %d applied, %d skipped, %d discarded\n",
			rep.TransactionRecovery.Replayed, rep.TransactionRecovery.Skipped, rep.TransactionRecovery.Discarded)
		for _, id := range rep.ReleasedExpired {
			fmt.Fprintf(stdout, "released expired lease: %s\n", id)
		}
		for _, id := range rep.ReleasedOrphans {
			fmt.Fprintf(stdout, "released orphan lease: %s\n", id)
		}
	}
	return nil
}

// runRepair implements `devsys repair`: dry-run lists proposals with a
// deterministic digest; --apply re-runs the dry-run and only applies when
// the supplied --confirm digest still matches, so every apply revalidates
// the evidence it consumes (方案 §15.4 推断→确认→重写).

func runRepair(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("repair", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apply := fs.Bool("apply", false, "apply the repair plan")
	confirm := fs.String("confirm", "", "digest from --dry-run")
	dryRun := fs.Bool("dry-run", false, "list proposed repairs without writing")
	actor := fs.String("actor", "", "operator")
	reason := fs.String("reason", "", "repair reason")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *actor == "" || *reason == "" {
		return errUsage("repair --dry-run --actor <a> --reason <r> | repair --apply --confirm <digest> --actor <a> --reason <r>")
	}
	root, err := resolveRoot()
	if err != nil {
		return errInternal("resolve working directory: %v", err)
	}
	ropts := reconcile.Options{Actor: *actor, Reason: *reason}
	if *apply {
		if *confirm == "" {
			return errUsage("repair --apply requires --confirm <digest>")
		}
		plan, err := reconcile.RepairDryRun(context.Background(), root, ropts)
		if err != nil {
			return errInternal("repair plan: %v", err)
		}
		plan.Digest = *confirm
		if len(plan.Proposals) == 0 {
			if !opts.quiet {
				fmt.Fprintln(stdout, "nothing to repair")
			}
			return nil
		}
		rep, err := reconcile.RepairApply(context.Background(), root, plan, ropts)
		if err != nil {
			if errors.Is(err, reconcile.ErrDigestMismatch) {
				return errPrecondition("confirmation digest does not match current state; run `devsys repair --dry-run` again")
			}
			return errInternal("repair apply: %v", err)
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK  bool                  `json:"ok"`
				Rep reconcile.ApplyReport `json:"apply"`
			}{true, rep})
		}
		if !opts.quiet {
			for _, a := range rep.Applied {
				fmt.Fprintf(stdout, "applied  %s\n", a)
			}
			for _, r := range rep.Rejected {
				fmt.Fprintf(stdout, "rejected %s\n", r)
			}
		}
		return nil
	}
	if !*dryRun {
		return errUsage("repair requires --dry-run or --apply --confirm <digest>")
	}
	plan, err := reconcile.RepairDryRun(context.Background(), root, ropts)
	if err != nil {
		return errInternal("repair plan: %v", err)
	}
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK   bool           `json:"ok"`
			Plan reconcile.Plan `json:"plan"`
		}{true, plan})
	}
	if !opts.quiet {
		if plan.Note != "" {
			fmt.Fprintf(stdout, "note: %s\n", plan.Note)
		}
		for _, p := range plan.Proposals {
			fmt.Fprintf(stdout, "%-26s %s  %s\n", p.Kind, p.WorkitemID, p.Description)
			for _, ev := range p.Evidence {
				if ev.Kind == reconcile.EvidenceAbsent {
					fmt.Fprintf(stdout, "  absent  %s\n", ev.Path)
				} else {
					fmt.Fprintf(stdout, "  file    %s  %s\n", ev.Path, shortHash(ev.SHA256))
				}
			}
		}
		fmt.Fprintf(stdout, "digest: %s\n", plan.Digest)
	}
	return nil
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
