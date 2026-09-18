package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"workloom/internal/app"
	"workloom/internal/domain"
	"workloom/internal/harness"
)

// runDecision routes the decision family (方案 §8.2 decision_*).
func runDecision(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 {
		return errUsage("`devsys decision` needs a subcommand (list | get | create | approve)")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch rest[0] {
	case "list":
		if len(rest) != 1 {
			return errUsage("`devsys decision list` takes no arguments")
		}
		items, err := svc.DecisionList(ctx)
		if err != nil {
			return err
		}
		if opts.jsonl {
			return writeJSONL(stdout, items)
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK        bool               `json:"ok"`
				Decisions []*domain.Decision `json:"decisions"`
			}{OK: true, Decisions: items})
		}
		if !opts.quiet {
			if len(items) == 0 {
				fmt.Fprintln(stdout, "no decisions")
			}
			for _, d := range items {
				fmt.Fprintf(stdout, "%s\t%s\t%s\n", d.ID, d.Status, d.Title)
			}
		}
		return nil
	case "get":
		if len(rest) != 2 {
			return errUsage("decision get <id>")
		}
		view, err := svc.DecisionGet(ctx, rest[1])
		if err != nil {
			return err
		}
		return outputRecord(stdout, opts, view)
	case "create":
		fs := flag.NewFlagSet("decision create", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		title := fs.String("title", "", "decision title")
		decision := fs.String("decision", "", "what was decided")
		contextText := fs.String("context", "", "why the decision was needed")
		reasoning := fs.String("reasoning", "", "the reasoning behind it")
		optionsList := fs.String("options", "", "comma-separated options considered")
		consequences := fs.String("consequences", "", "comma-separated expected consequences")
		related := fs.String("related", "", "comma-separated related work items")
		by := fs.String("by", "", "author identity")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *title == "" || *decision == "" || *by == "" {
			return errUsage("decision create --title T --decision D --by <author> [--context C] [--reasoning R] [--options a,b] [--consequences a,b] [--related WLM-1,WLM-2]")
		}
		view, err := svc.DecisionCreate(ctx, app.CreateDecisionRequest{
			Title: *title, Context: *contextText, Decision: *decision, Reasoning: *reasoning,
			Options: splitList(*optionsList), Consequences: splitList(*consequences),
			RelatedWorkItems: splitList(*related), CreatedBy: *by,
		})
		if err != nil {
			return err
		}
		return outputRecord(stdout, opts, view)
	case "approve":
		fs := flag.NewFlagSet("decision approve", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "decision id")
		by := fs.String("by", "", "decider identity")
		expect := fs.String("expect", "", "version hash from decision get")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" || *by == "" {
			return errUsage("decision approve --id <decision-id> --by <decider> [--expect <hash>]")
		}
		view, err := svc.DecisionApprove(ctx, *id, *by, *expect)
		if err != nil {
			return err
		}
		return outputRecord(stdout, opts, view)
	default:
		return errUsage("unknown `devsys decision` subcommand %q", rest[0])
	}
}

// runFinding routes the finding family (方案 §8.2 finding_*).
func runFinding(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 {
		return errUsage("`devsys finding` needs a subcommand (list | get | create | resolve)")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch rest[0] {
	case "list":
		if len(rest) != 1 {
			return errUsage("`devsys finding list` takes no arguments")
		}
		items, err := svc.FindingList(ctx)
		if err != nil {
			return err
		}
		if opts.jsonl {
			return writeJSONL(stdout, items)
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK       bool              `json:"ok"`
				Findings []*domain.Finding `json:"findings"`
			}{OK: true, Findings: items})
		}
		if !opts.quiet {
			if len(items) == 0 {
				fmt.Fprintln(stdout, "no findings")
			}
			for _, f := range items {
				fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", f.ID, f.Status, f.Severity, f.Title)
			}
		}
		return nil
	case "get":
		if len(rest) != 2 {
			return errUsage("finding get <id>")
		}
		view, err := svc.FindingGet(ctx, rest[1])
		if err != nil {
			return err
		}
		return outputRecord(stdout, opts, view)
	case "create":
		fs := flag.NewFlagSet("finding create", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		kind := fs.String("type", "issue", "finding type")
		title := fs.String("title", "", "finding title")
		description := fs.String("description", "", "what was found")
		evidence := fs.String("evidence", "", "comma-separated evidence references")
		severity := fs.String("severity", "", "severity label")
		related := fs.String("related", "", "comma-separated related work items")
		runID := fs.String("run", "", "run that discovered it")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *title == "" || *description == "" {
			return errUsage("finding create --title T --description D [--type issue] [--evidence a,b] [--severity high] [--related WLM-1] [--run <run-id>]")
		}
		view, err := svc.FindingCreate(ctx, app.CreateFindingRequest{
			Type: *kind, Title: *title, Description: *description, Evidence: splitList(*evidence),
			Severity: *severity, RelatedWorkItems: splitList(*related), DiscoveredByRunID: *runID,
		})
		if err != nil {
			return err
		}
		return outputRecord(stdout, opts, view)
	case "resolve":
		fs := flag.NewFlagSet("finding resolve", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "finding id")
		resolution := fs.String("resolution", "", "how it was resolved")
		expect := fs.String("expect", "", "version hash from finding get")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" {
			return errUsage("finding resolve --id <finding-id> [--resolution R] [--expect <hash>]")
		}
		view, err := svc.FindingResolve(ctx, *id, *resolution, *expect)
		if err != nil {
			return err
		}
		return outputRecord(stdout, opts, view)
	default:
		return errUsage("unknown `devsys finding` subcommand %q", rest[0])
	}
}

