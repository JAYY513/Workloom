package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"workloom/internal/archive"
)

// runArchive routes the archive family (M8.3, 方案 §14.2): conservative,
// operator-driven moves of the append-only JSONL streams into
// .devsys/archive/. Reads merge live and archived state automatically;
// there is no delete/purge shape (默认保守：不删，只归档).
func runArchive(stdout io.Writer, opts options, rest []string) error {
	if len(rest) == 0 {
		return errUsage("`devsys archive` needs a subcommand: events | runs")
	}
	switch rest[0] {
	case "events":
		return runArchiveEvents(stdout, opts, rest[1:])
	case "runs":
		return runArchiveRuns(stdout, opts, rest[1:])
	default:
		return errUsage("`devsys archive` needs a subcommand: events | runs")
	}
}

func archiveRoot() (string, error) {
	return resolveRoot()
}

func runArchiveEvents(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("archive events", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	before := fs.String("before", "", "archive whole month shards older than YYYY-MM")
	dryRun := fs.Bool("dry-run", false, "list what would move without writing")
	actor := fs.String("actor", "", "operator")
	reason := fs.String("reason", "", "why the archive runs")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *before == "" || *actor == "" || *reason == "" {
		return errUsage("archive events --before <YYYY-MM> [--dry-run] --actor <a> --reason <r>")
	}
	root, err := archiveRoot()
	if err != nil {
		return err
	}
	rep, err := archive.Apply(context.Background(), root, archive.Spec{
		BeforeMonth: *before, Actor: *actor, Reason: *reason, DryRun: *dryRun,
	})
	if err != nil {
		return errInternal("archive events: %v", err)
	}
	return renderArchive(stdout, opts, rep)
}

func runArchiveRuns(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("archive runs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ids := fs.String("id", "", "comma-separated run ids")
	dryRun := fs.Bool("dry-run", false, "list what would move without writing")
	actor := fs.String("actor", "", "operator")
	reason := fs.String("reason", "", "why the archive runs")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 || *ids == "" || *actor == "" || *reason == "" {
		return errUsage("archive runs --id <run-id,...> [--dry-run] --actor <a> --reason <r>")
	}
	root, err := archiveRoot()
	if err != nil {
		return err
	}
	rep, err := archive.Apply(context.Background(), root, archive.Spec{
		RunIDs: strings.Split(*ids, ","), Actor: *actor, Reason: *reason, DryRun: *dryRun,
	})
	if err != nil {
		return errInternal("archive runs: %v", err)
	}
	return renderArchive(stdout, opts, rep)
}

func renderArchive(stdout io.Writer, opts options, rep archive.Report) error {
	if opts.json {
		return json.NewEncoder(stdout).Encode(struct {
			OK      bool           `json:"ok"`
			Archive archive.Report `json:"archive"`
		}{true, rep})
	}
	if opts.quiet {
		return nil
	}
	if rep.DryRun {
		fmt.Fprintln(stdout, "dry-run: nothing was moved")
	}
	for _, a := range rep.Archived {
		fmt.Fprintf(stdout, "archived %s\n", a)
	}
	for _, s := range rep.Skipped {
		fmt.Fprintf(stdout, "skipped  %s\n", s)
	}
	if rep.Note != "" {
		fmt.Fprintf(stdout, "note: %s\n", rep.Note)
	}
	fmt.Fprintf(stdout, "live bytes: %d -> %d (archived %d)\n", rep.BytesBefore, rep.BytesAfter, rep.BytesArchived)
	return nil
}
