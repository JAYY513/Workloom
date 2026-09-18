#!/usr/bin/env bash
# M4.5 file-exchange protocol demo: read project state without the CLI's
# human output — raw managed files, `--jsonl` streams, `--json` envelopes —
# and branch on the documented exit codes. Repeatable; requires Go, Git and
# Python. Windows: run with Git Bash.
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
workspace="$(mktemp -d "${TMPDIR:-/tmp}/exchange-demo-XXXXXX")"
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
(cd "$repo_root" && go build -o "$workspace/devsys.exe" ./cmd/devsys)
D="$workspace/devsys.exe"
git init -q "$project"
cd "$project"
"$D" init >/dev/null

# --- fixtures -------------------------------------------------------------
"$D" workitem create --title "first exchange task" --actor demo --reason "protocol demo" >/dev/null
"$D" workitem create --title "second exchange task" --actor demo --reason "protocol demo" >/dev/null
"$D" decision create --title "exchange format" --decision "jsonl for lists, json for envelopes" --by demo >/dev/null

# --- 1. raw files: the managed state is the source of truth ---------------
echo "== raw files =="
python - "$project" <<'PY'
import glob, re, sys
root = sys.argv[1]
ids = []
for path in sorted(glob.glob(root + "/.devsys/workitems/*.yaml")):
    text = open(path, encoding="utf-8").read()
    ids.append(re.search(r"^id: (.+)$", text, re.M).group(1))
print("workitems from files:", ",".join(ids))
assert len(ids) == 2, ids
PY

# --- 2. --jsonl: one JSON record per line ---------------------------------
echo "== jsonl =="
"$D" --jsonl workitem list | python -c "
import json, sys
records = [json.loads(line) for line in sys.stdin if line.strip()]
assert len(records) == 2, records
print('jsonl records:', ','.join(r['id'] for r in records))
"

# --- 3. --json: one document with an envelope -----------------------------
echo "== json =="
"$D" --json decision list | python -c "
import json, sys
payload = json.load(sys.stdin)
assert payload['ok'] is True and len(payload['decisions']) == 1
print('json envelope ok:', payload['decisions'][0]['id'])
"

# --- 4. exit codes: scripts branch without parsing messages ---------------
echo "== exit codes =="
check_code() {
  local want="$1"; shift
  set +e
  "$@" >/dev/null 2>&1
  local got=$?
  set -e
  if [[ "$got" != "$want" ]]; then
    echo "FAIL: $* exited $got, want $want" >&2
    exit 1
  fi
  echo "  exit $got as documented: $*"
}
check_code 0 "$D" workitem list
check_code 2 "$D" bogus-command
check_code 3 "$D" workitem get NOPE-1
printf 'schema_version: 99\n' > .devsys/project.yaml
check_code 4 "$D" --json project get

echo "PASS: file exchange protocol (files, jsonl, json, exit codes)."
success=1