// runEvent routes the event family (方案 §8.2 event_*).
func runEvent(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 {
		return errUsage("`devsys event` needs a subcommand (list | record)")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch rest[0] {
	case "list":
		fs := flag.NewFlagSet("event list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		subjectType := fs.String("subject-type", "", "subject type filter")
		subjectID := fs.String("subject", "", "subject id filter")
		kind := fs.String("type", "", "event type filter")
		limit := fs.Int("limit", 0, "keep only the newest N events")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
			return errUsage("event list [--subject-type workitem] [--subject <id>] [--type comment] [--limit N]")
		}
		items, err := svc.EventList(ctx, app.EventListRequest{
			SubjectType: *subjectType, SubjectID: *subjectID, Type: *kind, Limit: *limit,
		})
		if err != nil {
			return err
		}
		if opts.jsonl {
			return writeJSONL(stdout, items)
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK     bool            `json:"ok"`
				Events []*domain.Event `json:"events"`
			}{OK: true, Events: items})
		}
		if !opts.quiet {
			if len(items) == 0 {
				fmt.Fprintln(stdout, "no events")
			}
			for _, ev := range items {
				fmt.Fprintf(stdout, "%s\t%s\t%s:%s\t%s\n", ev.Time.Format(time.RFC3339), ev.Type, ev.Subject.Type, ev.Subject.ID, ev.Actor)
			}
		}
		return nil
	case "record":
		fs := flag.NewFlagSet("event record", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		kind := fs.String("type", "", "event type")
		subjectType := fs.String("subject-type", "workitem", "subject type")
		subjectID := fs.String("subject", "", "subject id")
		actor := fs.String("actor", "", "author identity")
		content := fs.String("content", "", "event body")
		replyTo := fs.String("reply-to", "", "event id this event replies to")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *kind == "" || *subjectID == "" || *actor == "" {
			return errUsage("event record --type <type> --subject <id> --actor <a> [--subject-type workitem] [--content C] [--reply-to <event-id>]")
		}
		ev, err := svc.EventRecord(ctx, app.RecordEventRequest{
			Type: *kind, Subject: domain.Reference{Type: *subjectType, ID: *subjectID},
			Actor: *actor, Content: *content, ReplyTo: *replyTo,
		})
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK    bool          `json:"ok"`
				Event *domain.Event `json:"event"`
			}{OK: true, Event: ev})
		}
		if !opts.quiet {
			fmt.Fprintln(stdout, ev.ID)
		}
		return nil
	default:
		return errUsage("unknown `devsys event` subcommand %q", rest[0])
	}
}

