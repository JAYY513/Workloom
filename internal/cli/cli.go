// Package cli implements the devsys command surface: global switches, command
// dispatch, output rendering and the exit-code taxonomy. Keeping dispatch out
// of main() makes the exit contract testable.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"workloom/internal/project"
	"workloom/internal/registry"
	"workloom/internal/version"
)

// Exit codes, pinned by tests so scripts may rely on them. The knowledge
// layer adds its own 0/10/11 convention later (方案 §12.5, 实施计划 M4.5).
const (
	CodeOK           = 0
	CodeInternal     = 1
	CodeUsage        = 2
	CodePrecondition = 3
)

const usage = `devsys - project-local agent development infrastructure

usage:
  devsys [--json] [--quiet] <command>

commands:
  init        create .devsys/ in the current git repository root

options:
  --json      machine-readable output
  --quiet     suppress the human-readable success output
  --version   print build identity
  --help      print this help

exit codes:
  0  success
  1  internal error
  2  usage error
  3  precondition error (not a git repository, wrong directory, permissions)
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
				Code    int    `json:"code"`
				Kind    string `json:"kind"`
				Message string `json:"message"`
			} `json:"error"`
		}{}
		payload.Error.Code = ce.code
		payload.Error.Kind = ce.kind
		payload.Error.Message = ce.msg
		_ = json.NewEncoder(stderr).Encode(payload)
	} else {
		fmt.Fprintf(stderr, "devsys: %s\n", ce.msg)
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
