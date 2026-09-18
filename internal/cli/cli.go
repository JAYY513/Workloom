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
	"sort"
	"strings"
	"time"

	"workloom/internal/approval"
	"workloom/internal/config"
	"workloom/internal/domain"
	"workloom/internal/events"
	"workloom/internal/next"
	"workloom/internal/project"
	"workloom/internal/reconcile"
	"workloom/internal/record"
	"workloom/internal/registry"
	"workloom/internal/search"
	"workloom/internal/storage"
	"workloom/internal/version"
	"workloom/internal/workflow"
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
  workflow check  validate workflow policy files (read-only)
  workflow start|next|step-complete|pause|resume|cancel  drive a work item's workflow instance
  next          readiness verdict and the recommended next action (read-only)
  approval      list | request | approve | reject governance approvals

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
	case "workflow":
		return render(stderr, opts, runWorkflow(stdout, opts, rest))
	case "next":
		return render(stderr, opts, runNext(stdout, opts, rest))
	case "approval":
		return render(stderr, opts, runApproval(stdout, opts, rest))
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

// policySummary is the JSON view of one valid policy in `workflow check`.
type policySummary struct {
	File    string `json:"file"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// runWorkflowCheck implements `devsys workflow check`: the read-only policy
// diagnostic (方案 §5.3, 实施计划 M3.1). Like `config check` it reads plainly —
// no lock, no recovery, no writes — and reports every located issue as
// `file:line: field: reason`. Unknown keys are warnings: they are recorded,
// and a policy carrying only warnings still loads.
func runWorkflowCheck(stdout io.Writer, opts options, rest []string) error {
	if len(rest) != 0 {
		return errUsage("`devsys workflow check` takes no arguments (got %q)", rest[0])
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

	results := workflow.Load(cwd)
	var problems []config.Problem
	var warnings []workflow.Issue
	policies := 0
	for _, res := range results {
		if res.Policy != nil {
			policies++
		}
		for _, is := range res.Issues {
			switch is.Severity {
			case workflow.SeverityError:
				problems = append(problems, config.Problem{File: is.File, Line: is.Line, Field: is.Field, Reason: is.Reason})
			case workflow.SeverityWarning:
				warnings = append(warnings, is)
			}
		}
	}
	if len(problems) > 0 {
		return errInvalid(problems)
	}

	if opts.json {
		out := struct {
			OK       bool             `json:"ok"`
			Root     string           `json:"root"`
			Policies []policySummary  `json:"policies"`
			Warnings []workflow.Issue `json:"warnings,omitempty"`
		}{OK: true, Root: cwd, Policies: []policySummary{}}
		for _, res := range results {
			if res.Policy == nil {
				continue
			}
			out.Policies = append(out.Policies, policySummary{
				File: res.File, ID: res.Policy.ID, Name: res.Policy.Name, Version: res.Policy.Version,
			})
		}
		out.Warnings = append(out.Warnings, warnings...)
		return json.NewEncoder(stdout).Encode(out)
	}
	if !opts.quiet {
		fmt.Fprintf(stdout, "workflow ok: %d policies checked\n", policies)
	}
	// Warnings are diagnostics, not success banners: they stay visible under
	// --quiet so recorded issues are not silently dropped.
	for _, w := range warnings {
		fmt.Fprintf(stdout, "warning: %s\n", w.String())
	}
	return nil
}

// runWorkflow routes the workflow family: `check` validates policy files
// (read-only); the instance subcommands drive a work item's workflow
// instance through the domain layer (实施计划 M3.6). M4.2 exposes the same
// operations as MCP tools.
func runWorkflow(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 {
		return errUsage("`devsys workflow` needs a subcommand (check | start | next | step-complete | pause | resume | cancel)")
	}
	switch rest[0] {
	case "check":
		return runWorkflowCheck(stdout, opts, rest[1:])
	case "start":
		return runWorkflowStart(stdout, opts, rest[1:])
	case "next":
		return runWorkflowNext(stdout, opts, rest[1:])
	case "step-complete":
		return runWorkflowStepComplete(stdout, opts, rest[1:])
	case "pause", "resume", "cancel":
		return runWorkflowSignal(stdout, opts, rest[0], rest[1:])
	default:
		return errUsage("unknown `devsys workflow` subcommand %q", rest[0])
	}
}

// rootContext resolves the project root and verifies .devsys/ exists.
func rootContext() (string, context.Context, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", nil, errInternal("resolve working directory: %v", err)
	}
	devsys := filepath.Join(root, project.DevsysDirName)
	if _, err := os.Stat(devsys); errors.Is(err, fs.ErrNotExist) {
		return "", nil, errPrecondition("no %s/ in %s: run `devsys init` first", project.DevsysDirName, root)
	} else if err != nil {
		return "", nil, errInternal("inspect %s: %v", devsys, err)
	}
	return root, context.Background(), nil
}

// workflowContext prepares the shared context for instance subcommands.
func workflowContext() (string, *workitem.Store, context.Context, error) {
	root, ctx, err := rootContext()
	if err != nil {
		return "", nil, nil, err
	}
	return root, workitem.New(root), ctx, nil
}

// workflowPolicy resolves the policy an instance operation consumes. start
// is dispatch-like and requires a currently valid file; advancing and read
// operations accept the last-known-good snapshot and report the fallback in
// a notice (实施计划 M3.5).
func workflowPolicy(ctx context.Context, root string, wi *domain.WorkItem, policyID string, requireCurrent bool) (*workflow.Policy, string, error) {
	id := policyID
	if id == "" && wi.Workflow != nil {
		id = wi.Workflow.ID
	}
	if id == "" {
		return nil, "", errUsage("--policy <id> is required when the work item has no workflow instance")
	}
	res, err := workflow.Resolve(ctx, root, id)
	if err != nil {
		return nil, "", errWorkflowPolicy(err)
	}
	notice := ""
	if res.Issue != nil {
		notice = fmt.Sprintf("workflow policy %q is invalid: %s; using %s", res.ID, res.Issue.String(), res.Source)
		if requireCurrent {
			return nil, "", errWorkflowPolicy(fmt.Errorf(
				"workflow policy %q is invalid: %s; the operation is blocked until the file is fixed (%s is retained for read paths)",
				res.ID, res.Issue.String(), res.Source))
		}
	}
	return res.Policy, notice, nil
}

// mapWorkflowError renders domain instance refusals: the allowed candidates
// become located problems (exit 4).
func mapWorkflowError(err error, policyFile string) error {
	if errors.Is(err, storage.ErrNotInitialized) || errors.Is(err, workitem.ErrNotFound) {
		return errPrecondition("%v", err)
	}
	var se *workitem.WorkflowStepError
	if errors.As(err, &se) {
		problems := make([]config.Problem, 0, len(se.Allowed))
		for _, c := range se.Allowed {
			reason := fmt.Sprintf("when %q satisfied", c.When)
			switch {
			case c.When == "":
				reason = "unconditional"
			case !c.Satisfied:
				reason = fmt.Sprintf("when %q not satisfied", c.When)
			}
			problems = append(problems, config.Problem{File: policyFile, Field: c.To, Reason: reason})
		}
		return &codedError{code: CodeInvalid, kind: "workflow", msg: se.Error(), problems: problems}
	}
	return workitemError(err)
}

// outputWorkflow renders one work item's instance state and, for `next`, its
// candidates.
func outputWorkflow(stdout io.Writer, opts options, wi *domain.WorkItem, candidates []workflow.StepCandidate) error {
	if opts.json {
		out := struct {
			OK         bool                     `json:"ok"`
			WorkitemID string                   `json:"workitem_id"`
			Workflow   *domain.WorkflowInstance `json:"workflow"`
			Candidates []workflow.StepCandidate `json:"candidates,omitempty"`
		}{OK: true, WorkitemID: wi.ID, Workflow: wi.Workflow, Candidates: candidates}
		return json.NewEncoder(stdout).Encode(out)
	}
	if !opts.quiet {
		if wi.Workflow == nil {
			fmt.Fprintf(stdout, "%s: no workflow instance\n", wi.ID)
			return nil
		}
		inst := wi.Workflow
		fmt.Fprintf(stdout, "%s: workflow %s step %s paused=%t\n", wi.ID, inst.ID, inst.Step, inst.Paused)
		for _, c := range candidates {
			when := c.When
			if when == "" {
				when = "<unconditional>"
			}
			fmt.Fprintf(stdout, "  candidate: to=%s when=%q satisfied=%t\n", c.To, when, c.Satisfied)
		}
		if !inst.Paused {
			for _, c := range candidates {
				if c.Satisfied {
					fmt.Fprintf(stdout, "next: %s\n", c.To)
					break
				}
			}
		}
	}
	return nil
}

func runWorkflowStart(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("workflow start", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "work item id")
	policyID := fs.String("policy", "", "workflow policy id")
	actor := fs.String("actor", "", "operator")
	reason := fs.String("reason", "", "start reason")
	expect := fs.String("expect", "", "version hash from workitem get")
	owner := fs.String("owner", "", "lease owner when the work item is claimed")
	token := fs.String("token", "", "lease token when the work item is claimed")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *id == "" || *policyID == "" || *actor == "" || *reason == "" {
		return errUsage("workflow start --id <workitem-id> --policy <id> --actor <a> --reason <r> [--owner <o> --token <t>] [--expect <hash>]")
	}
	root, items, ctx, err := workflowContext()
	if err != nil {
		return err
	}
	wi, expected, err := expectedSnapshot(ctx, items, *id, *expect)
	if err != nil {
		return workitemError(err)
	}
	pol, _, err := workflowPolicy(ctx, root, wi, *policyID, true)
	if err != nil {
		return err
	}
	updated, err := items.WorkflowStart(ctx, *id, workitem.WorkflowStartOptions{
		Policy: pol, Actor: *actor, Reason: *reason, Owner: *owner, Token: *token, Expected: expected,
	})
	if err != nil {
		return mapWorkflowError(err, pol.File)
	}
	return outputWorkflow(stdout, opts, updated, nil)
}

func runWorkflowNext(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("workflow next", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "work item id")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *id == "" {
		return errUsage("workflow next --id <workitem-id>")
	}
	root, items, ctx, err := workflowContext()
	if err != nil {
		return err
	}
	wi, _, err := items.ReadSnapshot(ctx, *id)
	if err != nil {
		return workitemError(err)
	}
	pol, notice, err := workflowPolicy(ctx, root, wi, "", false)
	if err != nil {
		return err
	}
	if notice != "" && !opts.quiet {
		fmt.Fprintf(stdout, "warning: %s\n", notice)
	}
	updated, candidates, err := items.WorkflowNext(ctx, *id, pol)
	if err != nil {
		return mapWorkflowError(err, pol.File)
	}
	return outputWorkflow(stdout, opts, updated, candidates)
}

func runWorkflowStepComplete(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("workflow step-complete", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "work item id")
	to := fs.String("to", "", "target step id")
	actor := fs.String("actor", "", "operator")
	reason := fs.String("reason", "", "reason")
	expect := fs.String("expect", "", "version hash from workitem get")
	owner := fs.String("owner", "", "lease owner when the work item is claimed")
	token := fs.String("token", "", "lease token when the work item is claimed")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *id == "" || *actor == "" || *reason == "" {
		return errUsage("workflow step-complete --id <workitem-id> [--to <step>] --actor <a> --reason <r> [--owner <o> --token <t>] [--expect <hash>]")
	}
	root, items, ctx, err := workflowContext()
	if err != nil {
		return err
	}
	wi, expected, err := expectedSnapshot(ctx, items, *id, *expect)
	if err != nil {
		return workitemError(err)
	}
	pol, notice, err := workflowPolicy(ctx, root, wi, "", false)
	if err != nil {
		return err
	}
	if notice != "" && !opts.quiet {
		fmt.Fprintf(stdout, "warning: %s\n", notice)
	}
	updated, err := items.WorkflowStepComplete(ctx, *id, workitem.WorkflowStepOptions{
		Policy: pol, To: *to, Actor: *actor, Reason: *reason,
		Owner: *owner, Token: *token, Expected: expected,
	})
	if err != nil {
		return mapWorkflowError(err, pol.File)
	}
	return outputWorkflow(stdout, opts, updated, nil)
}

func runWorkflowSignal(stdout io.Writer, opts options, action string, rest []string) error {
	fs := flag.NewFlagSet("workflow "+action, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "work item id")
	actor := fs.String("actor", "", "operator")
	reason := fs.String("reason", "", "reason")
	expect := fs.String("expect", "", "version hash from workitem get")
	owner := fs.String("owner", "", "lease owner when the work item is claimed")
	token := fs.String("token", "", "lease token when the work item is claimed")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *id == "" || *actor == "" || *reason == "" {
		return errUsage("workflow %s --id <workitem-id> --actor <a> --reason <r> [--owner <o> --token <t>] [--expect <hash>]", action)
	}
	root, items, ctx, err := workflowContext()
	if err != nil {
		return err
	}
	wi, expected, err := expectedSnapshot(ctx, items, *id, *expect)
	if err != nil {
		return workitemError(err)
	}
	// Signals need no policy semantics, but the policy file locates refusals
	// when a candidate list is rendered.
	policyFile := ""
	if pol, _, perr := workflowPolicy(ctx, root, wi, "", false); perr == nil && pol != nil {
		policyFile = pol.File
	}
	signal := workitem.WorkflowSignalOptions{
		Actor: *actor, Reason: *reason, Owner: *owner, Token: *token, Expected: expected,
	}
	var updated *domain.WorkItem
	switch action {
	case "pause":
		updated, err = items.WorkflowPause(ctx, *id, signal)
	case "resume":
		updated, err = items.WorkflowResume(ctx, *id, signal)
	case "cancel":
		updated, err = items.WorkflowCancel(ctx, *id, signal)
	}
	if err != nil {
		return mapWorkflowError(err, policyFile)
	}
	return outputWorkflow(stdout, opts, updated, nil)
}

// runApproval routes the approval lifecycle (方案 §4.9, 实施计划 M3.7):
// request → approve | reject. Consumption itself happens inside the
// gate-checked transition (via the work item's Guard), never as a standalone
// command, so no caller can consume without advancing.
func runApproval(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 {
		return errUsage("`devsys approval` needs a subcommand (list | request | approve | reject)")
	}
	switch rest[0] {
	case "list":
		return runApprovalList(stdout, opts, rest[1:])
	case "request":
		return runApprovalRequest(stdout, opts, rest[1:])
	case "approve":
		return runApprovalDecide(stdout, opts, rest[1:], true)
	case "reject":
		return runApprovalDecide(stdout, opts, rest[1:], false)
	default:
		return errUsage("unknown `devsys approval` subcommand %q", rest[0])
	}
}

func approvalError(err error) error {
	if errors.Is(err, storage.ErrNotInitialized) || errors.Is(err, approval.ErrNotFound) {
		return errPrecondition("%v", err)
	}
	return &codedError{code: CodeInvalid, kind: "approval", msg: err.Error()}
}

func outputApproval(stdout io.Writer, opts options, apr *domain.Approval) error {
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK       bool             `json:"ok"`
			Approval *domain.Approval `json:"approval"`
		}{OK: true, Approval: apr})
	}
	if !opts.quiet {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\tstage=%s\trequested_status=%s\n",
			apr.ID, apr.Status, apr.Scope, apr.WorkItemID, apr.Stage, apr.RequestedStatus)
	}
	return nil
}

func runApprovalList(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("approval list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	workitemID := fs.String("workitem", "", "filter by work item")
	status := fs.String("status", "", "filter by status (pending|approved|rejected)")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 {
		return errUsage("approval list [--workitem <id>] [--status pending|approved|rejected]")
	}
	switch *status {
	case "", approval.StatusPending, approval.StatusApproved, approval.StatusRejected:
	default:
		return errUsage("approval list --status must be pending, approved or rejected (got %q)", *status)
	}
	root, ctx, err := rootContext()
	if err != nil {
		return err
	}
	approvals, err := approval.New(root).List(ctx, approval.Filter{WorkItemID: *workitemID, Status: *status})
	if err != nil {
		return approvalError(err)
	}
	if approvals == nil {
		approvals = []*domain.Approval{}
	}
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK        bool               `json:"ok"`
			Approvals []*domain.Approval `json:"approvals"`
		}{OK: true, Approvals: approvals})
	}
	if !opts.quiet {
		if len(approvals) == 0 {
			fmt.Fprintln(stdout, "no approvals")
		}
		for _, a := range approvals {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\tstage=%s\trequested_status=%s\n",
				a.ID, a.Status, a.Scope, a.WorkItemID, a.Stage, a.RequestedStatus)
		}
	}
	return nil
}

func runApprovalRequest(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("approval request", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	workitemID := fs.String("id", "", "work item id")
	stage := fs.String("stage", "", "gate stage the approval targets")
	scope := fs.String("scope", approval.ScopeStageGate, "stage_gate or action")
	runID := fs.String("run", "", "run that triggered the request")
	actor := fs.String("actor", "", "requester")
	reason := fs.String("reason", "", "why the approval is needed")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *workitemID == "" || *actor == "" || *reason == "" {
		return errUsage("approval request --id <workitem-id> --stage <stage> [--scope stage_gate|action] [--run <run-id>] --actor <a> --reason <r>")
	}
	root, ctx, err := rootContext()
	if err != nil {
		return err
	}
	wi, _, err := workitem.New(root).ReadSnapshot(ctx, *workitemID)
	if err != nil {
		return workitemError(err)
	}
	requestedStatus := ""
	if *scope == approval.ScopeStageGate {
		requestedStatus = wi.Status
	}
	apr, err := approval.New(root).Request(ctx, approval.RequestOptions{
		Scope: *scope, WorkItemID: wi.ID, RunID: *runID, Stage: *stage,
		RequestedStatus: requestedStatus, ProjectID: wi.ProjectID,
		RequestedBy: *actor, Reason: *reason,
	})
	if err != nil {
		return approvalError(err)
	}
	// Surface a request no gate will ever consume, instead of letting it
	// linger silently.
	if *scope == approval.ScopeStageGate {
		if res, perr := policyForWorkItem(ctx, root, wi); perr == nil && res.Policy != nil {
			gate, declared := res.Policy.Gates.Stages[*stage]
			if !declared || !gate.RequireApproval {
				if !opts.quiet {
					fmt.Fprintf(stdout, "warning: policy %q declares no require_approval gate for stage %q; nothing will consume this approval\n", res.Policy.ID, *stage)
				}
			}
		}
	}
	return outputApproval(stdout, opts, apr)
}

func runApprovalDecide(stdout io.Writer, opts options, rest []string, approve bool) error {
	action := "approve"
	usage := "approval approve --id <approval-id> --by <decider> [--comment <c>]"
	if !approve {
		action = "reject"
		usage = "approval reject --id <approval-id> --by <decider> --reason <r>"
	}
	fs := flag.NewFlagSet("approval "+action, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "approval id")
	by := fs.String("by", "", "decider")
	comment := fs.String("comment", "", "decision comment")
	reason := fs.String("reason", "", "rejection reason")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *id == "" || *by == "" || (!approve && *reason == "") {
		return errUsage("%s", usage)
	}
	root, ctx, err := rootContext()
	if err != nil {
		return err
	}
	if approve {
		apr, err := approval.New(root).Decide(ctx, *id, approval.DecideOptions{
			Approve: true, DecidedBy: *by, Comment: *comment,
		})
		if err != nil {
			return approvalError(err)
		}
		return outputApproval(stdout, opts, apr)
	}

	// Rejection of a stage gate commits the decision and the work item
	// disposition in ONE transaction (the decision rides the transition's
	// Guard), so a failed disposition leaves the approval pending instead of
	// half-applying (方案 §4.9).
	apr, err := approval.New(root).Get(ctx, *id)
	if err != nil {
		return approvalError(err)
	}
	disposition := ""
	if apr.Scope == approval.ScopeStageGate {
		disposition, err = rejectStageGate(ctx, root, apr, *by, *reason)
		if err != nil {
			return err
		}
	} else if _, err := approval.New(root).Decide(ctx, *id, approval.DecideOptions{
		Approve: false, DecidedBy: *by, Comment: *reason,
	}); err != nil {
		return approvalError(err)
	}
	apr, err = approval.New(root).Get(ctx, *id)
	if err != nil {
		return approvalError(err)
	}
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK          bool             `json:"ok"`
			Approval    *domain.Approval `json:"approval"`
			Disposition string           `json:"disposition,omitempty"`
		}{OK: true, Approval: apr, Disposition: disposition})
	}
	if !opts.quiet {
		fmt.Fprintf(stdout, "%s\t%s\n", apr.ID, apr.Status)
		if disposition != "" {
			fmt.Fprintf(stdout, "work item %s is now %s\n", apr.WorkItemID, disposition)
		}
	}
	return nil
}

// rejectStageGate records the rejection and moves the work item (default
// blocked, or the policy's on_reject regress target) in a single transition
// transaction. On any refusal nothing is committed — the approval stays
// pending and the error explains what blocked the disposition.
func rejectStageGate(ctx context.Context, root string, apr *domain.Approval, decider, reason string) (string, error) {
	items := workitem.New(root)
	wi, raw, err := items.ReadSnapshot(ctx, apr.WorkItemID)
	if err != nil {
		return "", workitemError(err)
	}
	target := domain.StatusBlocked
	if pol, perr := policyForWorkItem(ctx, root, wi); perr == nil && pol.Policy != nil && strings.HasPrefix(pol.Policy.OnReject, "regress:") {
		target = strings.TrimPrefix(pol.Policy.OnReject, "regress:")
	}
	now := time.Now().UTC()
	guard := func(tx *storage.Tx) ([]*domain.Event, error) {
		_, ev, derr := approval.DecideTx(tx, apr.ID, approval.DecideOptions{
			Approve: false, DecidedBy: decider, Comment: reason, Now: now,
		})
		if derr != nil {
			return nil, derr
		}
		return []*domain.Event{ev}, nil
	}
	if _, err := items.Transition(ctx, wi.ID, workitem.TransitionRequest{
		TargetStatus: target,
		Actor:        decider,
		Reason:       fmt.Sprintf("approval %s rejected: %s", apr.ID, reason),
		Now:          now,
		Guard:        guard,
	}, raw); err != nil {
		return "", &codedError{
			code: CodeInvalid,
			kind: "approval",
			msg:  fmt.Sprintf("rejection not recorded: work item %s could not move to %s (%v); nothing was changed", apr.WorkItemID, target, err),
		}
	}
	return target, nil
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
		wi, expected, err := expectedSnapshot(ctx, items, *id, *expect)
		if err != nil {
			return workitemError(err)
		}
		notice, approvalID, err := checkTransitionGate(ctx, root, wi, *to)
		if notice != "" && !opts.quiet {
			fmt.Fprintf(stdout, "warning: %s\n", notice)
		}
		if err != nil {
			return err
		}
		req := workitem.TransitionRequest{
			TargetStatus: *to, Actor: *actor, Reason: *reason,
			Now: time.Now().UTC(),
		}
		if approvalID != "" {
			// The approval is consumed inside the transition transaction:
			// it only becomes consumed when the advance it authorized
			// commits (实施计划 M3.7), and its event joins the same batch
			// with the very same timestamp.
			consumeID, stage, fromStatus, at := approvalID, *to, wi.Status, req.Now
			req.Guard = func(tx *storage.Tx) ([]*domain.Event, error) {
				ev, err := approval.ConsumeTx(tx, consumeID, wi.ID, stage, fromStatus, at, *actor, *reason)
				if err != nil {
					return nil, err
				}
				return []*domain.Event{ev}, nil
			}
		}
		wi, err = items.Transition(ctx, *id, req, expected)
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
		wi, expected, err := expectedSnapshot(ctx, items, *id, *expect)
		if err != nil {
			return workitemError(err)
		}
		if err := checkClaimQuality(ctx, root, wi); err != nil {
			return err
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

// expectedSnapshot reads the work item together with its raw bytes and turns
// the --expect version hash into the guard the mutations require: it accepts
// only when the snapshot hash matches the caller's view (fail-closed — a
// stale or unknown hash means no authority to mutate).
func expectedSnapshot(ctx context.Context, items *workitem.Store, id, expect string) (*domain.WorkItem, []byte, error) {
	wi, raw, err := items.ReadSnapshot(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if expect = strings.TrimSpace(expect); expect != "" {
		want, derr := hex.DecodeString(expect)
		sum := storage.HashBytes(raw)
		if derr != nil || len(want) != sha256.Size || !bytes.Equal(want, sum[:]) {
			return nil, nil, fmt.Errorf("version mismatch: work item changed since your read; rerun workitem get")
		}
	}
	return wi, raw, nil
}

// errWorkflowPolicy reports a workflow policy that a work item declared but
// that cannot be used (missing or invalid). Callers refuse rather than
// silently skipping gates (fail closed; M3.5 adds last-known-good).
func errWorkflowPolicy(err error) *codedError {
	return &codedError{code: CodeInvalid, kind: "workflow", msg: err.Error()}
}

// errGate renders a stage gate rejection with one problem per unmet
// requirement, located at the policy that declared the gate.
func errGate(policyFile, stage string, missing []string) *codedError {
	problems := make([]config.Problem, 0, len(missing))
	for _, m := range missing {
		problems = append(problems, config.Problem{File: policyFile, Field: "gates.stages." + stage, Reason: m})
	}
	word := "item"
	if len(missing) != 1 {
		word = "items"
	}
	return &codedError{
		code:     CodeInvalid,
		kind:     "gate",
		msg:      fmt.Sprintf("stage gate for %q is not satisfied (%d missing %s)", stage, len(missing), word),
		problems: problems,
	}
}

// errQuality renders a claim-time quality rejection with one problem per
// improvement item, located at the policy's threshold.
func errQuality(policyFile string, score, minScore int, improvements []string) *codedError {
	problems := make([]config.Problem, 0, len(improvements))
	for _, m := range improvements {
		problems = append(problems, config.Problem{File: policyFile, Field: "quality_gate.min_score", Reason: m})
	}
	return &codedError{
		code:     CodeInvalid,
		kind:     "quality",
		msg:      fmt.Sprintf("quality gate not satisfied: score %d is below min_score %d", score, minScore),
		problems: problems,
	}
}

// policyForWorkItem resolves the workflow policy a work item declares, or an
// empty resolution when it declares none. The current file is re-validated on
// every call; a failing file falls back to the last-known-good snapshot with
// the current failure reported in Resolution.Issue (实施计划 M3.5).
func policyForWorkItem(ctx context.Context, root string, wi *domain.WorkItem) (workflow.Resolution, error) {
	if wi.Workflow == nil {
		return workflow.Resolution{}, nil
	}
	id := wi.Workflow.ID
	if id == "" {
		return workflow.Resolution{}, fmt.Errorf("work item %s has a workflow instance without an id", wi.ID)
	}
	return workflow.Resolve(ctx, root, id)
}

// gateEvidence collects the evidence a stage gate consumes: artifact names,
// comment events and, for require_approval gates, the approved unconsumed
// approval matching the stage and the work item's current status (方案 §4.9).
// The matching approval id is returned so the advancing transaction can
// consume it atomically.
func gateEvidence(root string, wi *domain.WorkItem, stage string) (workflow.GateEvidence, string, error) {
	ctx := context.Background()
	artifacts, err := record.New(root).ListArtifacts(ctx)
	if err != nil {
		return workflow.GateEvidence{}, "", evidenceError(err)
	}
	names := map[string]bool{}
	for _, a := range artifacts {
		for _, related := range a.RelatedWorkItems {
			if related == wi.ID && a.Name != "" {
				names[a.Name] = true
			}
		}
	}
	byName := make([]string, 0, len(names))
	for n := range names {
		byName = append(byName, n)
	}
	sort.Strings(byName)
	comments, err := events.New(root).Read(ctx, events.Filter{
		Subject: &domain.Reference{Type: "workitem", ID: wi.ID},
		Type:    "comment",
	})
	if err != nil {
		return workflow.GateEvidence{}, "", evidenceError(err)
	}
	approvalID := ""
	matched, err := approval.MatchingGate(ctx, root, wi.ID, stage, wi.Status)
	switch {
	case err == nil:
		approvalID = matched.ID
	case errors.Is(err, approval.ErrNotFound):
	default:
		return workflow.GateEvidence{}, "", evidenceError(err)
	}
	return workflow.GateEvidence{
		ArtifactNames: byName,
		CommentCount:  len(comments),
		ApprovalReady: approvalID != "",
	}, approvalID, nil
}

func evidenceError(err error) error {
	if errors.Is(err, storage.ErrNotInitialized) {
		return errPrecondition("%v", err)
	}
	return errInternal("collect gate evidence: %v", err)
}

// checkTransitionGate refuses a transition whose target stage gate is not
// satisfied, listing every missing item. When the current policy file failed
// validation and the last-known-good snapshot took over, the returned notice
// carries that fact for the caller to surface.
//
// Known window: evidence (artifacts, comments) is collected before the
// transition transaction takes the project lock, so evidence removed in
// between is not re-checked. Gates are an application-level precondition,
// not a snapshot-isolated invariant; M4.2 lifts this wiring into the shared
// application service.
func checkTransitionGate(ctx context.Context, root string, wi *domain.WorkItem, target string) (string, string, error) {
	res, err := policyForWorkItem(ctx, root, wi)
	if err != nil {
		return "", "", errWorkflowPolicy(err)
	}
	if res.Policy == nil {
		return "", "", nil
	}
	notice := ""
	if res.Issue != nil {
		notice = fmt.Sprintf("workflow policy %q is invalid: %s; using %s", res.ID, res.Issue.String(), res.Source)
	}
	ev, approvalID, err := gateEvidence(root, wi, target)
	if err != nil {
		return "", "", err
	}
	gate := res.Policy.CheckGate(target, ev)
	if gate.Allowed {
		return notice, approvalID, nil
	}
	return notice, "", errGate(res.Policy.File, target, gate.Missing)
}

// checkClaimQuality refuses a claim whose work item scores below the policy's
// quality threshold. While the current policy file is invalid, claims are
// blocked outright — the last-known-good snapshot serves read paths only
// (方案 §5.3: 配置错误只阻塞新任务派发).
func checkClaimQuality(ctx context.Context, root string, wi *domain.WorkItem) error {
	res, err := policyForWorkItem(ctx, root, wi)
	if err != nil {
		return errWorkflowPolicy(err)
	}
	if res.Policy == nil {
		return nil
	}
	if res.Issue != nil {
		return errWorkflowPolicy(fmt.Errorf(
			"workflow policy %q is invalid: %s; claims stay blocked until the file is fixed (%s is retained for read paths)",
			res.ID, res.Issue.String(), res.Source))
	}
	quality := workflow.ScoreQuality(workflow.QualityInput{
		Title:              wi.Title,
		Description:        wi.Description,
		AcceptanceCriteria: wi.AcceptanceCriteria,
	})
	if !res.Policy.QualityGateBlocks(quality) {
		return nil
	}
	return errQuality(res.Policy.File, quality.Score, res.Policy.QualityGate.MinScore, quality.Improvements)
}

// runNext implements `devsys next`: the read-only readiness verdict and the
// single recommended next action (方案 §7.4, 实施计划 M3.4). Like doctor it
// never writes, recovers or creates the local lock; while pending
// transactions exist — or no writer ever ran — it reports without rendering
// business facts (方案 §15.4).
func runNext(stdout io.Writer, opts options, rest []string) error {
	if len(rest) != 0 {
		return errUsage("`devsys next` takes no arguments")
	}
	root, err := os.Getwd()
	if err != nil {
		return errInternal("resolve working directory: %v", err)
	}
	devsys := filepath.Join(root, project.DevsysDirName)
	if _, err := os.Stat(devsys); errors.Is(err, fs.ErrNotExist) {
		return errPrecondition("no %s/ in %s: run `devsys init` first", project.DevsysDirName, root)
	} else if err != nil {
		return errInternal("inspect %s: %v", devsys, err)
	}

	ctx := context.Background()
	doc, err := reconcile.Doctor(ctx, root, reconcile.Options{})
	if err != nil {
		return errInternal("next: inspect: %v", err)
	}
	in := next.Input{
		Now:              time.Now().UTC(),
		PendingTxns:      doc.PendingTransactions,
		ExpiredLeases:    doc.ExpiredLeases,
		OrphanLeases:     doc.OrphanLeases,
		UnreadableLeases: doc.UnreadableLeases,
		InspectionOK:     doc.InspectionOK,
		InspectionNote:   doc.Note,
		RecoverCommand:   `devsys recover --actor operator --reason "recover interrupted state"`,
	}
	// Business facts are only rendered from a trustworthy inspection
	// (pending transactions or no lock file both keep them out).
	if doc.InspectionOK && len(doc.PendingTransactions) == 0 {
		items, err := workitem.New(root).List(ctx)
		if err != nil {
			return errInternal("next: list work items: %v", err)
		}
		in.WorkItems = items
		pending, err := approval.New(root).List(ctx, approval.Filter{Status: approval.StatusPending})
		if err != nil {
			return errInternal("next: list approvals: %v", err)
		}
		for _, a := range pending {
			if a.InvalidatedAt != nil {
				continue
			}
			in.PendingApprovals = append(in.PendingApprovals, next.PendingApproval{ID: a.ID, WorkitemID: a.WorkItemID})
		}
	}
	md, problems := config.Diagnose(root)
	for _, p := range problems {
		in.MetadataProblems = append(in.MetadataProblems, p.String())
	}
	if md != nil && md.Project != nil {
		in.Milestones = md.Project.Milestones
	}
	// Invalid or missing policy files are recorded risks; the last-known-good
	// fallback keeps read paths usable while dispatch stays blocked.
	policies := workflow.Load(root)
	present := map[string]bool{}
	for _, res := range policies {
		name := res.File
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		present[strings.TrimSuffix(name, ".md")] = true
		for _, is := range res.Issues {
			if is.Severity == workflow.SeverityError {
				in.PolicyProblems = append(in.PolicyProblems, is.String())
			}
		}
	}
	for _, wi := range in.WorkItems {
		if wi.Workflow == nil || wi.Workflow.ID == "" || present[wi.Workflow.ID] {
			continue
		}
		in.PolicyProblems = append(in.PolicyProblems,
			fmt.Sprintf("work item %s references missing policy %q", wi.ID, wi.Workflow.ID))
	}

	rep := next.Evaluate(in)
	if opts.json {
		out := struct {
			OK bool `json:"ok"`
			next.Report
		}{OK: true, Report: rep}
		return json.NewEncoder(stdout).Encode(out)
	}
	if !opts.quiet {
		fmt.Fprintf(stdout, "readiness: %s\n", rep.Verdict)
		for _, r := range rep.Reasons {
			fmt.Fprintf(stdout, "  reason: %s\n", r)
		}
		for _, r := range rep.Risks {
			if r.WorkitemID != "" {
				fmt.Fprintf(stdout, "  risk: %s %s: %s\n", r.Kind, r.WorkitemID, r.Detail)
			} else {
				fmt.Fprintf(stdout, "  risk: %s: %s\n", r.Kind, r.Detail)
			}
		}
		for _, f := range rep.Fixes {
			fmt.Fprintf(stdout, "  fix: %s -> %s\n", f.Reason, f.Command)
		}
		n := rep.Next
		switch {
		case n.WorkitemID != "":
			fmt.Fprintf(stdout, "next: %s %s: %s\n", n.Action, n.WorkitemID, n.Reason)
		case n.MilestoneID != "":
			fmt.Fprintf(stdout, "next: %s %s: %s\n", n.Action, n.MilestoneID, n.Reason)
		default:
			fmt.Fprintf(stdout, "next: %s: %s\n", n.Action, n.Reason)
		}
	}
	return nil
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
