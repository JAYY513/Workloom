<!-- devsys-skill -->
# Devsys CLI reference (read-only unless noted)

Source of truth: `devsys --help` and per-command usage. This file lists
the daily subset only.

```sh
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
```
