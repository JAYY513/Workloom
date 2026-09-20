// Package workloom exists to embed the example workflow policies into the
// binary, so `devsys workflow init --template` works for users who only
// have the release binary — the docs/ tree ships with the repository, not
// with the binary. docs/examples/workflows/ stays the single source: the
// smoke scripts and the README copy from the same files.
package workloom

import "embed"

// WorkflowTemplatesFS holds the example workflow policies (quick-fix,
// feature-development, architecture-change, reference-template) exactly as
// they appear in docs/examples/workflows/.
//
//go:embed docs/examples/workflows/*.md
var WorkflowTemplatesFS embed.FS