// runArtifact routes the artifact family (方案 §8.2 artifact_*).
func runArtifact(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 {
		return errUsage("`devsys artifact` needs a subcommand (list | get | register | update | history)")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch rest[0] {
	case "list":
		if len(rest) != 1 {
			return errUsage("`devsys artifact list` takes no arguments")
		}
		items, err := svc.ArtifactList(ctx)
		if err != nil {
			return err
		}
		if opts.jsonl {
			return writeJSONL(stdout, items)
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK        bool               `json:"ok"`
				Artifacts []*domain.Artifact `json:"artifacts"`
			}{OK: true, Artifacts: items})
		}
		if !opts.quiet {
			if len(items) == 0 {
				fmt.Fprintln(stdout, "no artifacts")
			}
			for _, a := range items {
				fmt.Fprintf(stdout, "%s\t%s\tv%d\t%s\n", a.ID, a.Status, a.Version, a.Name)
			}
		}
		return nil
	case "get":
		if len(rest) != 2 {
			return errUsage("artifact get <id>")
		}
		view, err := svc.ArtifactGet(ctx, rest[1])
		if err != nil {
			return err
		}
		return outputRecord(stdout, opts, view)
	case "register":
		fs := flag.NewFlagSet("artifact register", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		kind := fs.String("type", "document", "artifact type")
		name := fs.String("name", "", "artifact name")
		path := fs.String("path", "", "repository path")
		source := fs.String("source", "", "where it came from")
		runID := fs.String("run", "", "run that produced it")
		status := fs.String("status", "draft", "status")
		related := fs.String("related", "", "comma-separated related work items")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *name == "" {
			return errUsage("artifact register --name N [--type document] [--path P] [--source S] [--run <run-id>] [--status draft] [--related WLM-1]")
		}
		view, err := svc.ArtifactRegister(ctx, app.RegisterArtifactRequest{
			Type: *kind, Name: *name, Path: *path, Source: *source,
			CreatedByRunID: *runID, Status: *status, RelatedWorkItems: splitList(*related),
		})
		if err != nil {
			return err
		}
		return outputRecord(stdout, opts, view)
	case "update":
		fs := flag.NewFlagSet("artifact update", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "artifact id to supersede")
		status := fs.String("status", "", "new status")
		path := fs.String("path", "", "new path")
		source := fs.String("source", "", "new source")
		related := fs.String("related", "", "comma-separated related work items (replaces)")
		expect := fs.String("expect", "", "version hash from artifact get")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" {
			return errUsage("artifact update --id <artifact-id> [--status S] [--path P] [--source S] [--related a,b] [--expect <hash>]")
		}
		req := app.UpdateArtifactRequest{ID: *id, Expect: *expect, Status: *status, Path: *path, Source: *source}
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "related" {
				req.RelatedWorkItems = splitList(*related)
			}
		})
		view, err := svc.ArtifactUpdate(ctx, req)
		if err != nil {
			return err
		}
		return outputRecord(stdout, opts, view)
	case "history":
		if len(rest) != 2 {
			return errUsage("artifact history <id>")
		}
		items, err := svc.ArtifactHistory(ctx, rest[1])
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK        bool               `json:"ok"`
				Artifacts []*domain.Artifact `json:"artifacts"`
			}{OK: true, Artifacts: items})
		}
		if !opts.quiet {
			for _, a := range items {
				fmt.Fprintf(stdout, "%s\tv%d\t%s\n", a.ID, a.Version, a.Status)
			}
		}
		return nil
	default:
		return errUsage("unknown `devsys artifact` subcommand %q", rest[0])
	}
}

// outputRecord renders one record view (decision/finding/artifact).
func outputRecord(stdout io.Writer, opts options, view app.RecordView) error {
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK       bool             `json:"ok"`
			Decision *domain.Decision `json:"decision,omitempty"`
			Finding  *domain.Finding  `json:"finding,omitempty"`
			Artifact *domain.Artifact `json:"artifact,omitempty"`
			Version  string           `json:"version,omitempty"`
		}{OK: true, Decision: view.Decision, Finding: view.Finding, Artifact: view.Artifact, Version: view.Version})
	}
	if !opts.quiet {
		switch {
		case view.Decision != nil:
			fmt.Fprintf(stdout, "%s\t%s\t%s\nversion: %s\n", view.Decision.ID, view.Decision.Status, view.Decision.Title, view.Version)
		case view.Finding != nil:
			fmt.Fprintf(stdout, "%s\t%s\t%s\nversion: %s\n", view.Finding.ID, view.Finding.Status, view.Finding.Title, view.Version)
		case view.Artifact != nil:
			fmt.Fprintf(stdout, "%s\t%s\tv%d\t%s\nversion: %s\n", view.Artifact.ID, view.Artifact.Status, view.Artifact.Version, view.Artifact.Name, view.Version)
		}
	}
	return nil
}

