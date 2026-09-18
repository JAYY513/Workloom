package cli

import (
	"context"
	"flag"
	"io"
	"strings"

	"workloom/internal/mcp"
	"workloom/internal/version"
)

// mcpInstructions is the optional `instructions` field of the initialize
// result: a factual description of what this server is, never advice the
// server pushes into an agent's prompt. Clients decide whether to surface it
// at all; the discipline text for agents lives in AGENTS.md, which the
// operator publishes explicitly with `devsys wire` (M4.6).
const mcpInstructions = `devsys — 项目本地开发基础设施的 MCP 服务器。` +
	`项目状态保存在仓库内 .devsys/（唯一事实来源），本服务器的工具与 devsys CLI 读写同一份状态；` +
	`工具返回的来源与版本哈希标识所读内容，写入工具要求携带版本哈希。`

// runMCP routes the mcp command family (实施计划 M4.1): the stdio server.
// Business tool families attach to the same registry in M4.2/M4.3.
func runMCP(opts options, stdin io.Reader, stdout, stderr io.Writer, rest []string) error {
	if len(rest) == 0 {
		return errUsage("`devsys mcp` requires a subcommand (serve)")
	}
	switch rest[0] {
	case "serve":
		return runMCPServe(opts, stdin, stdout, stderr, rest[1:])
	default:
		return errUsage("unknown `devsys mcp` subcommand %q", rest[0])
	}
}

// runMCPServe runs the stdio MCP server until the client closes stdin.
// stdout carries protocol traffic only; diagnostics go to stderr.
func runMCPServe(opts options, stdin io.Reader, stdout, stderr io.Writer, rest []string) error {
	fs := flag.NewFlagSet("mcp serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profileList := fs.String("profile", strings.Join(mcp.DefaultProfiles(), ","),
		"comma-separated profile set (session, executor, reviewer, admin)")
	// Registered only so the refusal names both spellings; serve output is
	// protocol JSON regardless.
	jsonFlag := fs.Bool("json", false, "unsupported")
	quietFlag := fs.Bool("quiet", false, "unsupported")
	if err := fs.Parse(rest); err != nil {
		return errUsage("`devsys mcp serve`: %v", err)
	}
	if opts.json || opts.quiet || *jsonFlag || *quietFlag {
		return errUsage("`devsys mcp serve` does not take --json or --quiet")
	}
	if fs.NArg() > 0 {
		return errUsage("`devsys mcp serve` takes no arguments (got %q)", fs.Arg(0))
	}
	profiles, err := mcp.ParseProfiles(*profileList)
	if err != nil {
		return errUsage("`devsys mcp serve`: %v", err)
	}
	svc, err := requireProjectRoot()
	if err != nil {
		return err
	}
	cfg := mcp.Config{
		Root:          svc.Root,
		Profiles:      profiles,
		ServerVersion: version.String(),
		Instructions:  mcpInstructions,
		Log:           stderr,
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
