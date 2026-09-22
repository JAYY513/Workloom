#!/usr/bin/env bash
# M6: execution layer — workspace invariants, the dispatch tick, retry and
# stall handling, the completion check, and the harness adapters (driven here
# through a stub codex on PATH, so the smoke suite needs no model provider).
# Repeatable. Requires Go, Git and Python.
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
workspace="$(mktemp -d "${TMPDIR:-/tmp}/smoke-m6-XXXXXX")"
project="$workspace/demo-project"
stubs="$workspace/bin"
success=0
cleanup() {
  if [[ $success -eq 1 && $keep -eq 1 ]]; then
    echo "KEPT_PROJECT=$project"
  else
    rm -rf -- "$workspace"
  fi
}
trap cleanup EXIT
mkdir -p -- "$project" "$stubs"
export DEVSYS_CONFIG_DIR="$workspace/config"
# The command binary is linked with -s -w: some endpoint protection flags the
# unstripped build of this program as a false positive, and the smoke suite is
# about behaviour, not debug symbols.
(cd "$repo_root" && go build -ldflags "-s -w" -o "$workspace/workloom.exe" ./cmd/workloom)
D="$workspace/workloom.exe"

# A stub codex stands in for the real CLI: the smoke suite pins the adapter
# chain (prompt on stdin, workspace cwd, exit code) without a provider.
if [[ "$(uname -s)" == MINGW* || "$(uname -s)" == MSYS* ]]; then
  cat > "$stubs/codex.cmd" <<'STUB'
@echo off
powershell -NoProfile -Command "$p = [Console]::In.ReadToEnd(); if ($p.Length -lt 10) { exit 9 }; [Console]::Error.WriteLine('stub codex: prompt bytes=' + $p.Length)"
if "%STUB_CODEX_FAIL%"=="1" (echo stub: refusing to work 1>&2 & exit /b 7)
git add -A
git -c user.email=agent@test -c user.name=agent commit -q -m "agent work" --allow-empty
echo {"type":"turn.completed","stub":true}
STUB
else
  cat > "$stubs/codex" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
