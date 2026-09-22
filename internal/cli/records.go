package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/JAYY513/Workloom/internal/app"
	"github.com/JAYY513/Workloom/internal/config"
	"github.com/JAYY513/Workloom/internal/domain"
	"github.com/JAYY513/Workloom/internal/harness"
)

// runDecision routes the decision family (方案 §8.2 decision_*).
func runDecision(stdout io.Writer, opts options, rest []string) error {
	if familyUsage(stdout, rest, "`workloom decision` needs a subcommand (list | get | create | approve)") {
		return nil
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch rest[0] {
	case "list":
		if len(rest) != 1 {
			return errUsage("`workloom decision list` takes no arguments")
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
		latest := latestFlag(fs)
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" || *by == "" {
			return errUsage("decision approve --id <decision-id> --by <decider> [--expect <hash> | --latest]")
		}
		if err := checkLatest(*latest, *expect, "decision approve --id <decision-id> --by <decider> [--expect <hash> | --latest]"); err != nil {
			return err
		}
		view, err := svc.DecisionApprove(ctx, *id, *by, *expect)
		if err != nil {
			return err
		}
		return outputRecord(stdout, opts, view)
	default:
		return errUsage("unknown `workloom decision` subcommand %q", rest[0])
	}
}

// runFinding routes the finding family (方案 §8.2 finding_*).
func runFinding(stdout io.Writer, opts options, rest []string) error {
	if familyUsage(stdout, rest, "`workloom finding` needs a subcommand (list | get | create | resolve)") {
		return nil
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch rest[0] {
	case "list":
		if len(rest) != 1 {
			return errUsage("`workloom finding list` takes no arguments")
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
		latest := latestFlag(fs)
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" {
			return errUsage("finding resolve --id <finding-id> [--resolution R] [--expect <hash> | --latest]")
		}
		if err := checkLatest(*latest, *expect, "finding resolve --id <finding-id> [--resolution R] [--expect <hash> | --latest]"); err != nil {
			return err
		}
		view, err := svc.FindingResolve(ctx, *id, *resolution, *expect)
		if err != nil {
			return err
		}
		return outputRecord(stdout, opts, view)
	default:
		return errUsage("unknown `workloom finding` subcommand %q", rest[0])
	}
}

// runEvent routes the event family (方案 §8.2 event_*).
func runEvent(stdout io.Writer, opts options, rest []string) error {
	if familyUsage(stdout, rest, "`workloom event` needs a subcommand (list | record)") {
		return nil
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
		return errUsage("unknown `workloom event` subcommand %q", rest[0])
	}
}

// runArtifact routes the artifact family (方案 §8.2 artifact_*).
func runArtifact(stdout io.Writer, opts options, rest []string) error {
	if familyUsage(stdout, rest, "`workloom artifact` needs a subcommand (list | get | register | update | history)") {
		return nil
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch rest[0] {
	case "list":
		if len(rest) != 1 {
			return errUsage("`workloom artifact list` takes no arguments")
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
		actor := fs.String("actor", "", "operator (audit trail)")
		reason := fs.String("reason", "", "registration reason (audit trail)")
		const registerUsage = "artifact register --name N --actor <a> --reason <r> [--type document] [--path P] [--source S] [--run <run-id>] [--status draft] [--related WLM-1]"
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *name == "" {
			return errUsage("%s", registerUsage)
		}
		view, err := svc.ArtifactRegister(ctx, app.RegisterArtifactRequest{
			Type: *kind, Name: *name, Path: *path, Source: *source,
			CreatedByRunID: *runID, Status: *status, RelatedWorkItems: splitList(*related),
			Actor: *actor, Reason: *reason,
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
		latest := latestFlag(fs)
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" {
			return errUsage("artifact update --id <artifact-id> [--status S] [--path P] [--source S] [--related a,b] [--expect <hash> | --latest]")
		}
		if err := checkLatest(*latest, *expect, "artifact update --id <artifact-id> [--status S] [--path P] [--source S] [--related a,b] [--expect <hash> | --latest]"); err != nil {
			return err
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
		return errUsage("unknown `workloom artifact` subcommand %q", rest[0])
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
	if familyUsage(stdout, rest, "`workloom run` needs a subcommand (list | get | log | create | update | heartbeat | exec | prompt | verify | complete | fail | cancel)") {
		return nil
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
		latest := latestFlag(fs)
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" {
			return errUsage("run update --id <run-id> [--status S] [--phase P] [--log L] [--command C] [--test T] [--changed-files a,b] [--expect <hash> | --latest]")
		}
		if err := checkLatest(*latest, *expect, "run update --id <run-id> [--status S] [--phase P] [--log L] [--command C] [--test T] [--changed-files a,b] [--expect <hash> | --latest]"); err != nil {
			return err
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
		harnessName := fs.String("harness", "", "drive this harness adapter (shell | codex | opencode | claude)")
		model := fs.String("model", "", "model the harness should use")
		actor := fs.String("actor", "", "who runs the command")
		reason := fs.String("reason", "", "why this attempt runs")
		if err := fs.Parse(rest[1:]); err != nil || *id == "" || *actor == "" || *reason == "" {
			return errUsage("run exec --id <run-id> --actor <a> --reason <r> [--timeout 30s] [--round N] [--harness <name> [--model <m>]] [-- <command...>]")
		}
		argv := fs.Args()
		if *harnessName == "" && len(argv) == 0 {
			return errUsage("run exec needs a command after -- or a --harness (workloom run exec --id <run-id> -- <command...>)")
		}
		if *harnessName != "" && len(argv) > 0 {
			return errUsage("run exec takes either --harness or a command, not both")
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
			Harness: *harnessName, Model: *model,
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
		latest := latestFlag(fs)
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" || *actor == "" || *reason == "" {
			return errUsage("%s", "run "+rest[0]+" --id <run-id> [--expect <hash> | --latest] --actor <a> --reason <r> [--note <text>] [--force --by <reviewer>]")
		}
		if err := checkLatest(*latest, *expect, "run "+rest[0]+" --id <run-id> [--expect <hash> | --latest] --actor <a> --reason <r> [--note <text>] [--force --by <reviewer>]"); err != nil {
			return err
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
		return errUsage("unknown `workloom run` subcommand %q", rest[0])
	}
}

// runContext routes the context family (方案 §8.2 context_*).
func runContext(stdout io.Writer, opts options, rest []string) error {
	if familyUsage(stdout, rest, "`workloom context` needs a subcommand (get | workitem | refresh | compact)") {
		return nil
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
		task := fs.String("task", "", "assemble the layered context for one work item")
		paths := fs.String("path", "", "comma-separated files this task expects to touch")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
			return errUsage("context %s [--limit N] [--task <workitem-id> [--path a,b]]", rest[0])
		}
		if *task != "" {
			return runContextTask(stdout, opts, svc, ctx, *task, splitPaths(*paths), *limit, rest[0] == "refresh")
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
			return errUsage("`workloom context compact` takes no arguments")
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
		pathsFlag := fs.String("path", "", "comma-separated files this task expects to touch")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 || *id == "" {
			return errUsage("context workitem --id <workitem-id> [--path a,b]")
		}
		paths := splitPaths(*pathsFlag)
		view, err := svc.ContextForWorkitem(ctx, *id, paths)
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
		return errUsage("unknown `workloom context` subcommand %q", rest[0])
	}
}

// runContextTask renders the layered assembly for one task: the project
// summary, the task with its records, and the knowledge pages it touches
// (方案 §11.1).
func runContextTask(stdout io.Writer, opts options, svc *app.Service, ctx context.Context, id string, paths []string, limit int, refresh bool) error {
	view, err := svc.TaskContext(ctx, id, paths, limit, refresh)
	if err != nil {
		return err
	}
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK bool `json:"ok"`
			app.TaskContext
		}{OK: true, TaskContext: view})
	}
	if !opts.quiet {
		fmt.Fprintf(stdout, "project: %s (%s)\nphase: %s\nreadiness: %s\n",
			view.Summary.Project.Name, view.Summary.Project.ID, view.Summary.Project.CurrentPhase, view.Summary.Verdict)
		fmt.Fprintf(stdout, "task: %s\t%s\t%s\t%s\n", view.Task.WorkItem.ID, view.Task.WorkItem.Status, view.Task.WorkItem.Type, view.Task.WorkItem.Title)
		for _, ref := range view.Task.Decisions {
			fmt.Fprintf(stdout, "  decision: %s\t%s\n", ref.Ref, ref.Title)
		}
		for _, ref := range view.Task.Findings {
			fmt.Fprintf(stdout, "  finding: %s\t%s\n", ref.Ref, ref.Title)
		}
		for _, ref := range view.Task.Artifacts {
			fmt.Fprintf(stdout, "  artifact: %s\t%s\n", ref.Ref, ref.Title)
		}
		for _, n := range view.Summary.Notices {
			fmt.Fprintf(stdout, "  notice: %s\n", n)
		}
	}
	// The knowledge references are the part an agent acts on (read these
	// paths); they stay visible under --quiet like the other findings.
	if view.Task.Knowledge.Notice != "" {
		fmt.Fprintf(stdout, "  knowledge: %s\n", view.Task.Knowledge.Notice)
	}
	for _, page := range view.Task.Knowledge.Pages {
		stale := ""
		if page.Stale {
			stale = "  [stale]"
		}
		fmt.Fprintf(stdout, "  knowledge: %s\t%s\t(%s: %s)%s\n", page.Path, page.Description, page.Match, page.Reason, stale)
	}
	return nil
}

// splitPaths reads a comma-separated path list; empty entries are dropped so
// `--path a,,b` behaves like `--path a,b`.
func splitPaths(list string) []string {
	var out []string
	for _, item := range strings.Split(list, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// runKnowledge routes the knowledge family: `status` reports the layer's
// availability, `scan` builds the index-layer snapshot of the working tree
// (M5.2), and `validate` checks the page layer's front matter contract
// (M5.1, 方案 §12.5).
func runKnowledge(stdout io.Writer, opts options, rest []string) error {
	if familyUsage(stdout, rest, "`workloom knowledge` needs a subcommand (status, scan, validate, refresh)") {
		return nil
	}
	switch rest[0] {
	case "status":
		if len(rest) != 1 {
			return errUsage("`workloom knowledge status` takes no arguments")
		}
		return runKnowledgeStatus(stdout, opts)
	case "refresh":
		return runKnowledgeRefresh(stdout, opts, rest[1:])
	case "validate":
		return runKnowledgeValidate(stdout, opts, rest[1:])
	case "scan":
		if len(rest) != 1 {
			return errUsage("`workloom knowledge scan` takes no arguments")
		}
		return runKnowledgeScan(stdout, opts)
	default:
		return errUsage("unknown `workloom knowledge` subcommand %q", rest[0])
	}
}

// runKnowledgeScan implements `workloom knowledge scan`: build the index layer's
// snapshot of the working tree (M5.2).
func runKnowledgeScan(stdout io.Writer, opts options) error {
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	view, err := svc.KnowledgeScan(context.Background())
	if err != nil {
		return err
	}
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK bool `json:"ok"`
			app.KnowledgeScanView
		}{OK: true, KnowledgeScanView: view})
	}
	if !opts.quiet {
		fmt.Fprintf(stdout, "knowledge scan: %d files, %s, %d languages -> %s\n",
			view.Files, humanBytes(view.TotalSize), len(view.Languages), view.File)
	}
	// Exclusions are the audit trail for "the index is smaller than the tree",
	// so they stay visible under --quiet.
	for _, exclusion := range view.Excluded {
		fmt.Fprintf(stdout, "  excluded: %s  (%s)\n", exclusion.Path, exclusion.Reason)
	}
	return nil
}

// humanBytes renders a byte count the way the scan summary reads.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// runKnowledgeValidate implements `workloom knowledge validate`: the page
// layer's format gate, meant to run in CI. Problems are printed with their
// location and field, and any error-level problem exits 4.
func runKnowledgeValidate(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("knowledge validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(rest); err != nil {
		return errUsage("knowledge validate [<dir|page.md>...]")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	view, err := svc.KnowledgeValidate(context.Background(), fs.Args())
	if err != nil {
		return err
	}
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK bool `json:"ok"`
			app.KnowledgeValidateView
		}{OK: true, KnowledgeValidateView: view})
	}
	if !opts.quiet {
		switch {
		case view.Missing:
			fmt.Fprintln(stdout, "knowledge pages: none (page layer not generated; 方案 §12.6 degradation)")
		default:
			fmt.Fprintf(stdout, "knowledge ok: %d pages, %d errors, %d warnings\n",
				len(view.Pages), view.Errors, view.Warnings)
		}
	}
	// Warnings are diagnostics, not success banners: they stay visible under
	// --quiet so a page that loads with advisories says so.
	for _, issue := range view.Issues {
		if issue.Severity == config.SeverityWarning {
			fmt.Fprintf(stdout, "%s\n", issue.String())
		}
	}
	return nil
}

// runKnowledgeStatus implements `workloom knowledge status`: the freshness gate
// CI and hooks read. The exit code carries the state (0 fresh, 10 stale,
// 11 missing — 方案 §12.5) after the report has been written.
func runKnowledgeStatus(stdout io.Writer, opts options) error {
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	view, err := svc.KnowledgeStatus(context.Background())
	if err != nil {
		return err
	}
	if opts.json {
		if err := json.NewEncoder(stdout).Encode(struct {
			OK bool `json:"ok"`
			app.KnowledgeStatusView
		}{OK: true, KnowledgeStatusView: view}); err != nil {
			return err
		}
	} else if !opts.quiet {
		fmt.Fprintf(stdout, "knowledge: %s\n%s\n", view.Status, view.Reason)
		if view.Head != "" {
			fmt.Fprintf(stdout, "  pages: %d  fresh: %d  changed files: %d\n", view.Pages, view.FreshPages, view.ChangedFiles)
			fmt.Fprintf(stdout, "  baseline: %s  head: %s (%s)\n", shortSHA(view.Baseline), shortSHA(view.Head), view.Branch)
		}
		fmt.Fprintf(stdout, "  index: %s\n", indexSummary(view))
	}
	// The per-page verdicts are the actionable part of the report, so they stay
	// visible under --quiet, like the other knowledge commands' findings. The
	// JSON envelope already carries them, so they are text output only.
	if !opts.json {
		for _, page := range view.Affected {
			fmt.Fprintf(stdout, "  affected: %s\n", page)
		}
		for _, page := range view.Unverifiable {
			fmt.Fprintf(stdout, "  unverifiable: %s\n", page)
		}
	}
	// The report itself is the output: stale and missing are states, not
	// errors, and the exit code is how a script tells them apart.
	switch view.Status {
	case app.KnowledgeStale:
		return exitWithCode(CodeStale)
	case app.KnowledgeMissing:
		return exitWithCode(CodeMissing)
	default:
		return nil
	}
}

// runKnowledgeRefresh implements `workloom knowledge refresh [--affected|--full]`:
// regenerate the pages that no longer match the tree, through the configured
// generator (M5.3, 方案 §12.6).
func runKnowledgeRefresh(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("knowledge refresh", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	full := fs.Bool("full", false, "regenerate every page")
	affected := fs.Bool("affected", false, "regenerate only the pages a change touches (default)")
	force := fs.Bool("force", false, "regenerate pages that are protected or hand-edited")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 {
		return errUsage("knowledge refresh [--affected|--full] [--force]")
	}
	if *full && *affected {
		return errUsage("knowledge refresh takes --affected or --full, not both")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	view, err := svc.KnowledgeRefresh(context.Background(), app.KnowledgeRefreshRequest{Full: *full, Force: *force})
	if err != nil {
		// A failed run still has a report worth printing: the generator's
		// output is usually the only clue, and the exit code carries the rest.
		if reportErr := reportRefresh(stdout, opts, view, true); reportErr != nil {
			return reportErr
		}
		return err
	}
	if err := reportRefresh(stdout, opts, view, false); err != nil {
		return err
	}
	if view.Missing {
		// Nothing was regenerated and nothing could be: the degraded answer
		// carries the page layer's own exit code rather than a success.
		return exitWithCode(CodeMissing)
	}
	return nil
}

// reportRefresh renders one refresh report. failed says the envelope describes a
// run that ended badly, so a JSON consumer is not told "ok".
func reportRefresh(stdout io.Writer, opts options, view app.KnowledgeRefreshView, failed bool) error {
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK bool `json:"ok"`
			app.KnowledgeRefreshView
		}{OK: !failed, KnowledgeRefreshView: view})
	}
	if !opts.quiet {
		fmt.Fprintf(stdout, "knowledge refresh (%s): %d page(s)\n", view.Scope, len(view.Pages))
	}
	if view.Resumed {
		if view.Checkpoint != "" {
			fmt.Fprintf(stdout, "  resumed an interrupted run (%s)\n", view.Checkpoint)
		} else {
			fmt.Fprintln(stdout, "  resumed an interrupted run")
		}
	}
	if view.Reason != "" {
		fmt.Fprintf(stdout, "%s\n", view.Reason)
	}
	// What the layer kept out of the generator's reach, what the generator
	// could not finish by itself, and its own output stay visible under
	// --quiet: they are what a run is judged by.
	for _, skip := range view.Skipped {
		fmt.Fprintf(stdout, "  skipped: %s  (%s)\n", skip.Path, skip.Reason)
	}
	for _, page := range view.AwaitingGeneration {
		fmt.Fprintf(stdout, "  awaiting generation: %s\n", page)
	}
	if view.Ran && view.Output != "" {
		fmt.Fprint(stdout, view.Output)
		if !strings.HasSuffix(view.Output, "\n") {
			fmt.Fprintln(stdout)
		}
	}
	if view.Status != nil {
		fmt.Fprintf(stdout, "after refresh: %s\n", view.Status.Status)
		for _, page := range view.Status.Affected {
			fmt.Fprintf(stdout, "  still stale: %s\n", page)
		}
	}
	return nil
}

// shortSHA trims a commit for display, tolerating an empty baseline.
func shortSHA(commit string) string {
	if commit == "" {
		return "(none)"
	}
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

// indexSummary describes the file index in the status report.
func indexSummary(view app.KnowledgeStatusView) string {
	if !view.IndexReady {
		return "not built (run `workloom knowledge scan`)"
	}
	return fmt.Sprintf("%d files", view.IndexFiles)
}

// runPrime is `workloom prime`: an alias for `session start --compact` with the
// same optional identity flags. One call, minimal orientation, read-only.
func runPrime(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("prime", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	harness := fs.String("harness", "", "harness name")
	agentID := fs.String("agent", "", "agent identity")
	workspace := fs.String("workspace", "", "workspace path")
	intent := fs.String("intent", "", "what the agent intends to do")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 {
		return errUsage("prime [--harness codex] [--agent <id>] [--workspace <path>] [--intent <text>]")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	view, err := svc.SessionStart(context.Background(), app.SessionRequest{
		Harness: *harness, AgentID: *agentID, Workspace: *workspace, Intent: *intent, Compact: true,
	})
	if err != nil {
		return err
	}
	return outputSession(stdout, opts, view)
}

// runSession routes the session family: `start` is the one-shot orientation
// a new agent session runs first (方案 §8.3, 实施计划 M4.4).
func runSession(stdout io.Writer, opts options, rest []string) error {
	if familyUsage(stdout, rest, "`workloom session` needs a subcommand (start)") {
		return nil
	}
	if rest[0] != "start" {
		return errUsage("`workloom session` needs a subcommand (start)")
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
	return outputSession(stdout, opts, view)
}

// outputSession renders one SessionView for both `session start` and `prime`.
func outputSession(stdout io.Writer, opts options, view app.SessionView) error {
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
