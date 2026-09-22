#!/usr/bin/env bash
# M8: device handoff (sync status), conflict annotation + dispatch gate,
# conservative archiving, and the Contrabass-board fixture loop.
# Repeatable. Requires Go, Git and Python.
# Windows: run with Git Bash, not a WSL shell without Go installed.
set -euo pipefail
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
case "$(uname -s)" in
  MINGW*|MSYS*) export PATH="/c/Program Files/Go/bin:$PATH" ;;
esac
command -v go >/dev/null || { echo 'Go is required; on Windows use Git Bash.' >&2; exit 127; }
command -v python >/dev/null || { echo 'Python is required.' >&2; exit 127; }
keep=0
case "${1:-}" in
  -Keep|--keep) keep=1 ;;
  "") ;;
  *) echo "usage: $0 [--keep]" >&2; exit 2 ;;
esac
workspace="$(mktemp -d "${TMPDIR:-/tmp}/smoke-m8-XXXXXX")"
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
# The command binary is linked with -s -w: some endpoint protection flags the
# unstripped build of this program as a false positive, and the smoke suite is
# about behaviour, not debug symbols.
(cd "$repo_root" && go build -ldflags "-s -w" -o "$workspace/workloom.exe" ./cmd/workloom)
D="$workspace/workloom.exe"

git init -q "$project"
cd "$project"
git config user.email devsys@test
git config user.name devsys
git config core.autocrlf false
"$D" init >/dev/null
item_id="$("$D" workitem create --title "接力任务" --actor me --reason smoke | head -1 | cut -f1)"
version_of() { "$D" --json workitem get "$1" | python -c "import json,sys; print(json.load(sys.stdin)['version'])"; }
walk() { # walk <id> <targets...>: fresh --expect per hop
  local id="$1"; shift
  for target in "$@"; do
    "$D" workitem transition --id "$id" --to "$target" --actor me --reason smoke --expect "$(version_of "$id")" >/dev/null
  done
}
walk "$item_id" backlog ready
git branch -M main
git add -A && git commit -qm fixture
remote="$workspace/origin.git"
git init -q --bare "$remote"
git remote add origin "$remote"
git push -qu origin main

echo "== sync status classifies the handoff (M8.1) =="
"$D" sync status >"$workspace/sync-clean.txt"
grep -q "handoff: ready" "$workspace/sync-clean.txt" || { echo "FAIL: clean tree not ready" >&2; cat "$workspace/sync-clean.txt" >&2; exit 1; }
"$D" --json sync status | python -c "
import json, sys
d = json.load(sys.stdin)
assert d['handoff_ready'] is True and d['blockers'] == [], d
print('json: handoff_ready=True, no blockers')
"
echo "PASS: a clean pushed tree is ready"
# Dirty .devsys/ blocks the handoff without any network or write: event record
# appends to the tracked shard, the verdict classifies it, checkout restores.
"$D" event record --type note --subject-type workitem --subject "$item_id" --actor me --content dirty-probe >/dev/null
"$D" sync status >"$workspace/sync-dirty.txt"
grep -q "blocked \[uncommitted-devsys\]" "$workspace/sync-dirty.txt" || { echo "FAIL: dirty .devsys/ not classified" >&2; cat "$workspace/sync-dirty.txt" >&2; exit 1; }
grep -q "handoff: NOT ready" "$workspace/sync-dirty.txt" || { echo "FAIL: dirty tree reported ready" >&2; exit 1; }
git checkout -q -- .devsys
echo "PASS: uncommitted .devsys/ blocks with uncommitted-devsys, then recovers"
# A live claim blocks under its own code; release + commit restores ready.
CLAIM_JSON="$("$D" --json workitem claim --id "$item_id" --owner old-device --reason handoff)"
echo "$CLAIM_JSON" | python -c "import json,sys; d=json.load(sys.stdin); assert d['status']=='in_progress', d"
"$D" sync status >"$workspace/sync-leased.txt"
grep -q "blocked \[active-leases\]" "$workspace/sync-leased.txt" || { echo "FAIL: active lease not classified" >&2; cat "$workspace/sync-leased.txt" >&2; exit 1; }
grep -q "handoff: NOT ready" "$workspace/sync-leased.txt" || { echo "FAIL: leased tree reported ready" >&2; exit 1; }
"$D" workitem release --id "$item_id" --owner old-device --token "$(grep '^token:' .devsys/scheduling/"$item_id".yaml | awk '{print $2}')" --actor me --reason handoff-done --expect "$(version_of "$item_id")" >/dev/null
git add -A && git commit -qm "chore(workloom): claim and release $item_id"
git push -q origin main
"$D" sync status | grep -q "handoff: ready" || { echo "FAIL: release did not restore ready" >&2; exit 1; }
echo "PASS: an active lease blocks with active-leases; release restores ready"

