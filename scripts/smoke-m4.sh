#!/usr/bin/env bash
# M4: MCP surface (profiles, health, records, session, error taxonomy),
# CLI/MCP write parity, exchange protocol (exit codes, jsonl), wire
# idempotency. Repeatable. Requires Go, Git and Python.
# Windows: run with Git Bash, not a WSL shell without Go installed.
set -euo pipefail
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
case "$(uname -s)" in
  MINGW*|MSYS*) export PATH="/c/Program Files/Go/bin:$PATH" ;;
esac
command -v go >/dev/null || { echo 'Go is required; on Windows use Git Bash.' >&2; exit 127; }
keep=0
case "${1:-}" in
  -Keep|--keep) keep=1 ;;
  "") ;;
  *) echo "usage: $0 [--keep]" >&2; exit 2 ;;
esac
workspace="$(mktemp -d "${TMPDIR:-/tmp}/smoke-m4-XXXXXX")"
project="$workspace/demo-project"
success=0
cleanup() {
  if [[ $success -eq 1 && $keep -eq 1 ]]; then
    echo "KEPT_PROJECT=$project"
  else
    rm -rf -- "$workspace"
  fi
}
trap cleanup EXIT
mkdir -p -- "$project"
export DEVSYS_CONFIG_DIR="$workspace/config"
(cd "$repo_root" && go build -o "$workspace/workloom.exe" ./cmd/workloom && go build -o "$workspace/m4helper.exe" ./scripts/m4helper)
D="$workspace/workloom.exe"
git init -q "$project"
cd "$project"
"$D" init >/dev/null

# 1. MCP surface through a real SDK client (profiles, health, records,
#    session interface, error taxonomy).
echo "== MCP surface =="
"$workspace/m4helper.exe" "$D" "$project"

# 2. CLI/MCP write parity: an MCP-created record is read identically by the CLI.
echo "== write parity =="
decision_id="$("$D" --jsonl decision list | python -c "import json,sys; print(json.loads(sys.stdin.readline())['id'])")"
cli_version="$("$D" --json decision get "$decision_id" | python -c "import json,sys; print(json.load(sys.stdin)['version'])")"
[[ -n "$cli_version" ]] || { echo "FAIL: CLI could not read the MCP-created decision" >&2; exit 1; }
echo "PASS: CLI reads the MCP-created decision $decision_id (version $cli_version)"

# 3. Session interface: one call yields a recommendation.
echo "== session =="
"$D" --json session start --harness smoke --agent smoke --intent acceptance \
  | python -c "
import json, sys
view = json.load(sys.stdin)
assert view['ok'] is True
assert view['recommended_next_action']['type'], view
print('PASS: session start recommends', view['recommended_next_action']['type'])
"

# 4. Exchange protocol: exit codes branch, jsonl streams one record per line.
echo "== exchange =="
check_code() {
  local want="$1"; shift
  set +e
  "$@" >/dev/null 2>&1
  local got=$?
  set -e
  [[ "$got" == "$want" ]] || { echo "FAIL: $* exited $got, want $want" >&2; exit 1; }
  echo "PASS: exit $got: $*"
}
check_code 0 "$D" workitem list
check_code 2 "$D" bogus-command
check_code 3 "$D" workitem get NOPE-1
"$D" workitem create --title "smoke task" --actor smoke --reason smoke >/dev/null
"$D" --jsonl workitem list | python -c "
import json, sys
records = [json.loads(line) for line in sys.stdin if line.strip()]
assert len(records) == 1, records
print('PASS: jsonl streamed', records[0]['id'])
"

# 5. wire: three runs are byte-identical and hand-written content survives.
echo "== wire =="
cat > AGENTS.md <<'EOF'
# 手写说明

这段内容必须原样保留。

<!-- repowiki:begin | 由 repowiki 管理 -->
## repowiki

- docs/repowiki/

<!-- repowiki:end -->
EOF
"$D" wire >/dev/null
first_hash="$(md5sum AGENTS.md | cut -d' ' -f1)"
"$D" wire >/dev/null
"$D" wire >/dev/null
second_hash="$(md5sum AGENTS.md | cut -d' ' -f1)"
[[ "$first_hash" == "$second_hash" ]] || { echo "FAIL: wire is not idempotent" >&2; exit 1; }
grep -q "这段内容必须原样保留。" AGENTS.md || { echo "FAIL: wire dropped hand-written content" >&2; exit 1; }
grep -q "<!-- repowiki:begin" AGENTS.md || { echo "FAIL: wire dropped the repowiki block" >&2; exit 1; }
echo "PASS: wire is idempotent and preserves hand-written content"

# 6. knowledge degrades instead of failing.
echo "== knowledge =="
"$D" --json knowledge status | python -c "
import json, sys
view = json.load(sys.stdin)
assert view['ok'] is True and view['status'] == 'unavailable', view
print('PASS: knowledge status degrades honestly:', view['reason'][:48], '...')
"

echo "PASS: M4 MCP surface / write parity / session / exchange / wire / knowledge."
success=1
