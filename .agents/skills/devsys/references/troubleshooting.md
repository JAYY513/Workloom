<!-- devsys-skill -->
# Devsys troubleshooting

Exit codes: 0 success / 1 internal / 2 usage / 3 precondition / 4 untrusted
managed state / 10 knowledge stale / 11 knowledge missing.

- `version mismatch … rerun workitem get` (exit 4): re-read the item
  and retry with the fresh hash. Never invent a hash. `--latest` is the
  explicit spelling of this path for single-operator work (still CAS).
- `no .devsys/ … run devsys init first` (exit 3): wrong directory or
  uninitialized project; find the project root first.
- Illegal transition (exit 4): stderr lists the allowed next states.
- `repair --dry-run` prints a digest; `--apply --confirm <digest>`
  revalidates before writing (the only path that may lower completeness).
- Windows Git Bash: avoid `$(…)` capture of JSON for tokens; read
  `grep '^token:' .devsys/scheduling/<id>.yaml` instead.
