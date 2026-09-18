// Package cli implements the devsys command surface: global switches, command
// dispatch, output rendering and the exit-code taxonomy. Keeping dispatch out
// of main() makes the exit contract testable.
package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"workloom/internal/registry"
	"workloom/internal/search"
	"workloom/internal/storage"
	"workloom/internal/version"
	"workloom/internal/workitem"
)

// Exit codes, pinned by tests so scripts may rely on them. The knowledge
// layer adds its own 0/10/11 convention later (方案 §12.5, 实施计划 M4.5).
const (
	CodeOK           = 0
	CodeInternal     = 1
	CodeUsage        = 2
	CodePrecondition = 3
	// CodeInvalid reports managed state that exists but cannot be trusted:
	// parse errors, unknown keys, wrong types, unsupported schema_version.
	// Write commands refuse; read-only diagnostics keep working (方案 §14.1).
	CodeInvalid = 4
)

const usage = `devsys - project-local agent development infrastructure

usage:
  devsys [--json] [--quiet] <command>

commands:
  init          create .devsys/ in the current git repository root
  config check  validate the managed metadata files (read-only)
  search <text> search project-local text records
  workitem      create, inspect, transition and claim work items
  doctor        report transactions and orphaned claims (read-only)
  recover       recover transactions, release expired/orphaned claims
  repair        --dry-run proposes repairs; --apply --confirm <digest> applies

options:
  --json      machine-readable output
  --quiet     suppress the human-readable success output
  --version   print build identity
  --help      print this help

exit codes:
  0  success
  1  internal error
  2  usage error
  3  precondition error (not a git repository, wrong directory, permissions, digest mismatch)
  4  invalid managed state (parse, field or schema_version problems)
`

type options struct {
	json  bool
	quiet bool
}

// codedError carries the exit-code class through dispatch.
type codedError struct {
	code int
	kind string
	msg  string
	// problems are the located defects behind an "invalid" error. They are
	// rendered one per line in human output and as a JSON array in --json.
	problems []config.Problem
}

func (e *codedError) Error() string { return e.msg }

func errUsage(format string, a ...any) *codedError {
	return &codedError{code: CodeUsage, kind: "usage", msg: fmt.Sprintf(format, a...)}
}

func errInternal(format string, a ...any) *codedError {
	return &codedError{code: CodeInternal, kind: "internal", msg: fmt.Sprintf(format, a...)}
}

func errPrecondition(format string, a ...any) *codedError {
	return &codedError{code: CodePrecondition, kind: "precondition", msg: fmt.Sprintf(format, a...)}
}

func errInvalid(problems []config.Problem) *codedError {
	word := "problems"
	if len(problems) == 1 {
		word = "problem"
	}
	return &codedError{
		code:     CodeInvalid,
		kind:     "invalid",
		msg:      fmt.Sprintf("invalid managed state (%d %s)", len(problems), word),
		problems: problems,
	}
}

// Run executes one CLI invocation and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	var opts options
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if a == "" || a[0] != '-' {
			break
		}
		switch a {
		case "--json":
			opts.json = true
		case "--quiet":
			opts.quiet = true
		case "--version", "-v":
			fmt.Fprintln(stdout, "devsys "+version.String())
			return CodeOK
		case "--help", "-h":
			fmt.Fprint(stdout, usage)
			return CodeOK
		default:
			return render(stderr, opts, errUsage("unknown option %q", a))
		}
	}
	if i == len(args) {
		return render(stderr, opts, errUsage("no command given"))
	}

	cmd, rest := args[i], args[i+1:]
	switch cmd {
	case "init":
		if len(rest) > 0 {
			return render(stderr, opts, errUsage("`devsys init` takes no arguments (got %q)", rest[0]))
		}
		return render(stderr, opts, runInit(stdout, opts))
	case "config":
		return render(stderr, opts, runConfigCheck(stdout, opts, rest))
	case "search":
		return render(stderr, opts, runSearch(stdout, opts, rest))
	case "workitem":
		return render(stderr, opts, runWorkitem(stdout, opts, rest))
	case "doctor":
		return render(stderr, opts, runDoctor(stdout, opts, rest))
	case "recover":
		return render(stderr, opts, runRecover(stdout, opts, rest))
	case "repair":
		return render(stderr, opts, runRepair(stdout, opts, rest))
	default:
		return render(stderr, opts, errUsage("unknown command %q", cmd))
	}
}

