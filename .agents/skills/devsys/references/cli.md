<!-- devsys-skill -->
# Devsys CLI reference (daily subset)

Source of truth: `devsys --help` and per-command usage. A line marked
`[w]` contains write subcommands (writes carry `--actor` / `--reason`, and most
carry a version guard: `--expect <hash>` or `--latest`).

```sh
devsys init                                 # [w] create .devsys/ (git repository root)
devsys wire [--dry-run]                     # [w] inject the AGENTS.md discipline block
devsys wire --skill | --check | --print-mcp <codex|claude|opencode>
devsys prime                                # orient: facts + recommended action (alias: session start --compact)
devsys session start [--compact]            # same, full context payload
devsys next                                 # readiness verdict (always exit 0)
devsys project status                       # counts + risks + next
devsys workitem list [--jsonl]              # one JSON record per line
devsys workitem get <id>                    # includes version: <64-hex>
devsys workitem create --title T --actor A --reason R                 # [w]
devsys workitem update --id <id> --acceptance a,b                     # [w] acceptance criteria (create has no such flag)
devsys workitem transition --id <id> --to <status> --actor A --reason R --expect <hash>   # [w]
devsys workitem claim --id <id> --owner O --reason R [--expect <hash>]                    # [w]
devsys workitem release/start/block/complete --id <id> --actor A --reason R [--expect <hash>]  # [w]
devsys workflow check                       # validate policy files (read-only)
devsys workflow list|get|start|next|step-complete|pause|resume|cancel --id <workitem>   # [w] instance writes
devsys approval list|get|request|approve|reject     # [w] request/approve/reject write
devsys decision/finding/event/artifact list|get|create ...    # [w] create writes
devsys run list|get|log|create|update|heartbeat|verify|complete|fail|cancel   # [w] except list/get/log/verify
devsys context get [--task <id>] [--limit N]   # read-only aggregation
devsys knowledge status                     # 0 fresh / 10 stale / 11 missing
devsys workspace view                       # read-only summary
devsys doctor                               # read-only reconcile report
devsys dispatch [--once|--dry-run|--watch] --actor A --reason R   # [w] one scheduling tick
devsys recover --actor A --reason R         # [w] operator recovery (idempotent)
devsys sync status                          # handoff readiness (exit 0)
devsys archive events|runs --actor A --reason R    # [w] conservative archive (no delete)
```
