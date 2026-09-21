// Package version carries the build identity of the devsys CLI.
//
// Both variables are overridable at build time, so a released binary can state
// exactly which source revision it came from:
//
//	go build -ldflags "-X workloom/internal/version.Version=0.1.0 \
//	                   -X workloom/internal/version.Commit=$(git rev-parse --short HEAD)" \
//	         -o bin/devsys ./cmd/devsys
package version

import "runtime/debug"

// Version is the semantic version of this build. Local builds that do not pass
// -ldflags keep the development marker instead of pretending to be a release.
var Version = "0.1.0-dev"

// Commit is the source revision this binary was built from, when known.
var Commit = "unknown"

func init() {
	// Source builds without -ldflags (README recipe, install.sh fallback)
	// still know their revision via the module's VCS stamping, so
	// `devsys --version` does not answer "unknown".
	if Commit != "" && Commit != "unknown" {
		return
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			Commit = s.Value[:7]
			return
		}
	}
}

// String renders the single-line identity printed by `devsys --version`.
func String() string {
	if Commit == "" || Commit == "unknown" {
		return Version
	}
	return Version + " (" + Commit + ")"
}
