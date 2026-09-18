package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The layer never speaks a generator's own format, and a generator never needs
// to know .devsys/ exists: the adapter maps both directions (方案 §12.6).
//
// The contract the layer offers a generator is argv, not an API:
//
//	<generator> --project <root> --scope-file <json> --format-version 1
//
// and the rules are idempotency, respect for `protected` and hand edits,
// baseline maintenance, and resumability. An adapter is what lets a generator
// whose CLI predates that contract still take part.

// Availability is what a probe learned about one generator.
type Availability struct {
	Installed bool   `json:"installed"`
	Detail    string `json:"detail,omitempty"`
}

// GenerateRequest is one adapter run.
type GenerateRequest struct {
	// Root is the project root.
	Root string
	// ScopeFile is the scope this layer wrote (Scope), project-relative.
	ScopeFile string
	// Pages is the scope's page list, resolved for the adapter's own
	// bookkeeping.
	Pages []string
}

// GenerateResult reports what an adapter run did.
type GenerateResult struct {
	// Output is the combined output of the steps that ran.
	Output string `json:"output,omitempty"`
	// Finalized reports whether the generator recorded a new baseline.
	Finalized bool `json:"finalized,omitempty"`
	// AwaitingGeneration lists pages the generator cannot write by itself.
	// RepoWiki is agent-driven: its CLI scans and bookkeeps, while the pages are
	// written through its skill. Saying so is the honest outcome.
	AwaitingGeneration []string `json:"awaiting_generation,omitempty"`
	// Note explains the run in one line.
	Note string `json:"note,omitempty"`
}

// Adapter bridges one external page generator.
type Adapter interface {
	// Name is the identifier a project writes in `knowledge_generator`.
	Name() string
	// Probe reports whether the generator is installed.
	Probe(ctx context.Context) Availability
	// Import maps the generator's own state onto the layer's State. A generator
	// that has not run yet returns (nil, nil): there is nothing to import, and
	// that is not a failure.
	Import(root string) (*State, error)
	// Generate runs the generator's mechanical steps for one scope.
	Generate(ctx context.Context, req GenerateRequest) (GenerateResult, error)
}

// Adapters lists the built-in adapters.
func Adapters() []Adapter { return []Adapter{Repowiki()} }

// AdapterByName resolves a `knowledge_generator` value to a built-in adapter.
func AdapterByName(name string) (Adapter, bool) {
	for _, adapter := range Adapters() {
		if strings.EqualFold(adapter.Name(), name) {
			return adapter, true
		}
	}
	return nil, false
}

// MergeStates layers an imported mapping under the project's own state: the
// page layer's own bookkeeping wins where both know a page, and the imported
// one fills the gaps (the reference generator keeps `sources` in its state, so
// without this merge its pages cannot be attributed at all).
func MergeStates(local, imported *State) *State {
	switch {
	case local == nil:
		return imported
	case imported == nil:
		return local
	}
	merged := &State{
		SchemaVersion: local.SchemaVersion,
		Baseline:      local.Baseline,
		Pages:         map[string]PageState{},
		Generator:     local.Generator,
		UpdatedAt:     local.UpdatedAt,
	}
	// Gaps are filled field by field: a local state that knows the commit but
	// not the branch still has a branch to learn, and an entry that knows a
	// hash but no sources still has sources to learn.
	if merged.Baseline.Commit == "" {
		merged.Baseline.Commit = imported.Baseline.Commit
	}
	if merged.Baseline.Branch == "" {
		merged.Baseline.Branch = imported.Baseline.Branch
	}
	for path, entry := range imported.Pages {
		merged.Pages[path] = entry
	}
	for path, entry := range local.Pages {
		previous := merged.Pages[path]
		merged.Pages[path] = PageState{
			ContentHash:  firstNonEmpty(entry.ContentHash, previous.ContentHash),
			Sources:      firstNonEmptyList(entry.Sources, previous.Sources),
			SourceCommit: firstNonEmpty(entry.SourceCommit, previous.SourceCommit),
		}
	}
	if merged.Generator == "" {
		merged.Generator = imported.Generator
	}
	return merged
}

// firstNonEmpty returns the first non-empty value.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// firstNonEmptyList returns the first non-empty list.
func firstNonEmptyList(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

// --- RepoWiki -------------------------------------------------------------

// repowikiBundleRoot is where RepoWiki keeps its bundle, and
// repowikiStateFile is where it keeps its own state: the two are different
// places — the bundle is documentation under docs/, the state is tooling state
// at the repository root. RepoWiki decides its layout; the adapter's job is to
// know it, not to change it.
const (
	repowikiBundleRoot = "docs/repowiki"
	repowikiStateFile  = ".repowiki/state.json"
)

// repowikiCommand is the CLI the adapter drives.
const repowikiCommand = "repowiki"

// repowikiAdapter drives RepoWiki's CLI (方案 §10.2/§12.6): `init` and `scan`
// are mechanical, the pages themselves are written through RepoWiki's skill,
// and `state --update` is the finalize step.
type repowikiAdapter struct{}

// Repowiki returns the built-in RepoWiki adapter.
func Repowiki() Adapter { return repowikiAdapter{} }

func (repowikiAdapter) Name() string { return "repowiki" }

// ProbeTimeout bounds a probe: a generator that hangs on startup must not hang
// the status command that asked whether it is there.
const ProbeTimeout = 10 * time.Second

// Probe runs the CLI without arguments: RepoWiki has no --version, and its
// banner is the availability answer. Installed means usable — a binary that is
// on PATH but cannot run is reported as not installed, with the reason.
func (repowikiAdapter) Probe(ctx context.Context) Availability {
	path, err := exec.LookPath(repowikiCommand)
	if err != nil {
		return Availability{Installed: false, Detail: repowikiCommand + " is not on PATH"}
	}
	probeCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, path).CombinedOutput()
	if err != nil {
		return Availability{Installed: false, Detail: fmt.Sprintf("%s is at %s but does not run: %v", repowikiCommand, path, err)}
	}
	first := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	return Availability{Installed: true, Detail: strings.TrimSpace(first)}
}

