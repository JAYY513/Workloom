// Command devsys is the CLI entry point for the project-local agent
// development infrastructure described in docs/原始文档/
// (方案 = specification, 实施计划 = step plan).
package main

import (
	"os"

	"workloom/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
