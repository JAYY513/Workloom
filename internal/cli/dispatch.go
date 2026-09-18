package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"time"

	"workloom/internal/app"
)

// runDispatch implements `devsys dispatch`: one scheduling tick (方案 §4.8).
// The tick is a command, not a service — `--watch` is a foreground
// convenience loop, and read-only commands never dispatch.
func runDispatch(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("dispatch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	once := fs.Bool("once", false, "run a single tick (the default)")
	watch := fs.Bool("watch", false, "keep ticking in the foreground until interrupted")
	interval := fs.Duration("interval", 30*time.Second, "delay between ticks under --watch")
	dryRun := fs.Bool("dry-run", false, "plan and report without recovering, claiming or starting anything")
	max := fs.Int("max", 0, "start at most this many attempts in one tick (0 = the caps decide)")
	actor := fs.String("actor", "", "operator identity")
	reason := fs.String("reason", "", "why the tick runs")
	// dispatch has no subcommand: the flags start at rest[0].
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *actor == "" || *reason == "" {
		return errUsage("dispatch [--once|--watch] [--interval 30s] [--dry-run] [--max N] --actor <a> --reason <r>")
	}
	if *once && *watch {
		return errUsage("dispatch takes either --once or --watch, not both")
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	for {
		report, err := svc.Dispatch(ctx, app.DispatchRequest{
			Actor: *actor, Reason: *reason, Max: *max, DryRun: *dryRun,
		})
		// The report is rendered even when the tick refused every candidate:
		// the operator needs the notices to know what to fix.
		if renderErr := renderDispatch(stdout, opts, report); renderErr != nil {
			return renderErr
		}
		if err != nil {
			return err
		}
		if !*watch {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(*interval):
		}
	}
}

func renderDispatch(stdout io.Writer, opts options, report app.DispatchReport) error {
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK bool `json:"ok"`
			app.DispatchReport
		}{OK: true, DispatchReport: report})
	}
	if opts.quiet {
		return nil
	}
	if report.DryRun {
		fmt.Fprintln(stdout, "dry-run: nothing was recovered, claimed or started")
	}
	if report.Recover != nil {
		fmt.Fprintf(stdout, "recovered: %d applied, %d released expired, %d released orphaned\n",
			report.Recover.TransactionRecovery.Replayed,
			len(report.Recover.ReleasedExpired), len(report.Recover.ReleasedOrphans))
	}
	fmt.Fprintf(stdout, "in flight: %d (global cap %d)\n", report.Plan.InFlight, report.Plan.Caps.Global)
	for _, attempt := range report.Started {
		pid := ""
		if attempt.PID > 0 {
			pid = fmt.Sprintf(" pid=%d", attempt.PID)
		}
		fmt.Fprintf(stdout, "started %s\t%s%s\tlog=%s\n", attempt.WorkitemID, attempt.RunID, pid, attempt.Log)
	}
	for _, decision := range report.Plan.Start {
		started := false
		for _, attempt := range report.Started {
			if attempt.WorkitemID == decision.WorkitemID {
				started = true
			}
		}
		if !started {
			fmt.Fprintf(stdout, "planned %s\t(not started)\n", decision.WorkitemID)
		}
	}
	reasons := map[string][]string{}
	for _, decision := range report.Plan.Skip {
		reasons[decision.Reason] = append(reasons[decision.Reason], decision.WorkitemID)
	}
	kinds := make([]string, 0, len(reasons))
	for reason := range reasons {
		kinds = append(kinds, reason)
	}
	sort.Strings(kinds)
	for _, reason := range kinds {
		ids := reasons[reason]
		sort.Strings(ids)
		fmt.Fprintf(stdout, "skipped (%s): %d\t%s\n", reason, len(ids), joinLimited(ids, 6))
	}
	for _, notice := range report.Notices {
		fmt.Fprintf(stdout, "notice: %s\n", notice)
	}
	return nil
}

// joinLimited renders a list for one line without letting a long queue push
// everything else off the screen.
func joinLimited(items []string, limit int) string {
	if len(items) <= limit {
		return fmt.Sprint(items)
	}
	return fmt.Sprintf("%v… (+%d)", items[:limit], len(items)-limit)
}
