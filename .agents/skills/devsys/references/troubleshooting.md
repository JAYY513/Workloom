<!-- devsys-skill -->
# Devsys troubleshooting

Exit codes: 0 success / 1 internal / 2 usage / 3 precondition / 4 untrusted
managed state / 10 knowledge stale / 11 knowledge missing.

- `version mismatch … rerun workitem get` (exit 4): re-read the item
  and retry with the fresh hash. Never invent a hash. `--latest` is the
  explicit spelling of this path for single-operator work (still CAS).
- `storage: version conflict` (exit 4): another writer moved the file
  after your read (a dispatch child is the usual one behind `run fail`);
  re-read and retry, or pass `--latest` where the command offers it.
- `quality gate not satisfied` (exit 4): the claim is refused. `workloom next`
  reports the same ready items as a `quality_blocked` risk and prints the
  `workitem update` command that unblocks the claim; `claim` prints a
  `warning:` line when no policy governs the item (no instance and no
  `config.yaml default_policy` — the gates did not run).
- Stage gate "an approved, unconsumed approval is required" with an
  invalidation note: the earlier approval died when the work item left the
  status it was requested from (§4.9); request a new one.
- `no .devsys/ … run workloom init first` (exit 3): wrong directory or
  uninitialized project; find the project root first.
- Illegal transition (exit 4): stderr lists the allowed next states.
- `repair --dry-run` prints a digest; `--apply --confirm <digest>`
  revalidates before writing (the only path that may lower completeness).
- Windows Git Bash: avoid `$(…)` capture of JSON for tokens; read
  `grep '^token:' .devsys/scheduling/<id>.yaml` instead.
- `mcp install` refuses JSONC or a Codex inline `mcp_servers` table,
  even with `--force`. Paste `workloom wire --print-mcp <codex|claude|opencode>`
  instead. The default is a dry-run; `--apply` writes.
