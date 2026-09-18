#!/usr/bin/env bash
# M3: policy gates, claim quality gate, record propagation, last-known-good,
# workflow instances and approvals. Repeatable. Requires Go, Git and Python.
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
workspace="$(mktemp -d "${TMPDIR:-/tmp}/smoke-m3-XXXXXX")"
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
version_of() { "$D" --json workitem get "$1" | python -c "import json,sys; print(json.load(sys.stdin)['version'])"; }
helper() { (cd "$repo_root" && go run ./scripts/m3helper "$@"); }
new_item() {
  "$D" workitem create --title "$1" --actor smoke --reason smoke | sed -n 's/^\(WLM-[0-9]*\).*/\1/p'
}

# 1. Fixtures: parent + direct sibling + child on the quick-fix policy, with
#    one decision and one finding linked to the child.
"$D" init >/dev/null
cp "$repo_root/docs/examples/workflows/quick-fix.md" .devsys/workflows/
eval "$(helper setup "$project")"
[[ -n "$CHILD" && -n "$PARENT" && -n "$SIBLING" ]] || { echo 'helper setup failed' >&2; exit 1; }

# 2. The done gate blocks the advance and lists the missing artifact.
set +e
BLOCKED="$("$D" workitem transition --id "$CHILD" --to done --actor smoke --reason "try" --expect "$(version_of "$CHILD")" 2>&1)"
code=$?
set -e
[[ $code -eq 4 ]] || { echo "gate did not block (exit $code): $BLOCKED" >&2; exit 1; }
case "$BLOCKED" in *'artifact "test-results" is required'*) ;; *) echo "missing list incomplete: $BLOCKED" >&2; exit 1;; esac
echo 'PASS: done gate blocked the advance with the missing artifact list'

# 3. With the artifact and a comment the transition succeeds; the completion
#    propagates the child's records to the parent and the sibling without
#    touching their status.
echo "$(helper evidence "$project" "$CHILD")"
"$D" workitem transition --id "$CHILD" --to done --actor smoke --reason "verified" --expect "$(version_of "$CHILD")" >/dev/null
for target in "$PARENT" "$SIBLING"; do
  line="$(helper status "$project" "$target")"
  case "$line" in *decision://decision-1*) ;; *) echo "no decision ref on $target: $line" >&2; exit 1;; esac
  case "$line" in *finding://finding-1*) ;; *) echo "no finding ref on $target: $line" >&2; exit 1;; esac
  case "$line" in *'STATUS=draft'*) ;; *) echo "target status changed: $line" >&2; exit 1;; esac
  n="$(helper propagated "$project" "$target")"
  [[ "$n" == "PROPAGATED=1" ]] || { echo "expected one propagation event on $target, got: $n" >&2; exit 1; }
done
case "$(helper status "$project" "$CHILD")" in *'STATUS=done'*) ;; *) echo 'child did not reach done' >&2; exit 1;; esac
echo 'PASS: completion propagated records to parent and sibling, statuses untouched'

# 4. The claim quality gate blocks a thin task and names the improvements.
eval "$(helper lowq "$project")"
set +e
BLOCKED="$("$D" workitem claim --id "$LOWQ" --owner smoke --reason "try" --expect "$(version_of "$LOWQ")" 2>&1)"
code=$?
set -e
[[ $code -eq 4 ]] || { echo "quality gate did not block (exit $code): $BLOCKED" >&2; exit 1; }
case "$BLOCKED" in *quality_gate.min_score*) ;; *) echo "no threshold location: $BLOCKED" >&2; exit 1;; esac
case "$BLOCKED" in *标题过短*) ;; *) echo "no improvement items: $BLOCKED" >&2; exit 1;; esac
helper improve "$project" "$LOWQ" >/dev/null
"$D" workitem claim --id "$LOWQ" --owner smoke --reason "improved" --expect "$(version_of "$LOWQ")" >/dev/null
echo 'PASS: quality gate blocked a thin task and released the improved one'

