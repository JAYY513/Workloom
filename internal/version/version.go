// Package version carries the build identity of the devsys CLI.
//
// Both variables are overridable at build time, so a released binary can state
// exactly which source revision it came from:
//
//	go build -ldflags "-X workloom/internal/version.Version=0.1.0 \
//	                   -X workloom/internal/version.Commit=$(git rev-parse --short HEAD)" \
//	         -o bin/devsys ./cmd/devsys
package version

// Version is the semantic version of this build. Local builds that do not pass
// -ldflags keep the development marker instead of pretending to be a release.
var Version = "0.1.0-dev"

// Commit is the source revision this binary was built from, when known.
var Commit = "unknown"

// String renders the single-line identity printed by `devsys --version`.
func String() string {
	if Commit == "" || Commit == "unknown" {
		return Version
	}
	return Version + " (" + Commit + ")"
}