// render prints an error (if any) and returns the exit code; nil means success
// with output already written by the command.
func render(stderr io.Writer, opts options, err error) int {
	if err == nil {
		return CodeOK
	}
	var ce *codedError
	if !errors.As(err, &ce) {
		ce = errInternal("%v", err)
	}
	if opts.json {
		payload := struct {
			OK    bool `json:"ok"`
			Error struct {
				Code     int              `json:"code"`
				Kind     string           `json:"kind"`
				Message  string           `json:"message"`
				Problems []config.Problem `json:"problems,omitempty"`
			} `json:"error"`
		}{}
		payload.Error.Code = ce.code
		payload.Error.Kind = ce.kind
		payload.Error.Message = ce.msg
		payload.Error.Problems = ce.problems
		_ = json.NewEncoder(stderr).Encode(payload)
	} else {
		fmt.Fprintf(stderr, "devsys: %s\n", ce.msg)
		for _, p := range ce.problems {
			fmt.Fprintf(stderr, "  %s\n", p.String())
		}
	}
	return ce.code
}

func runInit(stdout io.Writer, opts options) error {
	cwd, err := os.Getwd()
	if err != nil {
		return errInternal("resolve working directory: %v", err)
	}
	now := time.Now()

	res, err := project.Init(cwd, project.Options{Now: now})
	if err != nil {
		var ps config.Problems
		if errors.As(err, &ps) {
			return errInvalid(ps)
		}
		var pe *project.PreconditionError
		if errors.As(err, &pe) {
			return errPrecondition("%s", pe.Msg)
		}
		return errInternal("init failed: %v", err)
	}

	regPath, err := registry.Path()
	if err != nil {
		return errPrecondition("%v", err)
	}
	reg, err := registry.Load(regPath)
	if err != nil {
		return errInternal("read user registry: %v", err)
	}
	reg.Upsert(registry.Entry{ID: res.ID, Path: res.Root, LastSeenAt: now})
	if err := reg.Save(regPath); err != nil {
		return errPrecondition("write user registry: %v", err)
	}

	if opts.json {
		out := struct {
			OK              bool     `json:"ok"`
			Root            string   `json:"root"`
			ID              string   `json:"id"`
			Name            string   `json:"name"`
			Created         []string `json:"created"`
			RegistryPath    string   `json:"registry_path"`
			RegistryUpdated bool     `json:"registry_updated"`
		}{
			OK:              true,
			Root:            res.Root,
			ID:              res.ID,
			Name:            res.Name,
			Created:         res.Created,
			RegistryPath:    regPath,
			RegistryUpdated: true,
		}
		if out.Created == nil {
			out.Created = []string{}
		}
		return json.NewEncoder(stdout).Encode(out)
	}

	if !opts.quiet {
		fmt.Fprintf(stdout, "initialized %s/ in %s\n", project.DevsysDirName, res.Root)
		fmt.Fprintf(stdout, "project: %s (%s)\n", res.Name, res.ID)
		if len(res.Created) == 0 {
			fmt.Fprintln(stdout, "created: nothing (already initialized)")
		} else {
			fmt.Fprintf(stdout, "created: %d paths\n", len(res.Created))
		}
		fmt.Fprintf(stdout, "registry: %s\n", regPath)
	}
	return nil
}

// runSearch implements `devsys search <keyword>` (实施计划 M1.6): a plain
// read-only scan of .devsys/, excluding .cache/ and local/.
func runSearch(stdout io.Writer, opts options, rest []string) error {
	if len(rest) != 1 {
		return errUsage("`devsys search` needs exactly one keyword")
	}
	cwd, err := os.Getwd()
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
	if len(rest) == 0 {
		return errUsage("`devsys config` needs a subcommand (try `devsys config check`)")
	}
	if rest[0] != "check" {
		return errUsage("unknown `devsys config` subcommand %q (try `devsys config check`)", rest[0])
	}
	if len(rest) > 1 {
		return errUsage("`devsys config check` takes no arguments (got %q)", rest[1])
	}
	cwd, err := os.Getwd()
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
		fmt.Fprintf(stdout, "config ok: %d files checked\n", len(config.ManagedFiles()))
		if md.Project != nil {
			fmt.Fprintf(stdout, "project: %s (%s)\n", md.Project.Name, md.Project.ID)
		}
	}
	return nil
}