echo "== unresolved merge is annotated, never auto-merged (M8.2) =="
printf 'base\n' > clash.txt
git add -A && git commit -qm "clash base" && git push -q origin main
git checkout -qb side
printf 'side\n' > clash.txt && git commit -qam "side clash"
git checkout -q main
printf 'main\n' > clash.txt && git commit -qam "main clash"
if git merge side >/dev/null 2>&1; then
  echo "FAIL: expected a merge conflict" >&2; exit 1
fi
"$D" repair --dry-run --actor me --reason conflict >"$workspace/repair.txt"
grep -q "note_unmerged_paths" "$workspace/repair.txt" || { echo "FAIL: repair missed the unmerged path" >&2; cat "$workspace/repair.txt" >&2; exit 1; }
grep -q "MERGE_HEAD" "$workspace/repair.txt" || { echo "FAIL: repair missed the merge machinery" >&2; cat "$workspace/repair.txt" >&2; exit 1; }
D1="$(grep '^digest:' "$workspace/repair.txt")"
D2="$("$D" repair --dry-run --actor me --reason conflict | grep '^digest:')"
[[ "$D1" == "$D2" ]] || { echo "FAIL: digest unstable: $D1 vs $D2" >&2; exit 1; }
DIGEST="$(echo "$D1" | awk '{print $2}')"
echo "PASS: the dry run annotates clash.txt + MERGE_HEAD with a stable digest"
TREE_BEFORE="$(git rev-parse HEAD:.devsys)"
"$D" repair --apply --confirm "$DIGEST" --actor me --reason conflict >"$workspace/apply.txt"
grep -q "rejected clash.txt: requires human resolution; no automatic merge" "$workspace/apply.txt" || { echo "FAIL: apply did not reject clash.txt" >&2; cat "$workspace/apply.txt" >&2; exit 1; }
TREE_AFTER="$(git rev-parse HEAD:.devsys)"
[[ "$TREE_BEFORE" == "$TREE_AFTER" ]] || { echo "FAIL: apply touched .devsys under conflict" >&2; exit 1; }
echo "PASS: apply rejects every note and writes nothing under conflict"
set +e
"$D" dispatch --dry-run --actor me --reason tick >/dev/null 2>"$workspace/dispatch-blocked.txt"
code=$?
set -e
[[ "$code" -eq 3 ]] || { echo "FAIL: dispatch exited $code, want 3" >&2; exit 1; }
grep -q "dispatch blocked" "$workspace/dispatch-blocked.txt" || { echo "FAIL: dispatch did not name the block" >&2; cat "$workspace/dispatch-blocked.txt" >&2; exit 1; }
echo "PASS: dispatch refuses the tick (exit 3) while the merge is open"
printf 'resolved\n' > clash.txt
git add -A && git commit -qm "resolve: keep main"
git push -q origin main
"$D" dispatch --dry-run --actor me --reason tick >/dev/null || { echo "FAIL: dispatch did not recover after resolve" >&2; exit 1; }
"$D" sync status | grep -q "handoff: ready" || { echo "FAIL: sync not ready after resolve" >&2; exit 1; }
echo "PASS: a human resolve restores dispatch and the handoff verdict"

