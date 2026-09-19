#!/usr/bin/env bash
# M7: the read-only view — aggregation on a read-only copy, the offline static
# site, the local read-only service, and the freshness hints. No provider, no
# network: scratch project only.
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
workspace="$(mktemp -d "${TMPDIR:-/tmp}/smoke-m7-XXXXXX")"
project="$workspace/demo-project"
success=0
cleanup() {
  kill $(jobs -p) 2>/dev/null || true
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
(cd "$repo_root" && go build -ldflags "-s -w" -o "$workspace/devsys.exe" ./cmd/devsys)
D="$workspace/devsys.exe"
export PATH="$workspace:$PATH"  # devsys callable for the embedded checks

git init -q "$project"
cd "$project"
"$D" init >/dev/null
# One work item, then the page fixture below: mirrors scripts/smoke-m5.sh page().
item_id="$("$D" workitem create --title "看板任务" --actor me --reason smoke | head -1 | cut -f1)"
mkdir -p internal/store docs/repowiki/knowledge
printf 'package store\n' > internal/store/a.go
git add -A && git -c user.email=devsys@test -c user.name=devsys commit -q -m fixture
page() { # page <path> <source-commit> <sources...>
  local path="$1" commit="$2"; shift 2
  mkdir -p "$(dirname "$path")"
  {
    echo '---'
    echo 'status: stable'
    echo 'type: module'
    echo 'triggers:'
    echo '  - 存储层'
    echo 'description: 可靠文本存储的定位与边界'
    echo "source_commit: $commit"
    if [[ $# -gt 0 ]]; then
      echo 'sources:'
      for source in "$@"; do echo "  - $source"; done
    fi
    echo '---'
    echo
    echo '# 存储层'
    echo
    echo '正文。'
  } > "$path"
}
page docs/repowiki/knowledge/存储.md "$(git rev-parse HEAD)" 'internal/store/**'
git add -A && git -c user.email=devsys@test -c user.name=devsys commit -q -m pages
(cd .devsys && find . -path ./local -prune -o -type f -exec sha256sum {} + | sort) >"$workspace/before.sha"
echo "== view is read-only and complete (M7.1) =="
test -z "$(git status --short)" || { echo "FAIL: fixture left the tree dirty" >&2; git status --short >&2; exit 1; }
"$D" workspace view >"$workspace/view.txt"
for want in "progress:" "readiness:" "knowledge: fresh" "sources:"; do
  grep -q "$want" "$workspace/view.txt" || { echo "FAIL: view misses $want" >&2; cat "$workspace/view.txt" >&2; exit 1; }
done
"$D" --json workspace view | python -c "
import json, sys
m = json.load(sys.stdin)
assert m['ok'] is True, m
assert m['schema_version'] == 1, m['schema_version']
assert m['trust']['state'] == 'ok', m['trust']
assert len(m['progress']['items']) == 1, m['progress']
assert m['knowledge']['status'] == 'fresh', m['knowledge']
assert '.devsys/project.yaml' in m['sources'], m['sources']
print('model: items=1 knowledge=fresh trust=ok')
"
test -z "$(git status --short)" || { echo "FAIL: view wrote files" >&2; git status --short >&2; exit 1; }
(cd .devsys && find . -path ./local -prune -o -type f -exec sha256sum {} + | sort) >"$workspace/after.sha"
if ! cmp -s "$workspace/before.sha" "$workspace/after.sha"; then
  echo "FAIL: view changed managed files" >&2; diff "$workspace/before.sha" "$workspace/after.sha" >&2; exit 1
fi
echo "PASS: the view names its facts, sources and baseline, and writes nothing"
mkdir -p "$workspace/empty" && (cd "$workspace/empty" && git init -q .)
if (cd "$workspace/empty" && "$D" workspace view >/dev/null 2>&1); then
  echo "FAIL: view outside a project succeeded" >&2; exit 1
fi
(cd "$workspace/empty" && "$D" workspace view >/dev/null 2>"$workspace/noproj.txt") || code=$?
[[ "${code:-0}" -eq 3 ]] || { echo "FAIL: view outside a project exited $code, want 3" >&2; exit 1; }
echo "PASS: view outside a project is a precondition failure (exit 3)"

echo "== static site is offline and complete (M7.2) =="
site="$workspace/site"
"$D" workspace build --static --out "$site" | grep -q "6 pages" || { echo "FAIL: build did not report 6 pages" >&2; exit 1; }
for name in index.html tasks.html workflows.html runs.html records.html knowledge.html assets/style.css data/model.json; do
  test -f "$site/$name" || { echo "FAIL: missing $name" >&2; exit 1; }
done
echo "PASS: all 8 site files land below --out"
if grep -rEi 'https?://|@import|<script' "$site" >/dev/null; then
  echo "FAIL: the site references the network" >&2; grep -rEi 'https?://|@import|<script' "$site" >&2; exit 1
fi
grep -q '基线提交' "$site/index.html" || { echo "FAIL: index.html lost the baseline footer" >&2; exit 1; }
grep -q 'data/model.json' "$site/index.html" || { echo "FAIL: index.html lost the model link" >&2; exit 1; }
test -z "$(git status --short)" || { echo "FAIL: the explicit --out build wrote into the project" >&2; git status --short >&2; exit 1; }
echo "PASS: an explicit --out leaves the project tree clean"
"$D" workspace build --static >/dev/null
for name in index.html tasks.html workflows.html runs.html records.html knowledge.html assets/style.css data/model.json; do
  test -f ".devsys/dist/site/$name" || { echo "FAIL: default out misses $name" >&2; exit 1; }
done
echo "PASS: the default out is .devsys/dist/site/ (git-ignored)"
rm -rf .devsys/dist

echo "== serve is read-only and loopback-only (M7.3) =="
"$D" workspace serve --port 0 >"$workspace/serve-banner.txt" 2>&1 &
serve_pid=$!
for i in $(seq 1 100); do
  grep -Eq 'serving http://' "$workspace/serve-banner.txt" 2>/dev/null && break
  sleep 0.1
done
addr="$(sed -E 's#.*serving http://([^/]+)/.*#\1#' "$workspace/serve-banner.txt")"
test -n "$addr" || { echo "FAIL: no serve banner" >&2; cat "$workspace/serve-banner.txt" >&2; kill "$serve_pid" 2>/dev/null || true; exit 1; }
case "$addr" in 127.0.0.1:*) ;; *) echo "FAIL: serve bound $addr, want 127.0.0.1" >&2; kill "$serve_pid" 2>/dev/null || true; exit 1;; esac
python - "$addr" <<'PY'
import json, sys, urllib.request, urllib.error
addr = sys.argv[1]
def get(path, ctype):
    req = urllib.request.Request('http://%s%s' % (addr, path))
    with urllib.request.urlopen(req, timeout=10) as r:
        assert r.status == 200, (path, r.status)
        assert r.headers['Content-Type'].startswith(ctype), (path, r.headers['Content-Type'])
        assert r.headers['Cache-Control'] == 'no-store', (path, r.headers)
        return r.read().decode('utf-8')
for page in ['/', '/tasks.html', '/workflows.html', '/runs.html', '/records.html', '/knowledge.html']:
    body = get(page, 'text/html')
    assert '数据来源' in body and '基线提交' in body, page
get('/assets/style.css', 'text/css')
model = json.loads(get('/data/model.json', 'application/json'))
api = json.loads(get('/api/view', 'application/json'))
assert api['ok'] is True and api['generated_at'] and len(api['sources']) > 0, api
health = json.loads(get('/healthz', 'application/json'))
assert health == {'ok': True}, health
for method in ['POST', 'PUT', 'PATCH', 'DELETE']:
    for path in ['/', '/api/view', '/data/model.json']:
        req = urllib.request.Request('http://%s%s' % (addr, path), method=method)
        try:
            urllib.request.urlopen(req, timeout=10)
            raise SystemExit('FAIL: %s %s was not refused' % (method, path))
        except urllib.error.HTTPError as e:
            assert e.code == 405, (method, path, e.code)
            assert e.headers['Allow'] == 'GET, HEAD', (method, path, e.headers['Allow'])
            assert e.read().decode() == 'read-only: %s not allowed\n' % method, (method, path)
print('routes: 6 pages + css + model + api + healthz; writes refused with Allow: GET, HEAD')
PY
echo "PASS: every read route answers; every write method is a 405"
kill "$serve_pid" 2>/dev/null || true
wait "$serve_pid" 2>/dev/null || true
test -z "$(git status --short)" || { echo "FAIL: serve wrote files" >&2; git status --short >&2; exit 1; }
echo "PASS: the server wrote nothing while running"
if "$D" workspace serve --host 0.0.0.0 --port 0 >/dev/null 2>&1; then
  echo "FAIL: a non-loopback bind without --allow-remote was accepted" >&2; exit 1
fi
"$D" workspace serve --host 0.0.0.0 --port 0 >/dev/null 2>"$workspace/remote.txt" || code=$?
[[ "${code:-0}" -eq 2 ]] || { echo "FAIL: non-loopback bind exited $code, want 2" >&2; exit 1; }
echo "PASS: a non-loopback bind without --allow-remote is a usage error (exit 2)"

echo "== freshness hints name the fix (M7.4) =="
printf 'package store // edited\n' > internal/store/a.go
"$D" workspace view >"$workspace/stale.txt" || { echo "FAIL: stale view exited non-zero" >&2; exit 1; }
for want in "knowledge: stale" "affected: docs/repowiki/knowledge/存储.md" 'hint: run `devsys knowledge refresh` to regenerate affected pages'; do
  grep -qF "$want" "$workspace/stale.txt" || { echo "FAIL: stale view misses $want" >&2; cat "$workspace/stale.txt" >&2; exit 1; }
done
echo "PASS: a covered change marks the page stale, names it, and points at refresh (exit 0)"
set +e
"$D" knowledge status >/dev/null 2>&1; stale_code=$?
set -e
[[ "$stale_code" -eq 10 ]] || { echo "FAIL: knowledge status exited $stale_code, want 10" >&2; exit 1; }
echo "PASS: the same state through knowledge status exits 10 (one verdict, two faces)"
"$D" workspace build --static --out "$workspace/stale-site" >/dev/null
grep -q '知识已过期' "$workspace/stale-site/index.html" || { echo "FAIL: the stale site lost its banner" >&2; exit 1; }
grep -q 'devsys knowledge refresh' "$workspace/stale-site/knowledge.html" || { echo "FAIL: the stale knowledge page lost its hint" >&2; exit 1; }
echo "PASS: the stale static site carries the banner and the page hint"
"$D" workspace serve --port 0 >"$workspace/serve-stale.txt" 2>&1 &
serve_pid=$!
for i in $(seq 1 100); do
  grep -Eq 'serving http://' "$workspace/serve-stale.txt" 2>/dev/null && break
  sleep 0.1
done
stale_addr="$(sed -E 's#.*serving http://([^/]+)/.*#\1#' "$workspace/serve-stale.txt")"
python - "$stale_addr" <<'PY'
import sys, urllib.request
addr = sys.argv[1]
body = urllib.request.urlopen('http://%s/' % addr, timeout=10).read().decode('utf-8')
assert '知识已过期' in body, body[:500]
print('serve renders the stale banner from the same state')
PY
kill "$serve_pid" 2>/dev/null || true
wait "$serve_pid" 2>/dev/null || true
git checkout -q -- internal/store/a.go
rm -rf docs/repowiki
"$D" workspace view >"$workspace/missing.txt" || { echo "FAIL: missing view exited non-zero" >&2; exit 1; }
for want in "knowledge: missing" 'hint: configure `knowledge_generator`, then run `devsys knowledge refresh --full`'; do
  grep -qF "$want" "$workspace/missing.txt" || { echo "FAIL: missing view misses $want" >&2; cat "$workspace/missing.txt" >&2; exit 1; }
done
echo "PASS: a missing page layer names the supported degradation and --full"
git checkout -q -- docs/repowiki 2>/dev/null || true
mkdir -p .devsys/local/txn/txn-smoke
"$D" workspace view >"$workspace/pending.txt" || { echo "FAIL: pending view exited non-zero" >&2; exit 1; }
for want in "pending: txn-smoke" 'hint: run `devsys doctor` to inspect before `devsys recover`'; do
  grep -qF -- "$want" "$workspace/pending.txt" || { echo "FAIL: pending view misses $want" >&2; cat "$workspace/pending.txt" >&2; exit 1; }
done
if grep -q 'hint: run `devsys knowledge refresh`' "$workspace/pending.txt"; then
  echo "FAIL: the pending view suggests refresh against untrusted state" >&2; exit 1
fi
echo "PASS: pending transactions point at doctor, never at refresh"
rm -rf .devsys/local/txn/txn-smoke .devsys/dist

echo "PASS: M7 read-only view (aggregation, static site, local service, freshness hints)."
success=1
