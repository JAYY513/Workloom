// Package mcp exposes the devsys application service over the Model Context
// Protocol using the official Go SDK (github.com/modelcontextprotocol/go-sdk).
//
// The SDK owns the protocol: framing, lifecycle, version negotiation,
// tools/list and tools/call shapes, and input validation against the schema
// inferred from each handler's typed input. This package owns what is ours:
// which tools a profile exposes, and the error taxonomy shared with the CLI
// (internal/app) so both surfaces refuse the same way.
package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/JAYY513/Workloom/internal/app"
)

// ServerName is the fixed identity every devsys MCP surface reports.
const ServerName = "devsys"

// Tool profiles (方案 §8.6). The server exposes the union of the selected
// profiles' tools; the default is session + executor, and reviewer/admin
// stay opt-in so dangerous entries are explicit.
const (
	ProfileSession  = "session"
	ProfileExecutor = "executor"
	ProfileReviewer = "reviewer"
	ProfileAdmin    = "admin"
)

// Tool tiers (#301): core is the daily subset, standard is everything the
// selected profiles expose. Tiers compose with profiles by conjunction.
const (
	TierCore     = "core"
	TierStandard = "standard"
)

// DefaultTier is the tier an unconfigured server exposes.
func DefaultTier() string { return TierCore }

// ParseTier parses the --tier flag. Unknown names are rejected so a typo
// cannot silently widen or narrow the tool surface.
func ParseTier(tier string) (string, error) {
	tier = strings.TrimSpace(tier)
	if tier == "" {
		return DefaultTier(), nil
	}
	switch tier {
	case TierCore, TierStandard:
		return tier, nil
	default:
		return "", fmt.Errorf("unknown tier %q (expected core or standard)", tier)
	}
}

// tierLevel orders tiers: a selected tier exposes its own level and below.
func tierLevel(tier string) int {
	if tier == TierCore {
		return 0
	}
	return 1
}

// DefaultProfiles is the profile set an unconfigured server exposes.
func DefaultProfiles() []string { return []string{ProfileSession, ProfileExecutor} }

// ParseProfiles parses a comma-separated profile list. Empty input selects
// the defaults; unknown names are rejected so a typo cannot silently widen
// or narrow the tool surface.
func ParseProfiles(list string) ([]string, error) {
	list = strings.TrimSpace(list)
	if list == "" {
		return DefaultProfiles(), nil
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(list, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			return nil, fmt.Errorf("profile list %q contains an empty name", list)
		}
		switch name {
		case ProfileSession, ProfileExecutor, ProfileReviewer, ProfileAdmin:
		default:
			return nil, fmt.Errorf("unknown profile %q (expected session, executor, reviewer or admin)", name)
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

// Config is the project binding a tool set serves.
type Config struct {
	// Root is the absolute project root; tools read and write only beneath it.
	Root string
	// Profiles is the active profile set.
	Profiles []string
	// Tier caps the exposed tools: core shows the daily subset, standard
	// shows everything the profiles expose. Empty means the default tier.
	Tier string
	// ServerVersion is the build identity reported to clients.
	ServerVersion string
	// Instructions is the discipline note clients receive at initialize.
	Instructions string
	// Log receives server diagnostics (stderr in the CLI).
	Log io.Writer
}

// service binds the shared application service for one tool invocation.
// Tools are thin adapters: every business rule (gates, quality, approvals,
// version guards, transactions) stays in internal/app so the CLI and MCP
// cannot diverge.
func (cfg Config) service() *app.Service { return app.New(cfg.Root) }

// NewServer builds the SDK server with exactly the tools the selected
// profiles expose. Registration is the profile filter: a tool that is not
// exposed is never registered, so it cannot be called by name either.
func NewServer(cfg Config) *mcpsdk.Server {
	opts := &mcpsdk.ServerOptions{Instructions: cfg.Instructions}
	if cfg.Log != nil {
		opts.Logger = slog.New(slog.NewTextHandler(cfg.Log, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: ServerName, Version: cfg.ServerVersion}, opts)
	tier := cfg.Tier
	if tier == "" {
		tier = DefaultTier()
	}
	for _, spec := range allTools() {
		if !visible(spec.profiles, cfg.Profiles) || !visibleTier(spec.tier, tier) {
			continue
		}
		spec.register(server, cfg)
	}
	return server
}

// VisibleTools lists the tool names NewServer would register for cfg — the
// same profile∧tier conjunction — so the CLI can refuse to start a server
// whose filter leaves nothing to serve instead of spinning a silent no-op.
func VisibleTools(cfg Config) []string {
	tier := cfg.Tier
	if tier == "" {
		tier = DefaultTier()
	}
	var names []string
	for _, spec := range allTools() {
		if visible(spec.profiles, cfg.Profiles) && visibleTier(spec.tier, tier) {
			names = append(names, spec.name)
		}
	}
	return names
}

// Run serves the protocol over the given streams until the client closes
// them. Nil streams mean the process standard streams (stdio transport).
func Run(ctx context.Context, cfg Config, in io.ReadCloser, out io.WriteCloser) error {
	server := NewServer(cfg)
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	return server.Run(ctx, &mcpsdk.IOTransport{Reader: in, Writer: out})
}

// visible reports whether a tool is exposed under any selected profile. An
// empty profile list is visible in no profile: a tool that forgets to
// declare its profiles must not leak into admin surfaces by default.
func visible(toolProfiles, selected []string) bool {
	if len(toolProfiles) == 0 {
		return false
	}
	for _, want := range toolProfiles {
		for _, have := range selected {
			if want == have {
				return true
			}
		}
	}
	return false
}

// visibleTier reports whether a tool tier is exposed under the selected tier:
// core tools show at every tier, standard tools only at standard and above.
// An empty tool tier means standard (opt in to core explicitly).
func visibleTier(toolTier, selected string) bool {
	return tierLevel(toolTier) <= tierLevel(selected)
}
