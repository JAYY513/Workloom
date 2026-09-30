<!-- devsys-skill -->
# Devsys CLI reference (daily subset)

Source of truth: `workloom --help` and per-command usage. A line marked
`[w]` contains write subcommands (writes carry `--actor` / `--reason`, and most
carry a version guard: `--expect <hash>` or `--latest`).

```sh
workloom init                                 # [w] create .devsys/ in the current directory
workloom setup                                # [w] onboarding + quick-fix + safe new-project default_policy + wire/checks; existing choices win
workloom wire [--dry-run]                     # [w] inject the AGENTS.md discipline block
workloom wire --skill | --check | --print-mcp <codex|claude|opencode>
workloom mcp install [--scope user|project] [--client codex|claude|opencode] [--apply] [--force] [--dry-run]  # [w] register devsys; default dry-run, --apply writes, --force replaces an existing entry
workloom prime                                # orient: facts + recommended action (alias: session start --compact)
workloom session start [--compact]            # same, full context payload
workloom next                                 # readiness verdict (always exit 0); judges ready items with claim's quality gate
workloom project status                       # counts + risks + next
workloom project blueprint                    # exit 0 when no blueprint is declared
workloom project import-blueprint --artifact <id> --actor A --reason R [--expect <hash> | --latest]  # [w] import declared fields and bind
workloom project update --blueprint-artifact <id> --actor A --reason R [--expect <hash> | --latest]  # [w] bind only; does not import fields
workloom workitem list [--jsonl]              # one JSON record per line
workloom workitem get <id>                    # includes version: <64-hex>
workloom workitem create --title T --actor A --reason R [--description D | --description-file <path>] [--acceptance a,b]  # [w] one description source; a repeated --description is a usage error
workloom workitem update --id <id> --acceptance a,b                     # [w] acceptance criteria (replaces the list)
workloom workitem transition --id <id> --to <status> --actor A --reason R --expect <hash>   # [w]
workloom workitem claim --id <id> --owner O --reason R [--expect <hash>]                    # [w] warns when no policy gates the item
workloom workitem release/start/block/complete --id <id> --actor A --reason R [--expect <hash>]  # [w]
workloom workflow check                       # validate policy files (read-only)
workloom workflow list|get|start|next|step-complete|pause|resume|cancel --id <workitem>   # [w] instance writes
workloom approval list|get|request|approve|reject     # [w] request/approve/reject write
workloom event/artifact list|get|record|register|update ... # [w] timeline and durable stage evidence
workloom run list|get|log|create|update|heartbeat|verify|complete|fail|cancel   # [w] except list/get/log/verify
workloom context get [--task <id>] [--limit N]   # read-only aggregation
workloom knowledge status                     # 0 fresh / 10 stale / 11 missing
workloom workspace view                       # read-only summary
workloom doctor                               # read-only reconcile report
workloom dispatch [--once|--dry-run|--watch] --actor A --reason R   # [w] one scheduling tick
workloom recover --actor A --reason R         # [w] operator recovery (idempotent)
workloom sync status                          # handoff readiness (exit 0)
workloom archive events|runs --actor A --reason R    # [w] conservative archive (no delete)
```
