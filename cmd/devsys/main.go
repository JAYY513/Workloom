// Command devsys is the compatibility alias for the workloom CLI: the same
// entry point under the historical name, kept so existing scripts and
// muscle memory keep working. The primary binary is ./cmd/workloom.
package main

import (
	"os"

	"github.com/JAYY513/Workloom/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
