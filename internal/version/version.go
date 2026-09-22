// Package version carries the build identity of the workloom CLI.
//
// The identity comes from, in priority order:
//
//  1. -ldflags -X overrides (release pipeline), then
//
//  2. the module version stamped by the Go toolchain: the release tag for
//     `go install …@vX.Y.Z`, or a pseudo-version (v0.1.8-0.20260921…-abc)
//     for a plain repository `go build` — Go fills
//     debug.ReadBuildInfo().Main.Version whenever VCS metadata exists, then
//
//  3. the development marker, for source exports without VCS metadata (where
//     Main.Version is "(devel)").
//
// The commit short hash (from VCS stamping) is appended whenever known.
//
//	go build -ldflags "-X github.com/JAYY513/Workloom/internal/version.Version=0.1.0 \
//	                   -X github.com/JAYY513/Workloom/internal/version.Commit=$(git rev-parse --short HEAD)" \
//	         -o bin/workloom ./cmd/workloom
package version

import (
	"runtime/debug"
	"strings"
)

// devMarker is what local builds without -ldflags answer, instead of
// pretending to be a release.
const devMarker = "0.1.0-dev"

// Version is the semantic version of this build, when injected at build time.
var Version = devMarker

// Commit is the source revision this binary was built from, when known.
var Commit = "unknown"

func init() {
	// Source builds without -ldflags (README recipe, install.sh fallback)
	// still know their revision via the module's VCS stamping, so
	// `workloom --version` does not answer "unknown".
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

// String renders the single-line identity printed by `workloom --version`.
func String() string {
	version := effectiveVersion()
	if Commit == "" || Commit == "unknown" {
		return version
	}
	return version + " (" + Commit + ")"
}

// effectiveVersion resolves the dev marker to the module version stamped by
// the Go toolchain. `go install …@vX.Y.Z` yields the release tag; a plain
// repository `go build` yields a pseudo-version (v0.1.8-0.2026…-abc) because
// Go fills Main.Version whenever VCS metadata is present. Only a source
// export without VCS metadata reports "(devel)" there — not a version — and
// keeps the marker.
func effectiveVersion() string {
	if Version != devMarker {
		return Version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return Version
	}
	return resolveVersion(Version, bi.Main.Version)
}

// resolveVersion is the pure decision behind effectiveVersion, kept
// side-effect free for testing.
func resolveVersion(binaryVersion, mainVersion string) string {
	if binaryVersion != devMarker {
		return binaryVersion
	}
	if strings.HasPrefix(mainVersion, "v") {
		return mainVersion
	}
	return binaryVersion
}
