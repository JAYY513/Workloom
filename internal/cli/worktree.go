package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"workloom/internal/app"
	"workloom/internal/workspace"
)

// runWorktree routes the worktree family (方案 §4.8): the execution workspace
// of a work item, its lifecycle hooks and its invariants.
func runWorktree(stdout io.Writer, opts options, rest []string) error {
	if familyUsage(stdout, rest, "`devsys worktree` needs a subcommand (prepare | remove | list)") {
		return nil
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch rest[0] {
	case "prepare":
		fs := flag.NewFlagSet("worktree prepare", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		workitemID := fs.String("workitem", "", "work item id")
		runID := fs.String("run", "", "bind the workspace to this run")
		branch := fs.String("branch", "", "branch to check out (default devsys/<key>)")
		actor := fs.String("actor", "", "operator identity")
		reason := fs.String("reason", "", "why the workspace is needed")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 ||
			*workitemID == "" || *actor == "" || *reason == "" {
			return errUsage("worktree prepare --workitem <id> --actor <a> --reason <r> [--run <run-id>] [--branch <b>]")
		}
		view, err := svc.WorkspacePrepare(ctx, app.WorkspacePrepareRequest{
			WorkItemID: *workitemID, RunID: *runID, Branch: *branch,
			Actor: *actor, Reason: *reason,
		})
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK bool `json:"ok"`
				app.WorkspaceView
			}{OK: true, WorkspaceView: view})
		}
		if !opts.quiet {
			state := "reused"
			if view.Created {
				state = "created"
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", view.Key, view.Path, view.Branch, state)
			if view.Hook != nil && view.Hook.Ran {
				fmt.Fprintf(stdout, "after_create: exit=%d duration=%dms\n", view.Hook.ExitCode, view.Hook.DurationMS)
			}
			for _, warning := range view.Warnings {
				fmt.Fprintf(stdout, "warning: %s\n", warning)
			}
		}
		return nil
	case "remove":
		fs := flag.NewFlagSet("worktree remove", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		workitemID := fs.String("workitem", "", "work item id")
		path := fs.String("path", "", "workspace path")
		force := fs.Bool("force", false, "discard local changes")
		actor := fs.String("actor", "", "operator identity")
		reason := fs.String("reason", "", "why the workspace is removed")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 ||
			(*workitemID == "" && *path == "") || *actor == "" || *reason == "" {
			return errUsage("worktree remove --workitem <id> | --path <p> --actor <a> --reason <r> [--force]")
		}
		rep, err := svc.WorkspaceRemove(ctx, app.WorkspaceRemoveRequest{
			WorkItemID: *workitemID, Path: *path, Force: *force,
			Actor: *actor, Reason: *reason,
		})
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK bool `json:"ok"`
				workspace.RemoveReport
			}{OK: true, RemoveReport: rep})
		}
		if !opts.quiet {
			switch {
			case rep.Removed:
				fmt.Fprintf(stdout, "%s\tremoved\n", rep.Path)
			default:
				fmt.Fprintf(stdout, "%s\t%s\n", rep.Path, rep.Notice)
			}
			for _, warning := range rep.Warnings {
				fmt.Fprintf(stdout, "warning: %s\n", warning)
			}
		}
		return nil
	case "list":
		fs := flag.NewFlagSet("worktree list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		workitemID := fs.String("workitem", "", "only this work item's workspace")
		if err := fs.Parse(rest[1:]); err != nil || fs.NArg() != 0 {
			return errUsage("worktree list [--workitem <id>]")
		}
		items, err := svc.WorkspaceList(ctx, *workitemID)
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(struct {
				OK         bool                `json:"ok"`
				Workspaces []workspace.Listing `json:"workspaces"`
				Count      int                 `json:"count"`
			}{OK: true, Workspaces: items, Count: len(items)})
		}
		if !opts.quiet {
			if len(items) == 0 {
				fmt.Fprintln(stdout, "no workspaces")
				return nil
			}
			for _, item := range items {
				registered := ""
				if item.Registered {
					registered = "\t" + item.Branch
				}
				fmt.Fprintf(stdout, "%s\t%s%s\n", item.Key, item.Path, registered)
			}
		}
		return nil
	default:
		return errUsage("unknown `devsys worktree` subcommand %q", rest[0])
	}
}
