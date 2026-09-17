// Command devsys is the CLI entry point for the project-local agent
// development infrastructure described in docs/原始文档/
// (方案 = specification, 实施计划 = step plan).
//
// Scope of step M0.1: argument dispatch, help text, and a deterministic exit
// code. No business logic lives here yet; M0.2 adds subcommands and the global
// --json / --quiet switches.
package main

import (
	"fmt"
	"os"

	"workloom/internal/version"
)

// Exit codes. The taxonomy is intentionally small now; M0.2 extends it and pins
// it with tests before any script depends on it.
const (
	exitOK    = 0
	exitUsage = 2
)

const usage = `devsys - project-local agent development infrastructure

usage:
  devsys --version    print build identity
  devsys --help       print this help

exit codes:
  0  success
  2  usage error
`

func main() {
	os.Exit(run(os.Args[1:]))
}

// run keeps dispatch out of main so the exit contract stays testable.
func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return exitUsage
	}

	switch args[0] {
	case "--version", "-v":
		fmt.Println("devsys " + version.String())
		return exitOK
	case "--help", "-h":
		fmt.Print(usage)
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "devsys: unknown argument %q\n\n%s", args[0], usage)
		return exitUsage
	}
}
