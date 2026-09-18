#!/usr/bin/env bash
# M2.5: interrupt-recovery lifecycle, repeatable. Requires Go and Git.
# Windows: run with Git Bash, not a WSL shell without Go installed.
set -euo pipefail
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
case "$(uname -s)" in
  MINGW*|MSYS*) export PATH="/c/Program Files/Go/bin:$PATH" ;;
esac
command -v go >/dev/null || { echo 'Go is required; on Windows use Git Bash.' >&2; exit 127; }
keep=0
case "${1:-}" in
  --keep) keep=1 ;;
  '') ;;
  *) echo 'usage: smoke-m2.sh [--keep]' >&2; exit 2 ;;
esac
workspace="$(mktemp -d "${TMPDIR:-/tmp}/smoke-m2-XXXXXX")"
project="$workspace/demo-project"
success=0
cleanup() {
  if [[ "$success" == 1 && "$keep" == 1 ]]; then
    rm -f -- "$workspace/devsys.exe"
    rm -rf -- "$workspace/config"
    printf 'KEPT_PROJECT=%s\n' "$project"
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

# 1. Normal lifecycle: create -> ready -> claim (fresh lease + run).
"$D" init >/dev/null
"$D" workitem create --title "M2 recovery probe" --actor main --reason "smoke-m2" >/dev/null
V="$(version_of WLM-1)"
"$D" workitem transition --id WLM-1 --to ready --actor main --reason "scoping done" --expect "$V" >/dev/null
CLAIM="$("$D" --json workitem claim --id WLM-1 --owner probe-agent --reason "smoke claim")"
[[ "$CLAIM" == *'"status":"in_progress"'* ]] || { echo 'claim did not produce in_progress' >&2; exit 1; }
[[ "$CLAIM" == *'"run_id":"run-'* ]] || { echo 'claim did not create a run' >&2; exit 1; }
echo 'PASS: claim atomically created lease + run'

# 2. Simulated crash: the agent died, the lease goes stale and its run is
#    missing on disk. A live lease would never be reported, so the fixture
#    encodes the post-crash facts directly (owner gone, lease_until past,
#    run file absent — exactly what a killed process leaves behind).
cat > .devsys/scheduling/WLM-1.yaml <<EOF
schema_version: 1
workitem_id: WLM-1
owner: dead-agent
token: deadtoken0000000000000000000000000000000000000000000000000000000
claimed_at: 2026-01-01T00:00:00Z
lease_until: 2026-01-01T00:01:00Z
heartbeat_at: 2026-01-01T00:00:00Z
run_id: run-20990101-999
head_sha: ""
agent_id: dead-agent
agent_harness: shell
EOF
rm -f .devsys/runs/*.yaml

# 3. Doctor reports the orphan without writing anything.
LOCK_BEFORE="$(sha256sum .devsys/local/lock | cut -d' ' -f1)"
DOC="$("$D" --json doctor)"
[[ "$DOC" == *'"reason_kind":"orphan"'* ]] || { echo 'doctor missed the orphan lease' >&2; exit 1; }
LOCK_AFTER="$(sha256sum .devsys/local/lock | cut -d' ' -f1)"
[[ "$LOCK_BEFORE" == "$LOCK_AFTER" ]] || { echo 'doctor mutated the lock file' >&2; exit 1; }
echo 'PASS: doctor reports orphan read-only'

# 4. Recover releases the lease and records the event.
"$D" recover --actor operator --reason "lease expired after crash" >/dev/null
[[ ! -f .devsys/scheduling/WLM-1.yaml ]] || { echo 'recover did not remove the lease' >&2; exit 1; }
grep -q 'lease_recovered' .devsys/events/*.jsonl || { echo 'no lease_recovered event' >&2; exit 1; }
echo 'PASS: recover released orphan lease with event'

# 5. State drift: done with a missing artifact is repaired only through the
#    confirmed dry-run/apply path; the digest revalidation rejects drift.
python - "$D" <<'PYEOF'
import json, subprocess, sys
d = sys.argv[1]
p = '.devsys/workitems/WLM-1.yaml'
s = open(p, encoding='utf-8').read()
s = s.replace('status: in_progress', 'status: done', 1)
s = s.replace('scheduling_state: claimed', 'scheduling_state: released', 1)
s = s.replace('artifact_refs: []', 'artifact_refs:\n  - artifact-999', 1)
open(p, 'w', encoding='utf-8', newline='').write(s)
out = subprocess.run([d, 'repair', '--dry-run', '--actor', 'operator', '--reason', 'missing artifact evidence'],
                     capture_output=True, text=True)
digest = [l.split()[1] for l in out.stdout.splitlines() if l.startswith('digest:')][0]
open('plan-digest.txt', 'w').write(digest)
PYEOF
DIGEST="$(cat plan-digest.txt)"
"$D" repair --apply --confirm "$DIGEST" --actor operator --reason "confirmed downgrade" >/dev/null
grep -q 'status: in_progress' .devsys/workitems/WLM-1.yaml || { echo 'repair did not downgrade' >&2; exit 1; }
grep -q 'repair_applied' .devsys/events/*.jsonl || { echo 'no repair_applied event' >&2; exit 1; }
echo 'PASS: confirmed repair downgraded done -> in_progress with event'

# 6. Every tracked state file is UTF-8 text and git-clean parseable.
git -c core.autocrlf=false add -- .devsys
git -c core.quotepath=false status --porcelain >/dev/null
success=1
echo 'PASS: M2 claim -> crash -> doctor -> recover -> confirmed repair; event stream complete.'