// runWorkitem exposes the domain store without bypassing its write contracts.
func runWorkitem(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 {
		return errUsage("workitem requires create or get")
	}
	root, err := os.Getwd()
	if err != nil {
		return errInternal("working directory: %v", err)
	}
	items := workitem.New(root)
	ctx := context.Background()
	switch rest[0] {
	case "get":
		if len(rest) != 2 {
			return errUsage("workitem get <id>")
		}
		wi, raw, err := items.ReadSnapshot(ctx, rest[1])
		if err != nil {
			return workitemError(err)
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK      bool             `json:"ok"`
				Item    *domain.WorkItem `json:"item"`
				Version string           `json:"version"`
			}{true, wi, fmt.Sprintf("%x", storage.HashBytes(raw))})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s\t%s\t%s\nversion: %s\n", wi.ID, wi.Status, wi.Title, fmt.Sprintf("%x", storage.HashBytes(raw)))
		}
		return nil
	case "create":
		fs := flag.NewFlagSet("workitem create", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		title := fs.String("title", "", "work item title")
		prefix := fs.String("prefix", "WLM", "ID prefix")
		actor := fs.String("actor", "", "operator")
		reason := fs.String("reason", "", "creation reason")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *title == "" || *actor == "" || *reason == "" {
			return errUsage("workitem create --title <title> --actor <actor> --reason <reason> [--prefix WLM]")
		}
		meta, problems := config.Load(root)
		if len(problems) > 0 {
			return errInvalid(problems)
		}
		if meta.Project == nil {
			return errPrecondition("project not initialized; run devsys init")
		}
		now := time.Now().UTC()
		wi := &domain.WorkItem{ProjectID: meta.Project.ID, Title: *title, Type: "task", Status: "draft", ProposedBy: *actor, Reason: *reason, CreatedAt: now, UpdatedAt: now}
		id, err := items.Create(ctx, wi, *prefix)
		if err != nil {
			return workitemError(err)
		}
		return runWorkitem(stdout, opts, []string{"get", id})
	case "transition":
		fs := flag.NewFlagSet("workitem transition", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "work item id")
		to := fs.String("to", "", "target status")
		actor := fs.String("actor", "", "operator")
		reason := fs.String("reason", "", "transition reason")
		expect := fs.String("expect", "", "version hash from workitem get")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
			return errUsage("workitem transition --id <id> --to <status> --actor <a> --reason <r> --expect <hash>")
		}
		expected, err := expectedSnapshot(ctx, items, *id, *expect)
		if err != nil {
			return workitemError(err)
		}
		wi, err := items.Transition(ctx, *id, workitem.TransitionRequest{
			TargetStatus: *to, Actor: *actor, Reason: *reason,
		}, expected)
		if err != nil {
			return workitemError(err)
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s: %s\n", wi.ID, wi.Status)
		}
		return nil
	case "claim":
		fs := flag.NewFlagSet("workitem claim", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "work item id")
		owner := fs.String("owner", "", "claimer identity")
		reason := fs.String("reason", "", "claim reason")
		expect := fs.String("expect", "", "version hash from workitem get")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
			return errUsage("workitem claim --id <id> --owner <owner> --reason <reason> [--expect <hash>]")
		}
		expected, err := expectedSnapshot(ctx, items, *id, *expect)
		if err != nil {
			return workitemError(err)
		}
		res, err := items.Claim(ctx, *id, workitem.ClaimOptions{
			Owner: *owner, Actor: *owner, Reason: *reason, Expected: expected,
		})
		if err != nil {
			return workitemError(err)
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK     bool   `json:"ok"`
				RunID  string `json:"run_id"`
				Token  string `json:"token"`
				Status string `json:"status"`
			}{true, res.RunID, res.Token, res.Status})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "claimed %s: run=%s token=%s\n", *id, res.RunID, res.Token)
		}
		return nil
	default:
		return errUsage("unknown workitem operation %q", rest[0])
	}
}

func workitemError(err error) error {
	if errors.Is(err, storage.ErrNotInitialized) || errors.Is(err, workitem.ErrNotFound) {
		return errPrecondition("%v", err)
	}
	return &codedError{code: CodeInvalid, kind: "workitem", msg: err.Error()}
}

// decodeExpected converts the 64-hex version printed by workitem get back
// into the raw expected-bytes guard the mutations require. The hash alone
// is not the bytes, so mutations re-derive the guard by reading the file
// and comparing its hash before staging (fail-closed: unknown hash = no
// authority).
// expectedSnapshot turns the --expect version hash into the raw expected
// bytes the mutations require: it reads the current snapshot and only
// accepts when its hash matches the caller's view (fail-closed — a stale
// or unknown hash means no authority to mutate).
func expectedSnapshot(ctx context.Context, items *workitem.Store, id, expect string) ([]byte, error) {
	_, raw, err := items.ReadSnapshot(ctx, id)
	if err != nil {
		return nil, err
	}
	if expect = strings.TrimSpace(expect); expect != "" {
		want, derr := hex.DecodeString(expect)
		sum := storage.HashBytes(raw)
		if derr != nil || len(want) != sha256.Size || !bytes.Equal(want, sum[:]) {
			return nil, fmt.Errorf("version mismatch: work item changed since your read; rerun workitem get")
		}
	}
	return raw, nil
}

// runDoctor implements `devsys doctor`: the read-only inspection report
// (M2.3). It never recovers or writes; pending transactions are reported
// instead of business facts.
func runDoctor(stdout io.Writer, opts options, rest []string) error {
	if len(rest) != 0 {
		return errUsage("`devsys doctor` takes no arguments")
	}
	root, err := os.Getwd()
	if err != nil {
		return errInternal("resolve working directory: %v", err)
	}
	rep, err := reconcile.Doctor(context.Background(), root, reconcile.Options{})
	if err != nil {
		return errInternal("doctor: %v", err)
	}
	if opts.json {
		return json.NewEncoder(stdout).Encode(rep)
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
	root, err := os.Getwd()
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
	root, err := os.Getwd()
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
