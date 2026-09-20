// Package cli implements the devsys command surface: global switches, command
// dispatch, output rendering and the exit-code taxonomy. Keeping dispatch out
// of main() makes the exit contract testable.
//
// Since M4.2 every business operation lives in the shared application service
// (internal/app); this package only parses flags, calls the service and
// renders. The MCP server (internal/mcp) calls the same service, so no entry
// point can bypass a constraint the others enforce.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"workloom/internal/app"
	"workloom/internal/config"
	"workloom/internal/domain"
	"workloom/internal/project"
	"workloom/internal/version"
	"workloom/internal/workflow"
)

// Exit codes, pinned by tests so scripts may rely on them:
//
//	0  success
//	1  internal error
//	2  usage error
//	3  precondition error
//	4  invalid managed state
//
// `knowledge status` (and a refresh that found nothing to regenerate) adds a
// second, reserved convention on top: 10 = the page layer is stale, 11 = the
// page layer has not been generated (方案 §12.5). Those commands answer with
// that state through stdout and then exit with the matching code; no other
// command uses 10 or 11.
const (
	CodeOK           = 0
	CodeInternal     = 1
	CodeUsage        = 2
	CodePrecondition = 3
	// CodeInvalid reports managed state that exists but cannot be trusted:
	// parse errors, unknown keys, wrong types, unsupported schema_version.
	// Write commands refuse; read-only diagnostics keep working (方案 §14.1).
	CodeInvalid = 4
	// CodeStale and CodeMissing are the knowledge layer's own codes
	// (方案 §12.5): a stale page layer, and a page layer that does not exist.
	CodeStale   = 10
	CodeMissing = 11
)