# 5. Last-known-good: a corrupt policy blocks claims but keeps read paths and
#    advices working (with a warning). The successful claim above primed the
#    snapshot.
eval "$(helper lowq "$project")"; PRIMED="$LOWQ"
helper improve "$project" "$PRIMED" >/dev/null
eval "$(helper lowq "$project")"; BLOCKED_TASK="$LOWQ"
helper improve "$project" "$BLOCKED_TASK" >/dev/null
printf '%s\n' '---' 'name: 缺 id' 'version: "x"' '---' > .devsys/workflows/quick-fix.md
set +e
DENIED="$("$D" workitem claim --id "$BLOCKED_TASK" --owner smoke --reason "try" --expect "$(version_of "$BLOCKED_TASK")" 2>&1)"
code=$?
set -e
[[ $code -eq 4 ]] || { echo "invalid policy did not block the claim (exit $code): $DENIED" >&2; exit 1; }
case "$DENIED" in *'is invalid'*'last-known-good'*) ;; *) echo "claim refusal lacks the policy reason: $DENIED" >&2; exit 1;; esac
NEXT_JSON="$("$D" --json next)"
case "$NEXT_JSON" in *invalid_policy*) ;; *) echo "next lacks the invalid_policy risk: $NEXT_JSON" >&2; exit 1;; esac
WARNED="$("$D" workflow next --id "$PRIMED")"
case "$WARNED" in *last-known-good*) ;; *) echo "workflow next lacks the last-known-good notice: $WARNED" >&2; exit 1;; esac
cp "$repo_root/docs/examples/workflows/quick-fix.md" .devsys/workflows/quick-fix.md
"$D" workitem claim --id "$BLOCKED_TASK" --owner smoke --reason "restored" --expect "$(version_of "$BLOCKED_TASK")" >/dev/null
echo 'PASS: corrupt policy blocked claims, kept read paths on last-known-good, and recovery restored dispatch'

# 6. Workflow instances: condition branch, refused jump with the allowed list,
#    pause/resume, and no scheduling material.
cat > .devsys/workflows/flow.md <<'EOF'
---
id: flow
name: 流程
version: 1
steps:
  - id: inspect
    type: inspect
  - id: implement
    type: execute
  - id: verify
    type: verify
transitions:
  - from: inspect
    to: implement
    when: workitem.clarification_needed == false
  - from: inspect
    to: verify
    when: workitem.clarification_needed == true
  - from: implement
    to: verify
  - from: verify
    to: done
---
EOF
FLOW_WI="$(new_item 'flow smoke task')"
SCHED_BEFORE="$(cat .devsys/scheduling/*.yaml 2>/dev/null | md5sum || true)"
for t in backlog ready; do "$D" workitem transition --id "$FLOW_WI" --to "$t" --actor smoke --reason smoke --expect "$(version_of "$FLOW_WI")" >/dev/null; done
"$D" workflow start --id "$FLOW_WI" --policy flow --actor smoke --reason begin --expect "$(version_of "$FLOW_WI")" >/dev/null
NEXT_OUT="$("$D" workflow next --id "$FLOW_WI")"
case "$NEXT_OUT" in *'candidate: to=implement'*'next: implement'*) ;; *) echo "workflow next candidates wrong: $NEXT_OUT" >&2; exit 1;; esac
set +e
JUMP="$("$D" workflow step-complete --id "$FLOW_WI" --to nope --actor smoke --reason try --expect "$(version_of "$FLOW_WI")" 2>&1)"
code=$?
set -e
[[ $code -eq 4 ]] || { echo "jump was not refused (exit $code): $JUMP" >&2; exit 1; }
case "$JUMP" in *'not a declared transition'*'allowed next steps'*) ;; *) echo "jump refusal lacks the allowed list: $JUMP" >&2; exit 1;; esac
"$D" workflow step-complete --id "$FLOW_WI" --to implement --actor smoke --reason go --expect "$(version_of "$FLOW_WI")" >/dev/null
"$D" workflow pause --id "$FLOW_WI" --actor smoke --reason hold --expect "$(version_of "$FLOW_WI")" >/dev/null
set +e
PAUSED="$("$D" workflow step-complete --id "$FLOW_WI" --actor smoke --reason try --expect "$(version_of "$FLOW_WI")" 2>&1)"
code=$?
set -e
[[ $code -eq 4 ]] || { echo "paused advance was not refused (exit $code): $PAUSED" >&2; exit 1; }
case "$PAUSED" in *paused*) ;; *) echo "paused refusal lacks its reason: $PAUSED" >&2; exit 1;; esac
"$D" workflow resume --id "$FLOW_WI" --actor smoke --reason go --expect "$(version_of "$FLOW_WI")" >/dev/null
SCHED_AFTER="$(cat .devsys/scheduling/*.yaml 2>/dev/null | md5sum || true)"
[[ "$SCHED_BEFORE" == "$SCHED_AFTER" ]] || { echo 'workflow instance operations touched scheduling material' >&2; exit 1; }
echo 'PASS: workflow instance branched, refused a jump with the allowed list, held while paused, wrote no scheduling'

