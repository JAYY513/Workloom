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

const skillMainBody = `---
name: devsys
description: Devsys/Workloom 项目状态与工作追踪纪律（.devsys/ 是唯一事实来源）。Use when working in a Devsys-tracked project — 触发词：devsys、Workloom、项目状态、领取任务、workitem、run、决策/发现记录、恢复上下文。
---
` + skillMarker + `
# Devsys Skill

This project uses Devsys for tracked work. Prefer Devsys MCP tools when
available; otherwise use ` + "`workloom --json`" + ` through the shell.

## Start

1. Run workloom prime (or workloom session start) at the start of a new session — one call: project facts, work in flight, recommended action.
2. For a new project, handle “no blueprint declared” before “no work items”: ask for confirmed goals, create and bind the blueprint, then create the first work item.
3. A newly created work item starts as draft; read it, transition it to ready, then claim it. Never claim draft directly.
4. Read the recommended work item with workitem get or context get --task <id>.
5. If the item carries a workflow, read its steps with workloom workflow get --id <workitem>.

## Claim

Claim before tracked implementation (` + "`workitem claim --expect <version>`" + `);
reads after a write must re-read (expired hashes are refused, never forced).

## During work

- Record significant findings, decisions and blockers
  (` + "`finding`" + ` / ` + "`decision`" + ` / ` + "`event`" + `); keep the run evidence current (` + "`run update`" + `).
- Advance a workflow with ` + "`workloom workflow step-complete`" + ` when the policy declares steps.
- Blocked: ` + "`workloom workitem block`" + `; a gated stage needs ` + "`workloom approval request`" + ` and a human decision.

## Complete

1. Verify the implementation (tests or equivalent checks).
2. Complete the run (` + "`run complete`" + `; refused without branch evidence unless reviewed).
3. Transition the work item (` + "`workitem transition --expect <version>`" + `).

## Boundaries

- Never edit ` + "`.devsys/`" + ` files directly (repair via ` + "`workloom repair`" + `).
- See ` + "`references/cli.md`" + ` for the command table and ` + "`references/troubleshooting.md`" + ` for exit codes and retries.
- Operator-side families (dispatch, approval, archive, workspace) are listed in ` + "`workloom --help`" + `.
- Default MCP ` + "`--tier core`" + ` (20 tools) does not expose ` + "`run_update`" + ` / ` + "`run_fail`" + ` / ` + "`run_cancel`" + ` / ` + "`workitem_block`" + `. Use the CLI, or serve ` + "`--tier standard`" + `.
`

const skillCLIRef = skillMarker + `
# Devsys CLI reference (daily subset)

Source of truth: ` + "`workloom --help`" + ` and per-command usage. A line marked
` + "`[w]`" + ` contains write subcommands (writes carry ` + "`--actor`" + ` / ` + "`--reason`" + `, and most
carry a version guard: ` + "`--expect <hash>`" + ` or ` + "`--latest`" + `).

` + "```sh" + `
workloom init                                 # [w] create .devsys/ in the current directory
workloom setup                                # [w] one-command onboarding (init + starter workflow + wire + checks); idempotent, existing files win
workloom wire [--dry-run]                     # [w] inject the AGENTS.md discipline block
workloom wire --skill | --check | --print-mcp <codex|claude|opencode>
workloom mcp install [--scope user|project] [--client codex|claude|opencode] [--apply] [--force] [--dry-run]  # [w] register devsys; default dry-run, --apply writes, --force replaces an existing entry
workloom prime                                # orient: facts + recommended action (alias: session start --compact)
workloom session start [--compact]            # same, full context payload
workloom next                                 # readiness verdict (always exit 0); judges ready items with claim's quality gate
workloom project status                       # counts + risks + next
workloom project blueprint                    # exit 0 when no blueprint is declared
workloom workitem list [--jsonl]              # one JSON record per line
workloom workitem get <id>                    # includes version: <64-hex>
workloom workitem create --title T --actor A --reason R [--description D --acceptance a,b]  # [w] set acceptance criteria up front
workloom workitem update --id <id> --acceptance a,b                     # [w] acceptance criteria (replaces the list)
workloom workitem transition --id <id> --to <status> --actor A --reason R --expect <hash>   # [w]
workloom workitem claim --id <id> --owner O --reason R [--expect <hash>]                    # [w] warns when no policy gates the item
workloom workitem release/start/block/complete --id <id> --actor A --reason R [--expect <hash>]  # [w]
workloom workflow check                       # validate policy files (read-only)
workloom workflow list|get|start|next|step-complete|pause|resume|cancel --id <workitem>   # [w] instance writes
workloom approval list|get|request|approve|reject     # [w] request/approve/reject write
workloom decision/finding/event/artifact list|get|create ...    # [w] create writes
workloom run list|get|log|create|update|heartbeat|verify|complete|fail|cancel   # [w] except list/get/log/verify
workloom context get [--task <id>] [--limit N]   # read-only aggregation
workloom knowledge status                     # 0 fresh / 10 stale / 11 missing
workloom workspace view                       # read-only summary
workloom doctor                               # read-only reconcile report
workloom dispatch [--once|--dry-run|--watch] --actor A --reason R   # [w] one scheduling tick
workloom recover --actor A --reason R         # [w] operator recovery (idempotent)
workloom sync status                          # handoff readiness (exit 0)
workloom archive events|runs --actor A --reason R    # [w] conservative archive (no delete)
` + "```" + `
`

const skillFixRef = skillMarker + `
# Devsys troubleshooting

Exit codes: 0 success / 1 internal / 2 usage / 3 precondition / 4 untrusted
managed state / 10 knowledge stale / 11 knowledge missing.

- ` + "`version mismatch … rerun workitem get`" + ` (exit 4): re-read the item
  and retry with the fresh hash. Never invent a hash. ` + "`--latest`" + ` is the
  explicit spelling of this path for single-operator work (still CAS).
- ` + "`storage: version conflict`" + ` (exit 4): another writer moved the file
  after your read (a dispatch child is the usual one behind ` + "`run fail`" + `);
  re-read and retry, or pass ` + "`--latest`" + ` where the command offers it.
- ` + "`quality gate not satisfied`" + ` (exit 4): the claim is refused. ` + "`workloom next`" + `
  reports the same ready items as a ` + "`quality_blocked`" + ` risk and prints the
  ` + "`workitem update`" + ` command that unblocks the claim; ` + "`claim`" + ` prints a
  ` + "`warning:`" + ` line when no policy governs the item (no instance and no
  ` + "`config.yaml default_policy`" + ` — the gates did not run).
- Stage gate "an approved, unconsumed approval is required" with an
  invalidation note: the earlier approval died when the work item left the
  status it was requested from (§4.9); request a new one.
- ` + "`no .devsys/ … run workloom init first`" + ` (exit 3): wrong directory or
  uninitialized project; find the project root first.
- Illegal transition (exit 4): stderr lists the allowed next states.
- ` + "`repair --dry-run`" + ` prints a digest; ` + "`--apply --confirm <digest>`" + `
  revalidates before writing (the only path that may lower completeness).
- Windows Git Bash: avoid ` + "`$(…)`" + ` capture of JSON for tokens; read
  ` + "`grep '^token:' .devsys/scheduling/<id>.yaml`" + ` instead.
- ` + "`mcp install`" + ` refuses JSONC or a Codex inline ` + "`mcp_servers`" + ` table,
  even with ` + "`--force`" + `. Paste ` + "`workloom wire --print-mcp <codex|claude|opencode>`" + `
  instead. The default is a dry-run; ` + "`--apply`" + ` writes.
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
