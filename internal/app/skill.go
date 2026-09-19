package app

import (
	"os"
	"path/filepath"
	"strings"
)

// Skill file layout (#300): the agent-facing workflow that AGENTS.md points
// at. Paths are relative to the project root.
const (
	skillDir  = ".agents/skills/devsys"
	skillMain = ".agents/skills/devsys/SKILL.md"
	skillCLI  = ".agents/skills/devsys/references/cli.md"
	skillFix  = ".agents/skills/devsys/references/troubleshooting.md"
)

// skillMarker identifies files written by WriteSkill; hand-written skill
// files without the marker are never overwritten.
const skillMarker = "<!-- devsys-skill -->"

const skillMainBody = skillMarker + `
# Devsys Skill

This project uses Devsys for tracked work. Prefer Devsys MCP tools when
available; otherwise use ` + "`devsys --json`" + ` through the shell.

## Start

1. Run ` + "`devsys session start`" + ` (one call: project facts, work in flight, recommended action).
2. Read the recommended work item (` + "`workitem get`" + ` / ` + "`context get --task <id>`" + `).

## Claim

Claim before tracked implementation (` + "`workitem claim --expect <version>`" + `);
reads after a write must re-read (expired hashes are refused, never forced).

## During work

Record significant findings, decisions and blockers
(` + "`finding/event/decision`" + `); keep the run evidence current (` + "`run update`" + `).

## Complete

1. Verify the implementation (tests or equivalent checks).
2. Complete the run (` + "`run complete`" + `; refused without branch evidence unless reviewed).
3. Transition the work item (` + "`workitem transition --expect <version>`" + `).

## Boundaries

- Never edit ` + "`.devsys/`" + ` files directly (repair via ` + "`devsys repair`" + `).
- See ` + "`references/cli.md`" + ` for the command table and ` + "`references/troubleshooting.md`" + ` for exit codes and retries.
`

const skillCLIRef = skillMarker + `
# Devsys CLI reference (read-only unless noted)

Source of truth: ` + "`devsys --help`" + ` and per-command usage. This file lists
the daily subset only.

` + "```sh" + `
devsys session start [--compact]            # orient: facts + recommended action
devsys next                                 # readiness verdict (always exit 0)
devsys project status                       # counts + risks + next
devsys workitem list [--jsonl]              # one JSON record per line
devsys workitem get <id>                    # includes version: <64-hex>
devsys workitem create --title T --actor A --reason R
devsys workitem transition --id <id> --to <status> --actor A --reason R --expect <hash>
devsys workitem claim --id <id> --owner O --reason R [--expect <hash>]
devsys workitem release/start/block/complete --id <id> --actor A --reason R [--expect <hash>]
devsys decision/finding/event/artifact list|get|create ...
devsys run list|get|log|create|update|heartbeat|verify|complete|fail|cancel
devsys context get [--task <id>] [--limit N]   # read-only aggregation
devsys knowledge status                     # 0 fresh / 10 stale / 11 missing
devsys workspace view                       # read-only summary
devsys doctor                               # read-only reconcile report
devsys recover --actor A --reason R         # operator recovery (idempotent)
devsys sync status                          # handoff readiness (exit 0)
` + "```" + `
`

const skillFixRef = skillMarker + `
# Devsys troubleshooting

Exit codes: 0 success / 1 internal / 2 usage / 3 precondition / 4 untrusted
managed state / 10 knowledge stale / 11 knowledge missing.

- ` + "`version mismatch … rerun workitem get`" + ` (exit 4): re-read the item
  and retry with the fresh hash. Never invent a hash.
- ` + "`no .devsys/ … run devsys init first`" + ` (exit 3): wrong directory or
  uninitialized project; find the project root first.
- Illegal transition (exit 4): stderr lists the allowed next states.
- ` + "`repair --dry-run`" + ` prints a digest; ` + "`--apply --confirm <digest>`" + `
  revalidates before writing (the only path that may lower completeness).
- Windows Git Bash: avoid ` + "`$(…)`" + ` capture of JSON for tokens; read
  ` + "`grep '^token:' .devsys/scheduling/<id>.yaml`" + ` instead.
`

// skillFile is one file WriteSkill manages.
type skillFile struct {
	rel  string
	body string
}

// skillFiles lists every file WriteSkill owns.
func skillFiles() []skillFile {
	return []skillFile{
		{rel: skillMain, body: skillMainBody},
		{rel: skillCLI, body: skillCLIRef},
		{rel: skillFix, body: skillFixRef},
	}
}

// SkillStatus reports whether the skill files exist and carry our marker.
func (s *Service) SkillStatus() (files int, marked int) {
	for _, f := range skillFiles() {
		data, err := os.ReadFile(filepath.Join(s.Root, filepath.FromSlash(f.rel)))
		if err != nil {
			continue
		}
		files++
		if strings.Contains(string(data), skillMarker) {
			marked++
		}
	}
	return files, marked
}

// WriteSkill writes the skill files idempotently: existing files carrying our
// marker are refreshed to the current body, files without the marker are left
// untouched (hand-written skill wins), missing files are created. It returns
// the paths it created or refreshed.
func (s *Service) WriteSkill() ([]string, error) {
	var changed []string
	for _, f := range skillFiles() {
		p := filepath.Join(s.Root, filepath.FromSlash(f.rel))
		existing, err := os.ReadFile(p)
		switch {
		case err == nil:
			if string(existing) == f.body {
				continue
			}
			if !strings.Contains(string(existing), skillMarker) {
				continue
			}
		case os.IsNotExist(err):
		default:
			return nil, Internalf("read %s: %v", f.rel, err)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, Internalf("create %s: %v", filepath.Dir(f.rel), err)
		}
		if err := os.WriteFile(p, []byte(f.body), 0o644); err != nil {
			return nil, Internalf("write %s: %v", f.rel, err)
		}
		changed = append(changed, f.rel)
	}
	return changed, nil
}

// SkillMainBody exposes the managed SKILL.md body for tests.
func SkillMainBody() string { return skillMainBody }