// runRun routes the run family (方案 §8.2 run_*).
// orNoneText renders an absent hash the way the service does.
func orNoneText(sha string) string {
	if sha == "" {
		return "(none)"
	}
	return sha
}

func runRun(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 {
		return errUsage("`devsys run` needs a subcommand (list | get | log | create | update | heartbeat | exec | prompt | verify | complete | fail | cancel)")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch rest[0] {
	case "list":
		fs := flag.NewFlagSet("run list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		workitemID := fs.String("workitem", "", "filter by work item")
		status := fs.String("status", "", "filter by run status")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
			return errUsage("run list [--workitem <id>] [--status <status>]")
		}
		items, err := svc.RunList(ctx, app.RunListRequest{WorkItemID: *workitemID, Status: *status})
		if err != nil {
			return err
		}
		if opts.jsonl {
			return writeJSONL(stdout, items)
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK   bool          `json:"ok"`
				Runs []*domain.Run `json:"runs"`
			}{OK: true, Runs: items})
		}
		if !opts.quiet {
			if len(items) == 0 {
				fmt.Fprintln(stdout, "no runs")
			}
			for _, r := range items {
				fmt.Fprintf(stdout, "%s\t%s\t%s\tattempt=%d\n", r.ID, r.Status, r.WorkItemID, r.Attempt)
			}
		}
		return nil
	case "get":
		if len(rest) != 2 {
			return errUsage("run get <id>")
		}
		view, err := svc.RunGet(ctx, rest[1])
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK      bool        `json:"ok"`
				Run     *domain.Run `json:"run"`
				Version string      `json:"version"`
			}{OK: true, Run: view.Run, Version: view.Version})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s\t%s\t%s\nversion: %s\n", view.Run.ID, view.Run.Status, view.Run.WorkItemID, view.Version)
		}
		return nil
	case "log":
		fs := flag.NewFlagSet("run log", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "run id")
		limit := fs.Int("limit", 0, "keep only the newest N events")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" {
			return errUsage("run log --id <run-id> [--limit N]")
		}
		view, err := svc.RunLog(ctx, *id, *limit)
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK bool `json:"ok"`
				app.RunLogView
			}{OK: true, RunLogView: view})
		}
		if !opts.quiet {
			for _, line := range view.Logs {
				fmt.Fprintln(stdout, line)
			}
			for _, cmd := range view.Commands {
				fmt.Fprintf(stdout, "cmd: %s\n", cmd)
			}
			for _, t := range view.Tests {
				fmt.Fprintf(stdout, "test: %s\n", t)
			}
			for _, ev := range view.Events {
				fmt.Fprintf(stdout, "event: %s %s\n", ev.Type, ev.Actor)
			}
		}
		return nil
	case "create":
		fs := flag.NewFlagSet("run create", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		workitemID := fs.String("workitem", "", "work item id")
		phase := fs.String("phase", "", "initial phase")
		actor := fs.String("actor", "", "operator identity")
		reason := fs.String("reason", "", "why the run exists")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *workitemID == "" || *actor == "" || *reason == "" {
			return errUsage("run create --workitem <id> --actor <a> --reason <r> [--phase P]")
		}
		view, err := svc.RunCreate(ctx, app.CreateRunRequest{
			WorkItemID: *workitemID, Phase: *phase, Actor: *actor, Reason: *reason,
		})
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK      bool        `json:"ok"`
				Run     *domain.Run `json:"run"`
				Version string      `json:"version"`
			}{OK: true, Run: view.Run, Version: view.Version})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s\t%s\n", view.Run.ID, view.Run.Status)
		}
		return nil
	case "update":
		fs := flag.NewFlagSet("run update", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "run id")
		status := fs.String("status", "", "new status")
		phase := fs.String("phase", "", "new phase")
		logLine := fs.String("log", "", "log line to append")
		command := fs.String("command", "", "command to append")
		test := fs.String("test", "", "test to append")
		changed := fs.String("changed-files", "", "comma-separated changed files (replaces)")
		expect := fs.String("expect", "", "version hash from run get")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" {
			return errUsage("run update --id <run-id> [--status S] [--phase P] [--log L] [--command C] [--test T] [--changed-files a,b] [--expect <hash>]")
		}
		req := app.UpdateRunRequest{ID: *id, Expect: *expect}
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "status":
				req.Status = status
			case "phase":
				req.Phase = phase
			case "log":
				req.AddLogs = []string{*logLine}
			case "command":
				req.AddCommands = []string{*command}
			case "test":
				req.AddTests = []string{*test}
			case "changed-files":
				req.ChangedFiles = splitList(*changed)
			}
		})
		view, err := svc.RunUpdate(ctx, req)
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK      bool        `json:"ok"`
				Run     *domain.Run `json:"run"`
				Version string      `json:"version"`
			}{OK: true, Run: view.Run, Version: view.Version})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s\t%s\nversion: %s\n", view.Run.ID, view.Run.Status, view.Version)
		}
		return nil
	case "heartbeat":
		fs := flag.NewFlagSet("run heartbeat", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "run id")
		owner := fs.String("owner", "", "lease owner")
		token := fs.String("token", "", "lease token")
		actor := fs.String("actor", "", "operator identity")
		reason := fs.String("reason", "", "reason")
		extend := fs.Int("extend", 0, "lease extension in seconds")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" || *owner == "" || *token == "" || *actor == "" || *reason == "" {
			return errUsage("run heartbeat --id <run-id> --owner <o> --token <t> --actor <a> --reason <r> [--extend <seconds>]")
		}
		var extendBy time.Duration
		if *extend > 0 {
			extendBy = time.Duration(*extend) * time.Second
		}
		view, err := svc.RunHeartbeat(ctx, *id, *owner, *token, *actor, *reason, extendBy)
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK bool `json:"ok"`
				app.HeartbeatView
			}{OK: true, HeartbeatView: view})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s\t%s\tlease_until=%s\n", view.WorkitemID, view.RunID, view.LeaseUntil.Format(time.RFC3339))
		}
		return nil
	case "exec":
		fs := flag.NewFlagSet("run exec", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "run id")
		timeout := fs.Duration("timeout", 0, "terminate the process tree after this long (0 = no limit)")
		round := fs.Int("round", 0, "session round (default: continue the session)")
		actor := fs.String("actor", "", "who runs the command")
		reason := fs.String("reason", "", "why this attempt runs")
		if err := fs.Parse(rest[1:]); err != nil || *id == "" || *actor == "" || *reason == "" {
			return errUsage("run exec --id <run-id> --actor <a> --reason <r> [--timeout 30s] [--round N] -- <command...>")
		}
		argv := fs.Args()
		if len(argv) == 0 {
			return errUsage("run exec needs a command after -- (devsys run exec --id <run-id> -- <command...>)")
		}
		// Ctrl-C stops the attempt (and its process tree) instead of
		// leaving an orphan behind.
		runCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
		defer stop()
		// The command's output is mirrored live: stdout to stdout and
		// stderr to stderr, except under --json where everything goes to
		// stderr so stdout carries only the envelope (M4.5).
		sink := func(line harness.Line) {
			out := stdout
			if line.Stream == "stderr" || opts.json {
				out = opts.stderr
			}
			if out == nil {
				out = io.Discard
			}
			fmt.Fprintln(out, line.Text)
		}
		view, err := svc.RunExec(runCtx, app.RunExecRequest{
			RunID: *id, Argv: argv, Timeout: *timeout, Round: *round,
			Actor: *actor, Reason: *reason, Sink: sink,
		})
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK bool `json:"ok"`
				app.RunExecView
			}{OK: true, RunExecView: view})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s\t%s\tround=%d(%s)\tstatus=%s\texit=%d\tduration=%s\tlog=%s\n",
				view.RunID, view.Adapter, view.Round, view.Mode, view.Status,
				view.ExitCode, time.Duration(view.DurationMS)*time.Millisecond, view.Log)
			for _, warning := range view.Warnings {
				fmt.Fprintf(stdout, "warning: %s\n", warning)
			}
		}
		return nil
	case "verify":
		fs := flag.NewFlagSet("run verify", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "run id")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" {
			return errUsage("run verify --id <run-id>")
		}
		check, err := svc.RunVerify(ctx, *id)
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK bool `json:"ok"`
				app.CompletionCheck
			}{OK: true, CompletionCheck: check})
		}
		if !opts.quiet {
			verdict := "advanced"
			if !check.Advanced {
				verdict = "not advanced"
			}
			fmt.Fprintf(stdout, "%s\t%s\tclaim=%s\tcurrent=%s\n",
				check.RunID, verdict, orNoneText(check.ClaimHead), orNoneText(check.CurrentHead))
			if check.Reason != "" {
				fmt.Fprintf(stdout, "reason: %s\n", check.Reason)
			}
		}
		return nil
	case "prompt":
		fs := flag.NewFlagSet("run prompt", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "run id")
		round := fs.Int("round", 0, "round to assemble (default: the next one)")
		write := fs.Bool("write", false, "materialize the text under .devsys/local/runs/<id>/")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" {
			return errUsage("run prompt --id <run-id> [--round N] [--write]")
		}
		view, err := svc.RunPrompt(ctx, app.RunPromptRequest{RunID: *id, Round: *round, Write: *write})
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK bool `json:"ok"`
				app.RunPromptView
			}{OK: true, RunPromptView: view})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "# round %d (%s) hash=%s\n", view.Round, view.Mode, view.Hash)
			if view.Path != "" {
				fmt.Fprintf(stdout, "# written to %s\n", view.Path)
			}
			for _, notice := range view.Notices {
				fmt.Fprintf(stdout, "# notice: %s\n", notice)
			}
			fmt.Fprint(stdout, view.Text)
		}
		return nil
	case "complete", "fail", "cancel":
		outcome := map[string]string{
			"complete": app.RunSucceeded,
			"fail":     app.RunFailed,
			"cancel":   app.RunCanceled,
		}[rest[0]]
		fs := flag.NewFlagSet("run "+rest[0], flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "run id")
		expect := fs.String("expect", "", "version hash from run get")
		actor := fs.String("actor", "", "who decided the outcome")
		reason := fs.String("reason", "", "why the attempt ended this way")
		note := fs.String("note", "", "detail kept with the run's evidence")
		force := fs.Bool("force", false, "accept a completion the check refused (requires --by)")
		by := fs.String("by", "", "reviewer accepting a forced completion")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" || *actor == "" || *reason == "" {
			return errUsage("%s", "run "+rest[0]+" --id <run-id> [--expect <hash>] --actor <a> --reason <r> [--note <text>] [--force --by <reviewer>]")
		}
		view, err := svc.RunFinish(ctx, app.RunFinishRequest{
			RunID: *id, Expect: *expect, Outcome: outcome,
			Actor: *actor, Reason: *reason, Note: *note,
			Force: *force, By: *by,
		})
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK      bool        `json:"ok"`
				Run     *domain.Run `json:"run"`
				Version string      `json:"version"`
			}{OK: true, Run: view.Run, Version: view.Version})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s\t%s\n", view.Run.ID, view.Run.Status)
		}
		return nil
	default:
		return errUsage("unknown `devsys run` subcommand %q", rest[0])
	}
}