echo "== archiving keeps history queryable and shrinks live (M8.3) =="
"$D" event record --type note --subject-type workitem --subject "$item_id" --actor me --content live-event >/dev/null
N_BEFORE="$("$D" --json event list | python -c "import json,sys; print(len(json.load(sys.stdin)['events']))")"
LIVE_BEFORE="$(python -c "import os; print(sum(os.path.getsize(os.path.join(r,f)) for d in ('.devsys/events',) for r,_,fs in os.walk(d) for f in fs if f.endswith('.jsonl')))")"
"$D" archive events --before 2999-01 --dry-run --actor me --reason trim >"$workspace/archive-dry.txt"
grep -q "dry-run: nothing was moved" "$workspace/archive-dry.txt" || { echo "FAIL: dry run header missing" >&2; cat "$workspace/archive-dry.txt" >&2; exit 1; }
# The dry run must write nothing: only the pre-existing uncommitted fixture
# (the live-event record + claim/release cycle) may show as modified.
untracked="$(git status --short | grep '^??' || true)"
test -z "$untracked" || { echo "FAIL: archive dry run created files" >&2; echo "$untracked" >&2; exit 1; }
echo "PASS: the archive dry run lists the move and writes nothing"
"$D" archive events --before 2999-01 --actor me --reason trim >"$workspace/archive.txt"
grep -q "live bytes: $LIVE_BEFORE -> 0 " "$workspace/archive.txt" || { echo "FAIL: live bytes did not drain" >&2; cat "$workspace/archive.txt" >&2; exit 1; }
test -f .devsys/archive/manifest.yaml || { echo "FAIL: manifest missing" >&2; exit 1; }
N_AFTER="$("$D" --json event list | python -c "import json,sys; print(len(json.load(sys.stdin)['events']))")"
[[ "$N_BEFORE" == "$N_AFTER" ]] || { echo "FAIL: events $N_BEFORE -> $N_AFTER across archive" >&2; exit 1; }
echo "PASS: $N_AFTER events still queryable after live drained to 0"
# A running stream refuses archiving: only terminal runs may move. The guard
# uses its own item so the handoff item keeps its history for the board loop.
guard_id="$("$D" workitem create --title "归档守卫" --actor me --reason smoke | head -1 | cut -f1)"
walk "$guard_id" backlog ready
RUN_ID="$("$D" --json workitem claim --id "$guard_id" --owner archivist --reason runs-guard | python -c "import json,sys; print(json.load(sys.stdin)['run_id'])")"
if "$D" archive runs --id "$RUN_ID" --actor me --reason trim >/dev/null 2>"$workspace/archive-runs.txt"; then
  echo "FAIL: a running stream was archived" >&2; exit 1
fi
grep -q "only terminal runs may be archived" "$workspace/archive-runs.txt" || { echo "FAIL: refusal unexplained" >&2; cat "$workspace/archive-runs.txt" >&2; exit 1; }
echo "PASS: a running stream is refused with only-terminal-runs"
"$D" workitem release --id "$guard_id" --owner archivist --token "$(grep '^token:' .devsys/scheduling/"$guard_id".yaml | awk '{print $2}')" --actor me --reason runs-guard-done --expect "$(version_of "$guard_id")" >/dev/null
git add -A && git commit -qm "chore(workloom): archive segment $item_id"
git push -q origin main
echo "== board fixture loop flows back through the CLI only (M8.4) =="
walk "$item_id" review verification
git add -A && git commit -qm "chore(workloom): $item_id to verification"
python "$repo_root/scripts/contrabass/export.py" --root "$project" --out "$workspace/board" --devsys "$D" | grep -q "verification -> review" || { echo "FAIL: export missed the card" >&2; exit 1; }
python - "$workspace/board/issues/$item_id.json" <<'PY'
import json, sys
path = sys.argv[1]
card = json.load(open(path))
card['board_status'] = 'done'
card['result'] = {'summary': 'board executed OK', 'log': 'evidence'}
json.dump(card, open(path, 'w'), indent=2, ensure_ascii=False)
print('board marked done with a summary')
PY
python "$repo_root/scripts/contrabass/import.py" --root "$project" --board "$workspace/board" --dry-run --actor board --reason verdict --devsys "$D" | grep -q "dry-run would" || { echo "FAIL: import dry run silent" >&2; exit 1; }
test -z "$(git status --short -- .devsys)" || { echo "FAIL: import dry run touched .devsys" >&2; git status --short >&2; exit 1; }
echo "PASS: import --dry-run previews and leaves .devsys alone"
python "$repo_root/scripts/contrabass/import.py" --root "$project" --board "$workspace/board" --actor board --reason verdict --devsys "$D" | grep -q "verification -> done" || { echo "FAIL: import did not transition" >&2; exit 1; }
"$D" --json workitem get "$item_id" | python -c "
import json, sys
assert json.load(sys.stdin)['item']['status'] == 'done', 'board verdict did not land'
print('workitem is done')
"
"$D" --json event list --subject-type workitem --subject "$item_id" | python -c "
import json, sys
evs = json.load(sys.stdin)['events']
assert any(e['type'] == 'status_changed' and 'verification -> done' in (e.get('content') or '') for e in evs), evs[-3:]
assert any(e['type'] == 'comment' and (e.get('content') or '').startswith('[board] board executed OK') for e in evs), evs[-3:]
print('events carry the transition and the [board] comment')
"
echo "PASS: the board verdict lands as a transition plus a [board] comment"
git add -A && git commit -qm "chore(workloom): board verdict $item_id"
git push -q origin main
"$D" sync status | grep -q "handoff: ready" || { echo "FAIL: final handoff not ready" >&2; exit 1; }
test -z "$(git status --short)" || { echo "FAIL: final tree dirty" >&2; git status --short >&2; exit 1; }

echo "PASS: M8 device handoff (sync verdicts, conflict annotation, archive, board loop)."
success=1