prompt="$(cat)"
[[ ${#prompt} -ge 10 ]] || exit 9
echo "stub codex: prompt bytes=${#prompt}" >&2
if [[ "${STUB_CODEX_FAIL:-}" == "1" ]]; then echo "stub: refusing to work" >&2; exit 7; fi
git add -A
git -c user.email=agent@test -c user.name=agent commit -q -m "agent work" --allow-empty
echo '{"type":"turn.completed","stub":true}'
STUB
  chmod +x "$stubs/codex"
fi
export PATH="$workspace:$stubs:$PATH"  # devsys and the stub are both callable

git init -q "$project"
cd "$project"
"$D" init >/dev/null
cat > .devsys/workflows/task.md <<'EOF'
---
id: task
name: 执行层剧本
version: 1
steps:
  - id: implement
    type: execute
    required: true
concurrency:
  global: 1
limits:
  max_attempts: 2
  backoff_max_seconds: 1
  stall_threshold_seconds: 3600
---
在仓库根创建 notes.txt（一行 hello）并提交：{{workitem.title}}
EOF
cat >> .devsys/config.yaml <<'EOF'
dispatch_command: "echo dispatched"
EOF

create_item() {
  local title="$1" priority="$2"
  local id
  # workitem create prints a summary block; only the first line names the id.
  id="$("$D" workitem create --title "$title" --actor me --reason demo | head -1 | cut -f1)"
  "$D" workitem update --id "$id" --priority "$priority" >/dev/null
  "$D" workflow start --id "$id" --policy task --actor me --reason demo >/dev/null
  for target in backlog ready; do
    "$D" workitem transition --id "$id" --to "$target" --actor me --reason demo >/dev/null
  done
  printf '%s' "$id"
}
LOW="$(create_item "低优先级" 5)"
HIGH="$(create_item "高优先级" 9)"
git add -A && git -c user.email=devsys@test -c user.name=devsys commit -q -m fixture

echo "== workspace invariants =="
"$D" worktree list | grep -q "no workspaces" && echo "PASS: list is empty before any prepare"
if "$D" worktree remove --path "$workspace" --actor me --reason demo >/dev/null 2>&1; then
  echo "FAIL: a path outside the workspace root was accepted" >&2; exit 1
fi
echo "PASS: out-of-root removal refused"

echo "== dispatch tick (cap 1, ordered) =="
"$D" dispatch --once --actor ops --reason tick >"$workspace/tick1.txt"
grep -q "started $HIGH" "$workspace/tick1.txt" || { echo "FAIL: the higher priority item did not start first" >&2; cat "$workspace/tick1.txt"; exit 1; }
grep -q "cap_global" "$workspace/tick1.txt" || { echo "FAIL: the cap did not hold back the second item" >&2; exit 1; }
echo "PASS: only the higher priority item started, the other was capped"
"$D" dispatch --once --actor ops --reason tick >"$workspace/tick2.txt"
grep -q "in flight: 1" "$workspace/tick2.txt" || { echo "FAIL: the tick lost track of the in-flight attempt" >&2; exit 1; }
before="$(ls .devsys/runs/*.yaml | wc -l)"
"$D" dispatch --dry-run --actor ops --reason preview >/dev/null
after="$(ls .devsys/runs/*.yaml | wc -l)"
[[ "$before" == "$after" ]] || { echo "FAIL: the dry run created runs" >&2; exit 1; }
echo "PASS: a repeated tick starts nothing; the dry run changes nothing"

echo "== harness adapter chain (stub codex) =="
run_id="$("$D" run create --workitem "$HIGH" --actor me --reason harness | cut -f1)"
"$D" worktree prepare --workitem "$HIGH" --run "$run_id" --actor me --reason harness >/dev/null
printf 'hello\n' > ".devsys/workspaces/$HIGH/notes.txt" 2>/dev/null || true
"$D" run exec --id "$run_id" --harness codex --actor ops --reason acceptance --timeout 120s >/dev/null
"$D" run verify --id "$run_id" | grep -q "advanced" || { echo "FAIL: the completion check did not see the agent's commit" >&2; exit 1; }
"$D" run complete --id "$run_id" --actor ops --reason "acceptance" | grep -q succeeded
python - "$run_id" <<'PY'
import json, subprocess, sys
run = json.loads(subprocess.run(["workloom.exe", "--json", "run", "get", sys.argv[1]], capture_output=True, text=True).stdout)["run"]
assert run["agent"]["harness"] == "codex", run["agent"]
assert run["workspace"]["worktree"] == "WLM-2", run["workspace"]
print("PASS: harness=%s workspace=%s advanced=%s" % (run["agent"]["harness"], run["workspace"]["worktree"], run["verification"]["advanced"]))
PY

echo "== completion check refuses work that did not advance =="
run_id="$("$D" run create --workitem "$LOW" --actor me --reason check | cut -f1)"
"$D" worktree prepare --workitem "$LOW" --run "$run_id" --actor me --reason check >/dev/null
if "$D" run complete --id "$run_id" --actor agent --reason done >/dev/null 2>"$workspace/refusal.txt"; then
  echo "FAIL: a run that advanced nothing was marked succeeded" >&2; exit 1
fi
grep -q "cannot be marked succeeded" "$workspace/refusal.txt" || { echo "FAIL: the refusal was not explained" >&2; cat "$workspace/refusal.txt"; exit 1; }
"$D" --json workitem get "$LOW" | python -c "
import json,sys
item = json.load(sys.stdin)['item']
assert item['status'] == 'review', item['status']
print('PASS: the refusal routed', item['id'], 'to', item['status'])
"
"$D" run complete --id "$run_id" --force --by reviewer --actor reviewer --reason reviewed >/dev/null
echo "PASS: a reviewer accepted it explicitly"

echo "== retry sweep =="
# Free the slot the first dispatch holds, then let a third item fail its
# attempt: the next tick must sweep it into the retry queue.
# The harness-chain item still holds its claim: give it back so the next
# dispatch has a slot (the refusal path released the other one already).
read -r lease_owner lease_token <<<"$(python - "$HIGH" <<'PY'
import json, subprocess, sys
item = json.loads(subprocess.run(["workloom.exe", "--json", "workitem", "get", sys.argv[1]], capture_output=True, text=True).stdout)["item"]
print(item.get("lease_owner", ""), item.get("lease_token", ""))
PY
)"
"$D" workitem release --id "$HIGH" --owner "$lease_owner" --token "$lease_token" --actor ops --reason "free the slot" >/dev/null
FAIL_ID="$(create_item "会失败的尝试" 1)"
git add -A && git -c user.email=devsys@test -c user.name=devsys commit -q -m "fixture: failing item"
"$D" workitem update --id "$FAIL_ID" --assigned-harness codex >/dev/null
STUB_CODEX_FAIL=1 "$D" dispatch --once --actor ops --reason tick >"$workspace/tick3.txt"
grep -q "started $FAIL_ID" "$workspace/tick3.txt" || { echo "FAIL: the failing item was not dispatched" >&2; cat "$workspace/tick3.txt"; exit 1; }
sleep 3
"$D" dispatch --once --actor ops --reason tick >"$workspace/tick4.txt"
grep -q "swept $FAIL_ID" "$workspace/tick4.txt" || { echo "FAIL: the failed attempt was not swept" >&2; cat "$workspace/tick4.txt"; exit 1; }
grep -q "retry_queued" "$workspace/tick4.txt" || { echo "FAIL: the sweep did not queue a retry" >&2; exit 1; }
echo "PASS: the tick swept a failed attempt into the retry queue"

echo "== unavailable harness =="
run_id="$("$D" run create --workitem "$LOW" --actor me --reason harness-check | cut -f1)"
"$D" worktree prepare --workitem "$LOW" --run "$run_id" --actor me --reason harness-check >/dev/null
if "$D" run exec --id "$run_id" --harness claude --actor ops --reason acceptance 2>"$workspace/claude.txt"; then
  echo "NOTE: claude is installed on this machine; the refusal path was not exercised"
else
  grep -q "not available" "$workspace/claude.txt" || { echo "FAIL: the missing harness was not refused clearly" >&2; cat "$workspace/claude.txt"; exit 1; }
  echo "PASS: an unavailable harness is refused with its probe result"
fi

echo "== read-only commands never dispatch =="
before="$(ls .devsys/runs/*.yaml | wc -l)"
"$D" next >/dev/null || true
"$D" doctor >/dev/null || true
"$D" project status >/dev/null
after="$(ls .devsys/runs/*.yaml | wc -l)"
[[ "$before" == "$after" ]] || { echo "FAIL: a read-only command started an attempt" >&2; exit 1; }
echo "PASS: next, doctor and status changed nothing"

echo "PASS: M6 execution layer (workspaces, dispatch, retry, completion, harnesses)."
success=1