// runContext routes the context family (方案 §8.2 context_*).
func runContext(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 {
		return errUsage("`devsys context` needs a subcommand (get | workitem | refresh | compact)")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch rest[0] {
	case "get", "refresh":
		fs := flag.NewFlagSet("context "+rest[0], flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		limit := fs.Int("limit", 5, "max references per list")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
			return errUsage("context %s [--limit N]", rest[0])
		}
		var (
			view app.ContextView
			err  error
		)
		if rest[0] == "get" {
			view, err = svc.ContextGet(ctx, *limit)
		} else {
			view, err = svc.ContextRefresh(ctx, *limit)
		}
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK bool `json:"ok"`
				app.ContextView
			}{OK: true, ContextView: view})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "project: %s (%s)\nphase: %s\nreadiness: %s\n", view.Project.Name, view.Project.ID, view.Project.CurrentPhase, view.Verdict)
			for _, ref := range view.RecentDecisions {
				fmt.Fprintf(stdout, "  decision: %s\t%s\n", ref.Ref, ref.Title)
			}
			for _, ref := range view.RecentFindings {
				fmt.Fprintf(stdout, "  finding: %s\t%s\n", ref.Ref, ref.Title)
			}
			for _, ref := range view.RecentArtifacts {
				fmt.Fprintf(stdout, "  artifact: %s\t%s\n", ref.Ref, ref.Title)
			}
			for _, n := range view.Notices {
				fmt.Fprintf(stdout, "  notice: %s\n", n)
			}
		}
		return nil
	case "compact":
		if len(rest) != 1 {
			return errUsage("`devsys context compact` takes no arguments")
		}
		view, err := svc.ContextCompact(ctx)
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK bool `json:"ok"`
				app.ContextView
			}{OK: true, ContextView: view})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", view.Project.ID, view.Project.CurrentPhase, view.Verdict)
		}
		return nil
	case "workitem":
		fs := flag.NewFlagSet("context workitem", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "work item id")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" {
			return errUsage("context workitem --id <workitem-id>")
		}
		view, err := svc.ContextForWorkitem(ctx, *id)
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK bool `json:"ok"`
				app.WorkitemContextView
			}{OK: true, WorkitemContextView: view})
		}
		if !opts.quiet {
			fmt.Fprintf(stdout, "%s\t%s\t%s\nversion: %s\n", view.WorkItem.ID, view.WorkItem.Status, view.WorkItem.Title, view.Version)
			for _, ref := range view.Decisions {
				fmt.Fprintf(stdout, "  decision: %s\t%s\n", ref.Ref, ref.Title)
			}
			for _, ref := range view.Findings {
				fmt.Fprintf(stdout, "  finding: %s\t%s\n", ref.Ref, ref.Title)
			}
			for _, ref := range view.Artifacts {
				fmt.Fprintf(stdout, "  artifact: %s\t%s\n", ref.Ref, ref.Title)
			}
			for _, c := range view.Comments {
				fmt.Fprintf(stdout, "  comment: %s\t%s\n", c.ID, c.Actor)
			}
		}
		return nil
	default:
		return errUsage("unknown `devsys context` subcommand %q", rest[0])
	}
}