# 7. Approvals: the require_approval gate holds the advance until an approved
#    approval is consumed in the transition transaction.
cat > .devsys/workflows/approval-flow.md <<'EOF'
---
id: approval-flow
name: 审批门禁
version: 1
steps:
  - id: implement
    type: execute
gates:
  exempt_stages:
    - draft
    - backlog
  stages:
    in_progress:
      require_approval: true
---
EOF
APPROVAL_WI="$(new_item 'approval smoke task')"
for t in backlog ready; do "$D" workitem transition --id "$APPROVAL_WI" --to "$t" --actor smoke --reason smoke --expect "$(version_of "$APPROVAL_WI")" >/dev/null; done
"$D" workflow start --id "$APPROVAL_WI" --policy approval-flow --actor smoke --reason begin --expect "$(version_of "$APPROVAL_WI")" >/dev/null
set +e
GATED="$("$D" workitem transition --id "$APPROVAL_WI" --to in_progress --actor dev --reason try --expect "$(version_of "$APPROVAL_WI")" 2>&1)"
code=$?
set -e
[[ $code -eq 4 ]] || { echo "approval gate did not hold (exit $code): $GATED" >&2; exit 1; }
case "$GATED" in *'approval is required'*) ;; *) echo "approval refusal lacks its reason: $GATED" >&2; exit 1;; esac
"$D" approval request --id "$APPROVAL_WI" --stage in_progress --actor dev --reason "architecture change" >/dev/null
"$D" approval approve --id approval-1 --by boss --comment ok >/dev/null
"$D" workitem transition --id "$APPROVAL_WI" --to in_progress --actor dev --reason go --expect "$(version_of "$APPROVAL_WI")" >/dev/null
CONSUMED="$("$D" --json approval list --workitem "$APPROVAL_WI")"
case "$CONSUMED" in *'"consumed_at":"2'*) ;; *) echo "approval was not consumed: $CONSUMED" >&2; exit 1;; esac
# Rejecting a stage gate blocks the work item with the approval reference.
REJECT_WI="$(new_item 'reject smoke task')"
for t in backlog ready; do "$D" workitem transition --id "$REJECT_WI" --to "$t" --actor smoke --reason smoke --expect "$(version_of "$REJECT_WI")" >/dev/null; done
"$D" workflow start --id "$REJECT_WI" --policy approval-flow --actor smoke --reason begin --expect "$(version_of "$REJECT_WI")" >/dev/null
"$D" approval request --id "$REJECT_WI" --stage in_progress --actor dev --reason change >/dev/null
"$D" approval reject --id approval-2 --by boss --reason "risk too high" >/dev/null
REJECTED="$("$D" workitem get "$REJECT_WI" | sed -n '1p')"
case "$REJECTED" in *blocked*) ;; *) echo "rejected approval did not block the work item: $REJECTED" >&2; exit 1;; esac
case "$(cat .devsys/events/*.jsonl)" in *'approval-2 rejected: risk too high'*) ;; *) echo 'status_changed lacks the approval reference' >&2; exit 1;; esac
echo 'PASS: approval gate held until approval, consumed in the transition, and rejection blocked the work item'

success=1
echo 'PASS: M3 gates / quality gate / propagation / last-known-good / workflow instances / approvals.'