// repowikiState is the reference generator's own state file.
type repowikiState struct {
	Git struct {
		Commit string `json:"commit"`
		Branch string `json:"branch"`
	} `json:"git"`
	Pages map[string]struct {
		Sources     []string `json:"sources"`
		ContentHash string   `json:"content_hash"`
	} `json:"pages"`
}

// Import maps RepoWiki's state file onto the layer's State. Its content hashes
// are whole-file sha256 — the same value this layer records — so nothing needs
// translating, only locating.
func (repowikiAdapter) Import(root string) (*State, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(repowikiStateFile)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", repowikiStateFile, err)
	}
	var external repowikiState
	if err := json.Unmarshal(data, &external); err != nil {
		return nil, fmt.Errorf("%s: %w", repowikiStateFile, err)
	}
	state := &State{
		SchemaVersion: StateSchemaVersion,
		Baseline:      Baseline{Commit: external.Git.Commit, Branch: external.Git.Branch},
		Pages:         map[string]PageState{},
		Generator:     "repowiki",
	}
	for page, entry := range external.Pages {
		if !strings.HasSuffix(page, ".md") {
			continue
		}
		state.Pages[repowikiBundleRoot+"/"+page] = PageState{
			Sources:     entry.Sources,
			ContentHash: entry.ContentHash,
		}
	}
	return state, nil
}

// Generate runs RepoWiki's mechanical steps, then finalizes only when the
// scoped pages already match their records. Finalizing earlier would record a
// new baseline over pages that still describe older code — the one thing the
// freshness mechanism exists to prevent.
func (a repowikiAdapter) Generate(ctx context.Context, req GenerateRequest) (GenerateResult, error) {
	result := GenerateResult{}
	// What the last finalize recorded is read before anything runs: RepoWiki's
	// scan rewrites its own state, and a reference taken afterwards would say
	// "every page is new" about pages that are not.
	recorded, err := a.Import(req.Root)
	if err != nil {
		return result, err
	}
	for _, args := range [][]string{{"init"}, {"scan"}} {
		out, err := runRepowiki(ctx, req.Root, args...)
		result.Output += out
		if err != nil {
			return result, fmt.Errorf("repowiki %s: %w", strings.Join(args, " "), err)
		}
	}
	pending, err := a.pendingPages(req, recorded)
	if err != nil {
		return result, err
	}
	if len(pending) > 0 {
		result.AwaitingGeneration = pending
		result.Note = fmt.Sprintf("%d page(s) still need writing: RepoWiki writes pages through its skill, so the CLI alone cannot finish them; the baseline stays put until they match their records", len(pending))
		return result, nil
	}
	out, err := runRepowiki(ctx, req.Root, "state", "--update")
	result.Output += out
	if err != nil {
		return result, fmt.Errorf("repowiki state --update: %w", err)
	}
	result.Finalized = true
	result.Note = "repowiki recorded the new baseline: every scoped page matches its record"
	return result, nil
}

// pendingPages lists the scoped pages whose content does not match what the
// generator recorded. recorded is the generator's state as it was before this
// run touched anything.
func (repowikiAdapter) pendingPages(req GenerateRequest, recorded *State) ([]string, error) {
	if len(req.Pages) == 0 {
		return nil, nil
	}
	imported := recorded
	var pending []string
	for _, page := range req.Pages {
		recorded := ""
		if entry, ok := imported.Mapping(page); ok {
			recorded = entry.ContentHash
		}
		if recorded == "" {
			pending = append(pending, page)
			continue
		}
		actual, err := HashFile(filepath.Join(req.Root, filepath.FromSlash(page)))
		if err != nil {
			pending = append(pending, page)
			continue
		}
		if actual != recorded {
			pending = append(pending, page)
		}
	}
	return pending, nil
}

// adapterOutputLimit caps what an adapter's output can cost the caller, the
// same way the command path does: a verbose generator must not bloat the
// report it ends up in.
const adapterOutputLimit = 256 << 10

// runRepowiki runs one RepoWiki command in the project root.
func runRepowiki(ctx context.Context, root string, args ...string) (string, error) {
	path, err := exec.LookPath(repowikiCommand)
	if err != nil {
		return "", fmt.Errorf("%s is not on PATH", repowikiCommand)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if len(out) > adapterOutputLimit {
		return string(out[:adapterOutputLimit]) + fmt.Sprintf("\n[输出超过 %d 字节，已截断]", adapterOutputLimit), err
	}
	return string(out), err
}
