package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/JAYY513/Workloom/internal/app"
	"github.com/JAYY513/Workloom/internal/mcp"
	"github.com/JAYY513/Workloom/internal/version"
)

// mcpInstructions is the optional `instructions` field of the initialize
// result: a factual description of what this server is, never advice the
// server pushes into an agent's prompt. Clients decide whether to surface it
// at all; the discipline text for agents lives in AGENTS.md, which the
// operator publishes explicitly with `workloom wire` (M4.6).
const mcpInstructions = `workloom — 项目本地开发基础设施的 MCP 服务器。` +
	`项目状态保存在仓库内 .devsys/（唯一事实来源），本服务器的工具与 workloom CLI 读写同一份状态；` +
	`工具返回的来源与版本哈希标识所读内容，写入工具要求携带版本哈希。`

// runMCP routes the mcp command family (实施计划 M4.1): the stdio server
// plus the client-registration installer.
func runMCP(opts options, stdin io.Reader, stdout, stderr io.Writer, rest []string) error {
	if familyUsage(stdout, rest, "`workloom mcp` requires a subcommand (serve | install [--scope user|project] [--client codex|claude|opencode] [--dry-run])") {
		return nil
	}
	switch rest[0] {
	case "serve":
		return runMCPServe(opts, stdin, stdout, stderr, rest[1:])
	case "install":
		return runMCPInstall(stdout, opts, rest[1:])
	default:
		return errUsage("unknown `workloom mcp` subcommand %q", rest[0])
	}
}

// stringSlice collects repeated --client flags.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// runMCPInstall implements `workloom mcp install`: the productized counterpart
// of `wire --print-mcp`'s copy-paste snippets. Per-client failures are
// reported, not fatal: one unreadable client config must not stop the
// others.
func runMCPInstall(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("mcp install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	scope := fs.String("scope", "user", "user or project")
	apply := fs.Bool("apply", false, "write the registration (default is a dry-run)")
	force := fs.Bool("force", false, "replace an existing devsys entry")
	dryRun := fs.Bool("dry-run", false, "report without writing (the default)")
	var clients stringSlice
	fs.Var(&clients, "client", "restrict to one client (repeatable)")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 {
		return errUsage("`workloom mcp install` [--scope user|project] [--client codex|claude|opencode]... [--apply] [--force] [--dry-run]")
	}
	if *apply && *dryRun {
		return errUsage("`--apply` and `--dry-run` are mutually exclusive")
	}
	svc, err := appService()
	if err != nil {
		return err
	}
	view, err := svc.MCPInstall(context.Background(), app.MCPInstallRequest{
		Scope:   *scope,
		Clients: clients,
		Apply:   *apply,
		Force:   *force,
		DryRun:  *dryRun,
		Announce: func(client, path string) {
			if opts.json || opts.quiet {
				return
			}
			fmt.Fprintf(stdout, "target %s: %s\n", client, path)
		},
	})
	if opts.json {
		payload := struct {
			OK bool `json:"ok"`
			*app.MCPInstallView
			Error *setupError `json:"error,omitempty"`
		}{OK: err == nil, MCPInstallView: view}
		if err != nil {
			ce := toCoded(err)
			payload.Error = &setupError{Code: ce.code, Kind: ce.kind, Message: ce.msg}
			if encErr := json.NewEncoder(stdout).Encode(payload); encErr != nil {
				return encErr
			}
			return exitWithCode(ce.code)
		}
		if encErr := json.NewEncoder(stdout).Encode(payload); encErr != nil {
			return encErr
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !opts.quiet {
		fmt.Fprintf(stdout, "scope: %s\n", view.Scope)
		for _, r := range view.Results {
			fmt.Fprintf(stdout, "[%s] %s: %s (%s)\n", mcpMark(r.Status), r.Client, r.Detail, r.Path)
		}
	}
	return nil
}

// mcpMark maps a result status onto the tree marks the other reports use.
func mcpMark(status string) string {
	switch status {
	case "installed", "planned":
		return "v"
	case "skipped":
		return "="
	case "not-detected":
		return "?"
	default:
		return "x"
	}
}

// runMCPServe runs the stdio MCP server until the client closes stdin.
// stdout carries protocol traffic only; diagnostics go to stderr.
func runMCPServe(opts options, stdin io.Reader, stdout, stderr io.Writer, rest []string) error {
	fs := flag.NewFlagSet("mcp serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profileList := fs.String("profile", strings.Join(mcp.DefaultProfiles(), ","),
		"comma-separated profile set (session, executor, reviewer, admin)")
	tierName := fs.String("tier", mcp.DefaultTier(),
		"tool tier ceiling (core or standard; core is the daily subset)")
	// Registered only so the refusal names both spellings; serve output is
	// protocol JSON regardless.
	jsonFlag := fs.Bool("json", false, "unsupported")
	quietFlag := fs.Bool("quiet", false, "unsupported")
	if err := fs.Parse(rest); err != nil {
		return errUsage("`workloom mcp serve`: %v", err)
	}
	if opts.json || opts.quiet || *jsonFlag || *quietFlag {
		return errUsage("`workloom mcp serve` does not take --json or --quiet")
	}
	if fs.NArg() > 0 {
		return errUsage("`workloom mcp serve` takes no arguments (got %q)", fs.Arg(0))
	}
	profiles, err := mcp.ParseProfiles(*profileList)
	if err != nil {
		return errUsage("`workloom mcp serve`: %v", err)
	}
	tier, err := mcp.ParseTier(*tierName)
	if err != nil {
		return errUsage("`workloom mcp serve`: %v", err)
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	cfg := mcp.Config{
		Root:          svc.Root,
		Profiles:      profiles,
		Tier:          tier,
		ServerVersion: version.String(),
		Instructions:  mcpInstructions,
		Log:           stderr,
	}
	if names := mcp.VisibleTools(cfg); len(names) == 0 {
		// Serving zero tools is a silent no-op server: the usual cause is a
		// read-only profile paired with the core tier, so name the way out.
		word, verb := "profile", "has"
		if len(profiles) > 1 {
			word, verb = "profiles", "have"
		}
		quoted := make([]string, 0, len(profiles))
		for _, p := range profiles {
			quoted = append(quoted, fmt.Sprintf("%q", p))
		}
		return errUsage("mcp serve: %s %s %s no tools in tier %s; use --tier standard",
			word, strings.Join(quoted, ", "), verb, tier)
	}
	if err := mcp.Run(context.Background(), cfg, nopReadCloser{stdin}, nopWriteCloser{stdout}); err != nil {
		return errInternal("mcp serve: %v", err)
	}
	return nil
}

// nopReadCloser / nopWriteCloser adapt the injected streams to the SDK's
// transport, which closes what it is given. The process streams must not be
// closed by the server, hence the no-op Close.
type nopReadCloser struct{ io.Reader }

func (nopReadCloser) Close() error { return nil }

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