const usage = `devsys - project-local agent development infrastructure

usage:
  devsys [--json | --jsonl] [--quiet] <command>

commands:
  init          create .devsys/ in the current git repository root
  config check  validate the managed metadata files (read-only)
  search <text> search project-local text records
  project       list | get | status | blueprint | update | state-update
  workitem      list | get | create | update | transition | claim | release | start | block | complete | comment | dep
  workflow check  validate workflow policy files (read-only)
  workflow list|get|start|next|step-complete|pause|resume|cancel  drive a work item's workflow instance
  next          readiness verdict and the recommended next action (read-only)
  approval      list | get | request | approve | reject governance approvals
  decision      list | get | create | approve decision records
  finding       list | get | create | resolve finding records
  event         list | record project events (append-only)
  artifact      list | get | register | update | history artifact records
  run           list | get | log | create | update | heartbeat | exec | prompt | complete | fail | cancel
  worktree      prepare | remove | list execution workspaces (方案 §4.8)
  dispatch      one scheduling tick: recover, reconcile, dispatch (--watch loops; refused while a merge conflicts)
  archive       events --before <YYYY-MM> | runs --id <id,...> move JSONL streams to .devsys/archive/ (conservative, no delete)
  workspace     view [--limit N] | build --static [--out DIR] [--limit N] | serve [--host 127.0.0.1] [--port N] read-only project view / offline site / local service (方案 §17)
  knowledge         status | scan | validate [dir|page.md...] | refresh
  prime         alias for session start --compact (minimal orientation for agents)
  session start  one-shot session orientation (project, work in flight, next action)
  wire          AGENTS.md block [--dry-run] | --check | --skill | --print-mcp <codex|claude|opencode>
  doctor        report transactions and orphaned claims (read-only)
  recover       recover transactions, release expired/orphaned claims
  repair        --dry-run proposes repairs; --apply --confirm <digest> applies (unmerged paths surface as human-only notes)
  mcp serve     serve the Model Context Protocol over stdio (--profile ... --tier core|standard)

options:
  --json      machine-readable output (one JSON document)
  --jsonl     machine-readable lists: one JSON record per line
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
	json   bool
	jsonl  bool
	quiet  bool
	stderr io.Writer
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

// toCoded maps the shared application taxonomy onto exit codes and the CLI's
// rendering shape. Unknown errors are internal failures.
func toCoded(err error) *codedError {
	var ae *app.Error
	if errors.As(err, &ae) {
		code := CodeInvalid
		switch ae.Class() {
		case app.KindUsage:
			code = CodeUsage
		case app.KindPrecondition:
			code = CodePrecondition
		case app.KindInternal:
			code = CodeInternal
		}
		return &codedError{code: code, kind: ae.Kind, msg: ae.Message, problems: ae.Problems}
	}
	var ce *codedError
	if errors.As(err, &ce) {
		return ce
	}
	return errInternal("%v", err)
}

// writeJSONL renders a list as one JSON record per line: the exchange format
// for external scripts, which read a record without parsing an envelope.
// Records keep the same field names as the --json envelope carries.
func writeJSONL[T any](w io.Writer, items []T) error {
	enc := json.NewEncoder(w)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			return errInternal("write jsonl: %v", err)
		}
	}
	return nil
}

// appService binds the service to the project the command works on: the
// resolved root (DEVSYS_PROJECT_ROOT, else the nearest ancestor carrying
// `.devsys/`), so a command run from a subdirectory reaches the project.
// An agent working inside its workspace (a git worktree) gets that variable
// from the attempt's environment (方案 §4.8), so its reports land in the
// project's own state instead of the copy of .devsys/ the worktree carries.
func appService() (*app.Service, error) {
	return resolveService()
}

// requireProjectRoot verifies .devsys/ exists before a command runs, so the
// precondition message names the directory the operator is in.
func requireProjectRoot() (*app.Service, error) {
	svc, err := appService()
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Stat(filepath.Join(svc.Root, project.DevsysDirName)); errors.Is(statErr, os.ErrNotExist) {
		return nil, errPrecondition("no %s/ in %s: run `devsys init` first", project.DevsysDirName, svc.Root)
	} else if statErr != nil {
		return nil, errInternal("inspect %s: %v", filepath.Join(svc.Root, project.DevsysDirName), statErr)
	} else if !info.IsDir() {
		return nil, errPrecondition("%s is not a directory in %s", project.DevsysDirName, svc.Root)
	}
	return svc, nil
}

// Run executes one CLI invocation against the process streams and returns
// the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	return RunWithIO(args, os.Stdin, stdout, stderr)
}

// RunWithIO is Run with an explicit stdin, so `mcp serve` — and its tests —
// can drive the stdio protocol over injected streams.
func RunWithIO(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	opts := options{stderr: stderr}
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if a == "" || a[0] != '-' {
			break
		}
		switch a {
		case "--json":
			opts.json = true
		case "--jsonl":
			opts.jsonl = true
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
	if opts.json && opts.jsonl {
		return render(stderr, opts, errUsage("--json and --jsonl are mutually exclusive"))
	}

	cmd, rest := args[i], args[i+1:]
	switch cmd {
	case "init":
		if len(rest) > 0 {
			return render(stderr, opts, errUsage("`devsys init` takes no arguments (got %q)", rest[0]))
		}
		return render(stderr, opts, runInit(stdout, opts))
	case "sync":
		return render(stderr, opts, runSync(stdout, opts, rest))
	case "config":
		return render(stderr, opts, runConfigCheck(stdout, opts, rest))
	case "search":
		return render(stderr, opts, runSearch(stdout, opts, rest))
	case "project":
		return render(stderr, opts, runProject(stdout, opts, rest))
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
	case "decision":
		return render(stderr, opts, runDecision(stdout, opts, rest))
	case "finding":
		return render(stderr, opts, runFinding(stdout, opts, rest))
	case "event":
		return render(stderr, opts, runEvent(stdout, opts, rest))
	case "artifact":
		return render(stderr, opts, runArtifact(stdout, opts, rest))
	case "run":
		return render(stderr, opts, runRun(stdout, opts, rest))
	case "worktree":
		return render(stderr, opts, runWorktree(stdout, opts, rest))
	case "dispatch":
		return render(stderr, opts, runDispatch(stdout, opts, rest))
	case "archive":
		return render(stderr, opts, runArchive(stdout, opts, rest))
	case "context":
		return render(stderr, opts, runContext(stdout, opts, rest))
	case "workspace":
		return render(stderr, opts, runWorkspace(stdout, opts, rest))
	case "knowledge":
		return render(stderr, opts, runKnowledge(stdout, opts, rest))
	case "prime":
		return render(stderr, opts, runPrime(stdout, opts, rest))
	case "session":
		return render(stderr, opts, runSession(stdout, opts, rest))
	case "wire":
		return render(stderr, opts, runWire(stdout, opts, rest))
	case "mcp":
		return render(stderr, opts, runMCP(opts, stdin, stdout, stderr, rest))
	default:
		return render(stderr, opts, errUsage("unknown command %q", cmd))
	}
}

// codedExit is an exit code whose output the command already wrote. Commands
// that report a state rather than success or failure (`knowledge status`
// answers fresh/stale/missing) use it so the exit code carries the state
// without the error path repeating what stdout already said.
type codedExit struct{ code int }

func (e *codedExit) Error() string { return fmt.Sprintf("exit code %d", e.code) }

// exitWithCode returns an exit-code-only error.
func exitWithCode(code int) error { return &codedExit{code: code} }

// render prints an error (if any) and returns the exit code; nil means success
// with output already written by the command.
func render(stderr io.Writer, opts options, err error) int {
	if err == nil {
		return CodeOK
	}
	var exit *codedExit
	if errors.As(err, &exit) {
		return exit.code
	}
	ce := toCoded(err)
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
	svc, err := appService()
	if err != nil {
		return err
	}
	res, regPath, err := svc.ProjectCreate(context.Background(), "")
	if err != nil {
		return err
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

// runWire implements `devsys wire`: the idempotent AGENTS.md managed block
// (方案 §12.5, 实施计划 M4.6). Content outside the block — including other
// tools' blocks — is never touched. `--check` is the read-only environment
// report, `--skill` writes the agent skill files, `--print-mcp` prints a
// copy-paste MCP client snippet; all three are read-only except `--skill`.
func runWire(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("wire", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dryRun := fs.Bool("dry-run", false, "preview the change without writing")
	check := fs.Bool("check", false, "environment report (read-only, exit 0)")
	skill := fs.Bool("skill", false, "write .agents/skills/devsys/ files (idempotent)")
	printMCP := fs.String("print-mcp", "", "print MCP client snippet (codex|claude|opencode)")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 {
		return errUsage("`devsys wire` [--dry-run] [--check] [--skill] [--print-mcp codex|claude|opencode]")
	}
	if *check || *printMCP != "" {
		if *dryRun || *skill {
			return errUsage("`devsys wire --check/--print-mcp` takes no other flags")
		}
		svc, err := appService()
		if err != nil {
			return err
		}
		if *printMCP != "" {
			snippet, err := app.MCPSnippet(*printMCP, os.Args[0])
			if err != nil {
				return err
			}
			if opts.json {
				return json.NewEncoder(stdout).Encode(struct {
					OK      bool   `json:"ok"`
					Harness string `json:"harness"`
					Snippet string `json:"snippet"`
				}{OK: true, Harness: *printMCP, Snippet: snippet})
			}
			if !opts.quiet {
				fmt.Fprintln(stdout, snippet)
			}
			return nil
		}
		view := svc.WireCheck()
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK bool `json:"ok"`
				app.WireCheckView
			}{OK: true, WireCheckView: view})
		}
		if !opts.quiet {
			for _, l := range view.Lines {
				mark := "x"
				if l.OK {
					mark = "v"
				}
				fmt.Fprintf(stdout, "[%s] %s: %s\n", mark, l.Name, l.Detail)
			}
		}
		return nil
	}
	if *skill {
		if *dryRun {
			return errUsage("`devsys wire --skill` takes no other flags")
		}
		svc, err := requireProjectRoot()
		if err != nil {
			return err
		}
		changed, err := svc.WriteSkill()
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK      bool     `json:"ok"`
				Changed []string `json:"changed"`
			}{OK: true, Changed: changed})
		}
		if !opts.quiet {
			if len(changed) == 0 {
				fmt.Fprintln(stdout, "skill: already installed (no change)")
			} else {
				for _, p := range changed {
					fmt.Fprintf(stdout, "skill: wrote %s\n", p)
				}
			}
		}
		return nil
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	view, err := svc.Wire(context.Background(), *dryRun)
	if err != nil {
		return err
	}
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK     bool `json:"ok"`
			DryRun bool `json:"dry_run,omitempty"`
			app.WireView
		}{OK: true, DryRun: *dryRun, WireView: view})
	}
	if !opts.quiet {
		switch {
		case !view.Changed:
			fmt.Fprintf(stdout, "%s: already wired (no change)\n", view.Path)
		case *dryRun:
			fmt.Fprintf(stdout, "would update %s:\n%s", view.Path, view.Diff)
		case view.Created:
			fmt.Fprintf(stdout, "created %s\n", view.Path)
		default:
			fmt.Fprintf(stdout, "updated %s\n", view.Path)
		}
	}
	return nil
}

// latestFlag registers the --latest convenience flag next to --expect:
// --latest is the explicit spelling of the empty-expect path (read the
// current snapshot inside the same operation, still CAS).
func latestFlag(fs *flag.FlagSet) *bool {
	return fs.Bool("latest", false, "use the current snapshot (single-operator convenience)")
}

// checkLatest rejects --latest together with --expect and reports the usage
// string of the calling subcommand.
func checkLatest(latest bool, expect, usage string) error {
	if latest && strings.TrimSpace(expect) != "" {
		return errUsage("%s (pass --expect or --latest, not both)", usage)
	}
	return nil
}

// runProject routes the project family (方案 §8.2 project_*).
func runProject(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 {
		return errUsage("`devsys project` needs a subcommand (list | get | status | blueprint | update | state-update)")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch rest[0] {
	case "list":
		if len(rest) != 1 {
			return errUsage("`devsys project list` takes no arguments")
		}
		entries, err := svc.ProjectList(ctx)
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK       bool               `json:"ok"`
				Projects []app.ProjectEntry `json:"projects"`
			}{OK: true, Projects: entries})
		}
		if !opts.quiet {
			if len(entries) == 0 {
				fmt.Fprintln(stdout, "no registered projects")
			}
			for _, e := range entries {
				mark := " "
				if e.Current {
					mark = "*"
				}
				fmt.Fprintf(stdout, "%s %s\t%s\texists=%t\n", mark, e.ID, e.Path, e.Exists)
			}
		}
		return nil
	case "get":
		if len(rest) != 1 {
			return errUsage("`devsys project get` takes no arguments")
		}
		view, err := svc.ProjectGet(ctx)
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK      bool            `json:"ok"`
				Project *domain.Project `json:"project"`
				Version string          `json:"version"`
			}{OK: true, Project: view.Project, Version: view.Version})
		}
		if !opts.quiet {
			p := view.Project
			fmt.Fprintf(stdout, "%s\t%s\t%s\nphase: %s\nversion: %s\n", p.ID, p.Name, p.Status, p.CurrentPhase, view.Version)
		}
		return nil
	case "status":
		if len(rest) != 1 {
			return errUsage("`devsys project status` takes no arguments")
		}
		view, err := svc.ProjectStatus(ctx)
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK bool `json:"ok"`
				app.ProjectStatusView
			}{OK: true, ProjectStatusView: view})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "project: %s (%s)\n", view.Project.Name, view.Project.ID)
			fmt.Fprintf(stdout, "phase: %s\n", view.Project.CurrentPhase)
			fmt.Fprintf(stdout, "readiness: %s\n", view.Verdict)
			statuses := make([]string, 0, len(view.Counts))
			for status := range view.Counts {
				statuses = append(statuses, status)
			}
			sortStrings(statuses)
			for _, status := range statuses {
				fmt.Fprintf(stdout, "  %s: %d\n", status, view.Counts[status])
			}
			for _, r := range view.Risks {
				fmt.Fprintf(stdout, "  risk: %s: %s\n", r.Kind, r.Detail)
			}
		}
		return nil
	case "blueprint":
		if len(rest) != 1 {
			return errUsage("`devsys project blueprint` takes no arguments")
		}
		art, err := svc.ProjectBlueprint(ctx)
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK       bool             `json:"ok"`
				Artifact *domain.Artifact `json:"artifact"`
			}{OK: true, Artifact: art})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s\t%s\tv%d\t%s\n", art.ID, art.Name, art.Version, art.Path)
		}
		return nil
	case "update":
		fs := flag.NewFlagSet("project update", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		name := fs.String("name", "", "project name")
		description := fs.String("description", "", "project description")
		status := fs.String("status", "", "project status")
		phase := fs.String("phase", "", "current phase")
		expect := fs.String("expect", "", "version hash from project get")
		latest := latestFlag(fs)
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
			return errUsage("project update [--name N] [--description D] [--status S] [--phase P] [--expect <hash> | --latest]")
		}
		if err := checkLatest(*latest, *expect, "project update [--name N] [--description D] [--status S] [--phase P] [--expect <hash> | --latest]"); err != nil {
			return err
		}
		req := app.UpdateProjectRequest{Expect: *expect}
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "name":
				req.Name = name
			case "description":
				req.Description = description
			case "status":
				req.Status = status
			case "phase":
				req.CurrentPhase = phase
			}
		})
		view, err := svc.ProjectUpdate(ctx, req)
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK      bool            `json:"ok"`
				Project *domain.Project `json:"project"`
				Version string          `json:"version"`
			}{OK: true, Project: view.Project, Version: view.Version})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s\t%s\t%s\nversion: %s\n", view.Project.ID, view.Project.Name, view.Project.Status, view.Version)
		}
		return nil
	case "state-update":
		fs := flag.NewFlagSet("project state-update", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		summary := fs.String("summary", "", "state summary")
		risks := fs.String("risks", "", "comma-separated risks")
		blockers := fs.String("blockers", "", "comma-separated blockers")
		focus := fs.String("next-focus", "", "comma-separated next focus items")
		expect := fs.String("expect", "", "version hash from project state")
		latest := latestFlag(fs)
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
			return errUsage("project state-update [--summary S] [--risks a,b] [--blockers a,b] [--next-focus a,b] [--expect <hash> | --latest]")
		}
		if err := checkLatest(*latest, *expect, "project state-update [--summary S] [--risks a,b] [--blockers a,b] [--next-focus a,b] [--expect <hash> | --latest]"); err != nil {
			return err
		}
		req := app.UpdateStateRequest{Expect: *expect}
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "summary":
				req.Summary = summary
			case "risks":
				req.Risks = splitList(*risks)
			case "blockers":
				req.Blockers = splitList(*blockers)
			case "next-focus":
				req.NextFocus = splitList(*focus)
			}
		})
		view, err := svc.ProjectStateUpdate(ctx, req)
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK      bool                     `json:"ok"`
				State   *domain.CurrentStateFile `json:"state"`
				Version string                   `json:"version"`
			}{OK: true, State: view.State, Version: view.Version})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s\nversion: %s\n", view.State.Summary, view.Version)
		}
		return nil
	default:
		return errUsage("unknown `devsys project` subcommand %q", rest[0])
	}
}

// splitList parses a comma-separated flag value into a trimmed list.
func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func sortStrings(list []string) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j] < list[j-1]; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// runWorkitem exposes the work item family through the shared service.
func runWorkitem(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 {
		return errUsage("workitem needs a subcommand (list | get | create | update | transition | claim | release | start | block | complete | comment | dep)")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch rest[0] {
	case "list":
		fs := flag.NewFlagSet("workitem list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		status := fs.String("status", "", "filter by status")
		kind := fs.String("type", "", "filter by type")
		parent := fs.String("parent", "", "filter by parent work item")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
			return errUsage("workitem list [--status S] [--type T] [--parent <id>]")
		}
		items, err := svc.WorkitemList(ctx, app.WorkitemFilter{Status: *status, Type: *kind, Parent: *parent})
		if err != nil {
			return err
		}
		if opts.jsonl {
			return writeJSONL(stdout, items)
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK    bool               `json:"ok"`
				Items []*domain.WorkItem `json:"items"`
			}{OK: true, Items: items})
		}
		if !opts.quiet {
			if len(items) == 0 {
				fmt.Fprintln(stdout, "no work items")
			}
			for _, wi := range items {
				fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", wi.ID, wi.Status, wi.Type, wi.Title)
			}
		}
		return nil
	case "get":
		if len(rest) != 2 {
			return errUsage("workitem get <id>")
		}
		view, err := svc.WorkitemGet(ctx, rest[1])
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK      bool             `json:"ok"`
				Item    *domain.WorkItem `json:"item"`
				Version string           `json:"version"`
			}{OK: true, Item: view.Item, Version: view.Version})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s\t%s\t%s\nversion: %s\n", view.Item.ID, view.Item.Status, view.Item.Title, view.Version)
		}
		return nil
	case "create":
		fs := flag.NewFlagSet("workitem create", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		title := fs.String("title", "", "work item title")
		prefix := fs.String("prefix", "WLM", "ID prefix")
		description := fs.String("description", "", "work item description")
		acceptance := fs.String("acceptance", "", "comma-separated acceptance criteria")
		kind := fs.String("type", "task", "work item type")
		priority := fs.Int("priority", 0, "numeric priority (higher first)")
		parent := fs.String("parent", "", "parent work item id")
		actor := fs.String("actor", "", "operator")
		reason := fs.String("reason", "", "creation reason")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *title == "" || *actor == "" || *reason == "" {
			return errUsage("workitem create --title <title> --actor <actor> --reason <reason> [--prefix WLM] [--description D] [--acceptance a,b] [--type T] [--priority N] [--parent <id>]")
		}
		view, err := svc.WorkitemCreate(ctx, app.CreateWorkitemRequest{
			Title: *title, Description: *description, Type: *kind, Prefix: *prefix,
			ParentID: *parent, Priority: *priority, Actor: *actor, Reason: *reason,
			AcceptanceCriteria: splitList(*acceptance),
		})
		if err != nil {
			return err
		}
		return outputWorkitem(stdout, opts, view)
	case "update":
		fs := flag.NewFlagSet("workitem update", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "work item id")
		title := fs.String("title", "", "new title")
		description := fs.String("description", "", "new description")
		priority := fs.Int("priority", 0, "new priority")
		acceptance := fs.String("acceptance", "", "comma-separated acceptance criteria (replaces the list)")
		assignedAgent := fs.String("assigned-agent", "", "agent identity to assign (empty clears)")
		assignedHarness := fs.String("assigned-harness", "", "harness adapter dispatch should drive (empty clears)")
		expect := fs.String("expect", "", "version hash from workitem get")
		latest := latestFlag(fs)
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" {
			return errUsage("workitem update --id <id> [--title T] [--description D] [--priority N] [--acceptance a,b] [--assigned-agent A] [--assigned-harness H] [--expect <hash> | --latest]")
		}
		if err := checkLatest(*latest, *expect, "workitem update --id <id> [--expect <hash> | --latest]"); err != nil {
			return err
		}
		req := app.UpdateWorkitemRequest{Expect: *expect}
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "title":
				req.Title = title
			case "description":
				req.Description = description
			case "priority":
				req.Priority = priority
			case "acceptance":
				req.AcceptanceCriteria = splitList(*acceptance)
			case "assigned-agent":
				req.AssignedAgent = assignedAgent
			case "assigned-harness":
				req.AssignedHarness = assignedHarness
			}
		})
		view, err := svc.WorkitemUpdate(ctx, *id, req)
		if err != nil {
			return err
		}
		return outputWorkitem(stdout, opts, view)
	case "transition":
		fs := flag.NewFlagSet("workitem transition", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "work item id")
		to := fs.String("to", "", "target status")
		actor := fs.String("actor", "", "operator")
		reason := fs.String("reason", "", "transition reason")
		expect := fs.String("expect", "", "version hash from workitem get")
		latest := latestFlag(fs)
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
			return errUsage("workitem transition --id <id> --to <status> --actor <a> --reason <r> [--expect <hash> | --latest]")
		}
		if err := checkLatest(*latest, *expect, "workitem transition --id <id> --to <status> --actor <a> --reason <r> [--expect <hash> | --latest]"); err != nil {
			return err
		}
		view, notice, err := svc.WorkitemTransition(ctx, *id, *to, *actor, *reason, *expect)
		if notice != "" && !opts.quiet {
			fmt.Fprintf(stdout, "warning: %s\n", notice)
		}
		if err != nil {
			return err
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s: %s\n", view.Item.ID, view.Item.Status)
		}
		return nil
	case "claim":
		fs := flag.NewFlagSet("workitem claim", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "work item id")
		owner := fs.String("owner", "", "claimer identity")
		reason := fs.String("reason", "", "claim reason")
		expect := fs.String("expect", "", "version hash from workitem get")
		latest := latestFlag(fs)
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
			return errUsage("workitem claim --id <id> --owner <owner> --reason <reason> [--expect <hash> | --latest]")
		}
		if err := checkLatest(*latest, *expect, "workitem claim --id <id> --owner <owner> --reason <reason> [--expect <hash> | --latest]"); err != nil {
			return err
		}
		res, err := svc.WorkitemClaim(ctx, *id, *owner, *reason, *expect)
		if err != nil {
			return err
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
	case "release":
		fs := flag.NewFlagSet("workitem release", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "work item id")
		owner := fs.String("owner", "", "lease owner")
		token := fs.String("token", "", "lease token")
		actor := fs.String("actor", "", "operator")
		reason := fs.String("reason", "", "release reason")
		expect := fs.String("expect", "", "version hash from workitem get")
		latest := latestFlag(fs)
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" || *owner == "" || *token == "" || *actor == "" || *reason == "" {
			return errUsage("workitem release --id <id> --owner <owner> --token <token> --actor <a> --reason <r> [--expect <hash> | --latest]")
		}
		if err := checkLatest(*latest, *expect, "workitem release --id <id> --owner <owner> --token <token> --actor <a> --reason <r> [--expect <hash> | --latest]"); err != nil {
			return err
		}
		view, err := svc.WorkitemRelease(ctx, *id, *owner, *token, *actor, *reason, *expect)
		if err != nil {
			return err
		}
		return outputWorkitem(stdout, opts, view)
	case "start":
		fs := flag.NewFlagSet("workitem start", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "work item id")
		owner := fs.String("owner", "", "lease owner")
		token := fs.String("token", "", "lease token")
		actor := fs.String("actor", "", "operator")
		reason := fs.String("reason", "", "start reason")
		expect := fs.String("expect", "", "version hash from workitem get")
		latest := latestFlag(fs)
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" || *owner == "" || *token == "" || *actor == "" || *reason == "" {
			return errUsage("workitem start --id <id> --owner <owner> --token <token> --actor <a> --reason <r> [--expect <hash> | --latest]")
		}
		if err := checkLatest(*latest, *expect, "workitem start --id <id> --owner <owner> --token <token> --actor <a> --reason <r> [--expect <hash> | --latest]"); err != nil {
			return err
		}
		res, err := svc.WorkitemStart(ctx, *id, *owner, *token, *actor, *reason, *expect)
		if err != nil {
			return err
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
			fmt.Fprintf(stdout, "started %s: run=%s\n", res.WorkitemID, res.RunID)
		}
		return nil
	case "block", "complete":
		target := domain.StatusBlocked
		if rest[0] == "complete" {
			target = domain.StatusDone
		}
		fs := flag.NewFlagSet("workitem "+rest[0], flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "work item id")
		actor := fs.String("actor", "", "operator")
		reason := fs.String("reason", "", "reason")
		expect := fs.String("expect", "", "version hash from workitem get")
		latest := latestFlag(fs)
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
			return errUsage("workitem %s --id <id> --actor <a> --reason <r> [--expect <hash> | --latest]", rest[0])
		}
		if err := checkLatest(*latest, *expect, "workitem "+rest[0]+" --id <id> --actor <a> --reason <r> [--expect <hash> | --latest]"); err != nil {
			return err
		}
		view, notice, err := svc.WorkitemTransition(ctx, *id, target, *actor, *reason, *expect)
		if notice != "" && !opts.quiet {
			fmt.Fprintf(stdout, "warning: %s\n", notice)
		}
		if err != nil {
			return err
		}
		return outputWorkitem(stdout, opts, view)
	case "comment":
		fs := flag.NewFlagSet("workitem comment", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "work item id")
		text := fs.String("text", "", "comment text")
		actor := fs.String("actor", "", "author")
		replyTo := fs.String("reply-to", "", "event id this comment replies to")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
			return errUsage("workitem comment --id <id> --text <text> --actor <a> [--reply-to <event-id>]")
		}
		view, err := svc.WorkitemComment(ctx, *id, *text, *actor, *replyTo)
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK    bool          `json:"ok"`
				Event *domain.Event `json:"event"`
			}{OK: true, Event: view.Event})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s\n", view.Event.ID)
		}
		return nil
	case "dep":
		if len(rest) < 2 {
			return errUsage("workitem dep add|remove --id <id> --depends-on <id> [--expect <hash> | --latest]")
		}
		action := rest[1]
		if action != "add" && action != "remove" {
			return errUsage("workitem dep add|remove --id <id> --depends-on <id> [--expect <hash> | --latest]")
		}
		fs := flag.NewFlagSet("workitem dep "+action, flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "work item id")
		dependsOn := fs.String("depends-on", "", "dependency target id")
		expect := fs.String("expect", "", "version hash from workitem get")
		latest := latestFlag(fs)
		if err := fs.Parse(rest[2:]); err != nil || fs.NArg() != 0 || *id == "" || *dependsOn == "" {
			return errUsage("workitem dep %s --id <id> --depends-on <id> [--expect <hash> | --latest]", action)
		}
		if err := checkLatest(*latest, *expect, "workitem dep "+action+" --id <id> --depends-on <id> [--expect <hash> | --latest]"); err != nil {
			return err
		}
		var view app.WorkItemView
		var err error
		if action == "add" {
			view, err = svc.WorkitemAddDependency(ctx, *id, *dependsOn, *expect)
		} else {
			view, err = svc.WorkitemRemoveDependency(ctx, *id, *dependsOn, *expect)
		}
		if err != nil {
			return err
		}
		return outputWorkitem(stdout, opts, view)
	default:
		return errUsage("unknown workitem operation %q", rest[0])
	}
}

// outputWorkitem renders one work item and its version hash.
func outputWorkitem(stdout io.Writer, opts options, view app.WorkItemView) error {
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK      bool             `json:"ok"`
			Item    *domain.WorkItem `json:"item"`
			Version string           `json:"version"`
		}{OK: true, Item: view.Item, Version: view.Version})
	}
	if !opts.quiet {
		fmt.Fprintf(stdout, "%s\t%s\t%s\nversion: %s\n", view.Item.ID, view.Item.Status, view.Item.Title, view.Version)
	}
	return nil
}

// runWorkflow routes the workflow family: `check` validates policy files
// (read-only); the instance subcommands drive a work item's workflow
// instance through the shared service (实施计划 M3.6).
func runWorkflow(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 {
		return errUsage("`devsys workflow` needs a subcommand (check | list | get | start | next | step-complete | pause | resume | cancel)")
	}
	switch rest[0] {
	case "check":
		return runWorkflowCheck(stdout, opts, rest[1:])
	case "list":
		return runWorkflowList(stdout, opts, rest[1:])
	case "get":
		return runWorkflowGet(stdout, opts, rest[1:])
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

// runWorkflowCheck implements `devsys workflow check`: the read-only policy
// diagnostic (方案 §5.3, 实施计划 M3.1). Unknown keys are warnings: they are
// recorded, and a policy carrying only warnings still loads.
func runWorkflowCheck(stdout io.Writer, opts options, rest []string) error {
	if len(rest) != 0 {
		return errUsage("`devsys workflow check` takes no arguments (got %q)", rest[0])
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	policies, warnings, err := svc.WorkflowList(context.Background())
	if err != nil {
		return err
	}
	if opts.json {
		out := struct {
			OK       bool                `json:"ok"`
			Root     string              `json:"root"`
			Policies []app.PolicySummary `json:"policies"`
			Warnings []workflow.Issue    `json:"warnings,omitempty"`
		}{OK: true, Root: svc.Root, Policies: []app.PolicySummary{}}
		out.Policies = append(out.Policies, policies...)
		out.Warnings = append(out.Warnings, warnings...)
		return json.NewEncoder(stdout).Encode(out)
	}
	if !opts.quiet {
		fmt.Fprintf(stdout, "workflow ok: %d policies checked\n", len(policies))
	}
	// Warnings are diagnostics, not success banners: they stay visible under
	// --quiet so recorded issues are not silently dropped.
	for _, w := range warnings {
		fmt.Fprintf(stdout, "warning: %s\n", w.String())
	}
	return nil
}

// runWorkflowList lists the policy files with their identity (M4.2).
func runWorkflowList(stdout io.Writer, opts options, rest []string) error {
	if len(rest) != 0 {
		return errUsage("`devsys workflow list` takes no arguments (got %q)", rest[0])
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	policies, warnings, err := svc.WorkflowList(context.Background())
	if err != nil {
		return err
	}
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK       bool                `json:"ok"`
			Policies []app.PolicySummary `json:"policies"`
			Warnings []workflow.Issue    `json:"warnings,omitempty"`
		}{OK: true, Policies: policies, Warnings: warnings})
	}
	if !opts.quiet {
		if len(policies) == 0 {
			fmt.Fprintln(stdout, "no workflow policies")
		}
		for _, p := range policies {
			fmt.Fprintf(stdout, "%s\t%s\t%s\tv%d\n", p.ID, p.Name, p.File, p.Version)
		}
	}
	for _, w := range warnings {
		fmt.Fprintf(stdout, "warning: %s\n", w.String())
	}
	return nil
}

// runWorkflowGet reads one work item's instance and candidates.
func runWorkflowGet(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("workflow get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "work item id")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *id == "" {
		return errUsage("workflow get --id <workitem-id>")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	view, err := svc.WorkflowGet(context.Background(), *id)
	if err != nil {
		return err
	}
	return outputWorkflow(stdout, opts, view)
}

// outputWorkflow renders one work item's instance state and, for read
// operations, its candidates.
func outputWorkflow(stdout io.Writer, opts options, view app.WorkflowView) error {
	if opts.json {
		out := struct {
			OK         bool                     `json:"ok"`
			WorkitemID string                   `json:"workitem_id"`
			Workflow   *domain.WorkflowInstance `json:"workflow"`
			Candidates []workflow.StepCandidate `json:"candidates,omitempty"`
			Notice     string                   `json:"notice,omitempty"`
		}{OK: true, WorkitemID: view.WorkitemID, Workflow: view.Workflow, Candidates: view.Candidates, Notice: view.Notice}
		return json.NewEncoder(stdout).Encode(out)
	}
	if !opts.quiet {
		if view.Notice != "" {
			fmt.Fprintf(stdout, "warning: %s\n", view.Notice)
		}
		if view.Workflow == nil {
			fmt.Fprintf(stdout, "%s: no workflow instance\n", view.WorkitemID)
			return nil
		}
		inst := view.Workflow
		fmt.Fprintf(stdout, "%s: workflow %s step %s paused=%t\n", view.WorkitemID, inst.ID, inst.Step, inst.Paused)
		for _, c := range view.Candidates {
			when := c.When
			if when == "" {
				when = "<unconditional>"
			}
			fmt.Fprintf(stdout, "  candidate: to=%s when=%q satisfied=%t\n", c.To, when, c.Satisfied)
		}
		if !inst.Paused {
			for _, c := range view.Candidates {
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
	latest := latestFlag(fs)
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *id == "" || *policyID == "" || *actor == "" || *reason == "" {
		return errUsage("workflow start --id <workitem-id> --policy <id> --actor <a> --reason <r> [--owner <o> --token <t>] [--expect <hash> | --latest]")
	}
	if err := checkLatest(*latest, *expect, "workflow start --id <workitem-id> --policy <id> --actor <a> --reason <r> [--owner <o> --token <t>] [--expect <hash> | --latest]"); err != nil {
		return err
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	view, err := svc.WorkflowStart(context.Background(), *id, *policyID, *actor, *reason, *owner, *token, *expect)
	if err != nil {
		return err
	}
	return outputWorkflow(stdout, opts, view)
}

func runWorkflowNext(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("workflow next", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "work item id")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *id == "" {
		return errUsage("workflow next --id <workitem-id>")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	view, err := svc.WorkflowStepNext(context.Background(), *id)
	if err != nil {
		return err
	}
	return outputWorkflow(stdout, opts, view)
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
	latest := latestFlag(fs)
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *id == "" || *actor == "" || *reason == "" {
		return errUsage("workflow step-complete --id <workitem-id> [--to <step>] --actor <a> --reason <r> [--owner <o> --token <t>] [--expect <hash> | --latest]")
	}
	if err := checkLatest(*latest, *expect, "workflow step-complete --id <workitem-id> [--to <step>] --actor <a> --reason <r> [--owner <o> --token <t>] [--expect <hash> | --latest]"); err != nil {
		return err
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	view, err := svc.WorkflowStepComplete(context.Background(), *id, *to, *actor, *reason, *owner, *token, *expect)
	if view.Notice != "" && !opts.quiet {
		fmt.Fprintf(stdout, "warning: %s\n", view.Notice)
	}
	if err != nil {
		return err
	}
	return outputWorkflow(stdout, opts, view)
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
	latest := latestFlag(fs)
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *id == "" || *actor == "" || *reason == "" {
		return errUsage("workflow %s --id <workitem-id> --actor <a> --reason <r> [--owner <o> --token <t>] [--expect <hash> | --latest]", action)
	}
	if err := checkLatest(*latest, *expect, "workflow "+action+" --id <workitem-id> --actor <a> --reason <r> [--owner <o> --token <t>] [--expect <hash> | --latest]"); err != nil {
		return err
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	view, err := svc.WorkflowSignal(context.Background(), action, *id, *actor, *reason, *owner, *token, *expect)
	if err != nil {
		return err
	}
	return outputWorkflow(stdout, opts, view)
}

// runApproval routes the approval lifecycle (方案 §4.9, 实施计划 M3.7):
// request → approve | reject. Consumption itself happens inside the
// gate-checked transition, never as a standalone command, so no caller can
// consume without advancing.
func runApproval(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 {
		return errUsage("`devsys approval` needs a subcommand (list | get | request | approve | reject)")
	}
	switch rest[0] {
	case "list":
		return runApprovalList(stdout, opts, rest[1:])
	case "get":
		return runApprovalGet(stdout, opts, rest[1:])
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

// approvalState renders the effective state of an approval for humans. A
// consumed or invalidated approval keeps its decided status on disk, but
// neither can be used again (方案 §4.9), so the list must not show them as
// usable; --json still carries the raw fields.
func approvalState(a *domain.Approval) string {
	switch {
	case a.InvalidatedAt != nil:
		return "invalidated"
	case a.ConsumedAt != nil:
		return "consumed"
	default:
		return a.Status
	}
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
			apr.ID, approvalState(apr), apr.Scope, apr.WorkItemID, apr.Stage, apr.RequestedStatus)
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
	case "", "pending", "approved", "rejected":
	default:
		return errUsage("approval list --status must be pending, approved or rejected (got %q)", *status)
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	approvals, err := svc.ApprovalList(context.Background(), app.ApprovalFilter{WorkItemID: *workitemID, Status: *status})
	if err != nil {
		return err
	}
	if opts.jsonl {
		return writeJSONL(stdout, approvals)
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
				a.ID, approvalState(a), a.Scope, a.WorkItemID, a.Stage, a.RequestedStatus)
		}
	}
	return nil
}

func runApprovalGet(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("approval get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	id := fs.String("id", "", "approval id")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *id == "" {
		return errUsage("approval get --id <approval-id>")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	apr, err := svc.ApprovalGet(context.Background(), *id)
	if err != nil {
		return err
	}
	return outputApproval(stdout, opts, apr)
}

func runApprovalRequest(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("approval request", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	workitemID := fs.String("id", "", "work item id")
	stage := fs.String("stage", "", "gate stage the approval targets")
	scope := fs.String("scope", "stage_gate", "stage_gate or action")
	runID := fs.String("run", "", "run that triggered the request")
	actor := fs.String("actor", "", "requester")
	reason := fs.String("reason", "", "why the approval is needed")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *workitemID == "" || *actor == "" || *reason == "" {
		return errUsage("approval request --id <workitem-id> --stage <stage> [--scope stage_gate|action] [--run <run-id>] --actor <a> --reason <r>")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	apr, warning, err := svc.ApprovalRequest(context.Background(), app.RequestApprovalRequest{
		WorkItemID: *workitemID, Stage: *stage, Scope: *scope, RunID: *runID, Actor: *actor, Reason: *reason,
	})
	if err != nil {
		return err
	}
	if warning != "" && !opts.quiet {
		fmt.Fprintf(stdout, "warning: %s\n", warning)
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
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	ctx := context.Background()
	if approve {
		apr, err := svc.ApprovalApprove(ctx, app.DecideApprovalRequest{ID: *id, By: *by, Comment: *comment})
		if err != nil {
			return err
		}
		return outputApproval(stdout, opts, apr)
	}
	// Rejection of a stage gate commits the decision and the work item
	// disposition in ONE transaction, so a failed disposition leaves the
	// approval pending instead of half-applying (方案 §4.9).
	apr, disposition, err := svc.ApprovalReject(ctx, app.DecideApprovalRequest{ID: *id, By: *by, Reason: *reason})
	if err != nil {
		return err
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

// runNext implements `devsys next`: the read-only readiness verdict and the
// single recommended next action (方案 §7.4, 实施计划 M3.4). Like doctor it
// never writes, recovers or creates the local lock; while pending
// transactions exist — or no writer ever ran — it reports without rendering
// business facts (方案 §15.4).
func runNext(stdout io.Writer, opts options, rest []string) error {
	if len(rest) != 0 {
		return errUsage("`devsys next` takes no arguments")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	report, _, err := svc.Next(context.Background())
	if err != nil {
		return err
	}
	if opts.json {
		out := struct {
			OK      bool     `json:"ok"`
			Verdict string   `json:"verdict"`
			Reasons []string `json:"reasons"`
			Risks   any      `json:"risks"`
			Fixes   any      `json:"fixes,omitempty"`
			Next    any      `json:"next"`
		}{OK: true, Verdict: report.Verdict, Reasons: report.Reasons, Risks: report.Risks, Fixes: report.Fixes, Next: report.Next}
		return json.NewEncoder(stdout).Encode(out)
	}
	if !opts.quiet {
		fmt.Fprintf(stdout, "readiness: %s\n", report.Verdict)
		for _, r := range report.Reasons {
			fmt.Fprintf(stdout, "  reason: %s\n", r)
		}
		for _, r := range report.Risks {
			if r.WorkitemID != "" {
				fmt.Fprintf(stdout, "  risk: %s %s: %s\n", r.Kind, r.WorkitemID, r.Detail)
			} else {
				fmt.Fprintf(stdout, "  risk: %s: %s\n", r.Kind, r.Detail)
			}
		}
		for _, f := range report.Fixes {
			fmt.Fprintf(stdout, "  fix: %s -> %s\n", f.Reason, f.Command)
		}
		n := report.Next
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