// runKnowledge routes the knowledge family: `status` reports the layer's
// availability (the index layer arrives with M5, 方案 §12.6).
func runKnowledge(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 || rest[0] != "status" {
		return errUsage("`devsys knowledge` needs a subcommand (status)")
	}
	if len(rest) != 1 {
		return errUsage("`devsys knowledge status` takes no arguments")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	view, err := svc.KnowledgeStatus(context.Background())
	if err != nil {
		return err
	}
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK bool `json:"ok"`
			app.KnowledgeStatusView
		}{OK: true, KnowledgeStatusView: view})
	}
	if !opts.quiet {
		fmt.Fprintf(stdout, "knowledge: %s (%s)\n%s\n", view.Status, view.Layer, view.Reason)
		for _, p := range view.Paths {
			fmt.Fprintf(stdout, "  path: %s\n", p)
		}
	}
	return nil
}

// runSession routes the session family: `start` is the one-shot orientation
// a new agent session runs first (方案 §8.3, 实施计划 M4.4).
func runSession(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 || rest[0] != "start" {
		return errUsage("`devsys session` needs a subcommand (start)")
	}
	fs := flag.NewFlagSet("session start", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	harness := fs.String("harness", "", "harness name")
	agentID := fs.String("agent", "", "agent identity")
	workspace := fs.String("workspace", "", "workspace path")
	intent := fs.String("intent", "", "what the agent intends to do")
	compact := fs.Bool("compact", false, "trim the context payload")
	if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
		return errUsage("session start [--harness codex] [--agent <id>] [--workspace <path>] [--intent <text>] [--compact]")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	view, err := svc.SessionStart(context.Background(), app.SessionRequest{
		Harness: *harness, AgentID: *agentID, Workspace: *workspace, Intent: *intent, Compact: *compact,
	})
	if err != nil {
		return err
	}
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK bool `json:"ok"`
			app.SessionView
		}{OK: true, SessionView: view})
	}
	if !opts.quiet {
		fmt.Fprintf(stdout, "project: %s (%s)\nphase: %s\n", view.Project.Name, view.Project.ID, view.Project.CurrentPhase)
		if view.State.Summary != "" {
			fmt.Fprintf(stdout, "state: %s\n", view.State.Summary)
		}
		for _, wi := range view.CurrentWorkitems {
			fmt.Fprintf(stdout, "  workitem: %s\t%s\t%s\n", wi.ID, wi.Status, wi.Title)
		}
		action := view.RecommendedNextAction
		if action.ID != "" {
			fmt.Fprintf(stdout, "next: %s %s: %s\n", action.Type, action.ID, action.Reason)
		} else {
			fmt.Fprintf(stdout, "next: %s: %s\n", action.Type, action.Reason)
		}
		if action.Command != "" {
			fmt.Fprintf(stdout, "command: %s\n", action.Command)
		}
		for _, n := range view.Notices {
			fmt.Fprintf(stdout, "  notice: %s\n", n)
		}
	}
	return nil
}
