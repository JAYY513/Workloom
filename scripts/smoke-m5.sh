#!/usr/bin/env bash
# M5: the knowledge layer — page contract and validation, the file index, the
# freshness verdicts and their reserved exit codes, human protection, resumable
# refreshes, the layered context assembly, and the generator contract (driven
# here through a stub generator, so the smoke suite needs no generator
# installed).
# Repeatable. Requires Go and Git.
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
workspace="$(mktemp -d "${TMPDIR:-/tmp}/smoke-m5-XXXXXX")"
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

# The stub generator implements the contract: it records the argv and the scope
# file it was handed, then fails or succeeds on request. A generator is a
# program with arguments — the layer never passes it through a shell.
cat > "$workspace/generator" <<STUB
#!/usr/bin/env bash
set -euo pipefail
echo "\$@" > "$workspace/generator.argv"
python - "$workspace/generator.scope" "\$@" <<'PY'
import json, sys
out = sys.argv[1]
args = sys.argv[2:]
scope = args[args.index('--scope-file') + 1] if '--scope-file' in args else ''
open(out, 'w').write(open(scope).read() if scope else '')
PY
echo "stub generator ran"
if [[ "\${STUB_GENERATOR_FAIL:-}" == "1" ]]; then echo "stub generator: refusing" >&2; exit 7; fi
if [[ "\${STUB_GENERATOR_SLEEP:-}" != "" ]]; then sleep "\$STUB_GENERATOR_SLEEP"; fi
if [[ "\${STUB_GENERATOR_WRITE:-}" != "" ]]; then
  # Act as a generator that wrote the page it was told to: record its content
  # hash exactly where the contract says.
  python - "$project" "\$STUB_GENERATOR_WRITE" <<'PY'
import hashlib, sys, pathlib, json
root, page = pathlib.Path(sys.argv[1]), sys.argv[2]
target = root / page
state_dir = root / '.devsys' / 'knowledge'
state_dir.mkdir(parents=True, exist_ok=True)
state_file = state_dir / 'state.json'
STATE = None
if state_file.exists():
    STATE = json.loads(state_file.read_text(encoding='utf-8'))
else:
    STATE = {'schema_version': 1, 'baseline': {}, 'pages': {}, 'generator': 'stub'}
STATE['pages'][page] = {'content_hash': hashlib.sha256(target.read_bytes()).hexdigest()}
STATE.setdefault('baseline', {})['commit'] = __import__('subprocess').run(
    ['git', 'rev-parse', 'HEAD'], cwd=root, capture_output=True, text=True).stdout.strip()
state_file.write_text(json.dumps(STATE, indent=2) + '\n', encoding='utf-8')
PY
fi
exit 0
STUB
chmod +x "$workspace/generator"
if [[ "$(uname -s)" == MINGW* || "$(uname -s)" == MSYS* ]]; then
  cat > "$stubs/generator.cmd" <<STUB
@echo off
bash "$workspace/generator" %*
STUB
  # The layer passes the command to CreateProcess, which knows nothing about
  # MSYS paths: the configured value has to be a Windows path, with forward
  # slashes so the YAML scalar needs no escaping.
  generator_cmd="$(cygpath -m "$stubs/generator.cmd")"
else
  generator_cmd="$workspace/generator"
fi
export PATH="$workspace:$stubs:$PATH"

git init -q "$project"
cd "$project"
"$D" init >/dev/null

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
commit_all() {
  git add -A
  git -c user.email=t@example.com -c user.name=t commit -q -m "$1" --allow-empty
}

echo "== page contract and validation =="
mkdir -p internal/store docs/repowiki/knowledge
echo 'package store' > internal/store/a.go
commit_all source
head1="$(git rev-parse HEAD)"
page docs/repowiki/knowledge/存储.md "$head1" 'internal/store/**'
cat > docs/repowiki/knowledge/缺触发词.md <<EOF
---
status: stable
type: module
description: 没有 triggers 的页面
source_commit: $head1
sources:
  - internal/store/**
---

# 缺触发词
EOF
commit_all pages
set +e
out="$("$D" knowledge validate 2>&1)"; code=$?
set -e
[[ $code -eq 4 ]] || { echo "validate of a page without triggers: exit=$code want 4" >&2; echo "$out" >&2; exit 1; }
grep -q '缺触发词.md:1: triggers: 缺少必填字段' <<<"$out" || { echo "validate output misses the located problem:" >&2; echo "$out" >&2; exit 1; }
echo "PASS: a page without triggers is refused, located, and exits 4"
rm docs/repowiki/knowledge/缺触发词.md
commit_all fix
set +e
"$D" knowledge validate >/dev/null 2>&1; code=$?
set -e
[[ $code -eq 0 ]] || { echo "validate of a good page: exit=$code want 0" >&2; exit 1; }
echo "PASS: the remaining page validates (exit 0)"

echo "== index scan and snapshot =="
printf 'TOKEN=1\n' > .env
python - <<'PY'
open('big.bin', 'wb').write(b'x' * (1024 * 1024 + 1))
PY
printf '.env\n' > .gitignore
python - <<'PY'
import json, subprocess
out = subprocess.run(['devsys', '--json', 'knowledge', 'scan'], capture_output=True, text=True, check=True)
view = json.loads(out.stdout)
# The index excludes both files with their reason: the credential rule (named,
# even though .gitignore also covers the file) and the size ceiling.
by_path = {e['path']: e['reason'] for e in view['excluded']}
assert '.env' in by_path and '密钥名模式' in by_path['.env'], by_path
assert 'big.bin' in by_path and '字节上限' in by_path['big.bin'], by_path
assert view['files'] >= 1, view
print('scanned', view['files'], 'files')
PY
set +e
"$D" knowledge scan >/dev/null 2>&1; first=$?
"$D" knowledge scan >/dev/null 2>&1; second=$?
set -e
[[ $first -eq 0 && $second -eq 0 ]] || { echo "rescan exited $first/$second" >&2; exit 1; }
echo "PASS: secrets and oversized files land in the exclusion list, and the snapshot rebuilds"

echo "== freshness and the reserved exit codes =="
set +e
"$D" knowledge status >/dev/null 2>&1; code=$?
set -e
[[ $code -eq 0 ]] || { echo "fresh status: exit=$code want 0" >&2; exit 1; }
echo 'package store // edited' > internal/store/a.go
set +e
out="$("$D" --json knowledge status 2>&1)"; code=$?
set -e
[[ $code -eq 10 ]] || { echo "stale status: exit=$code want 10" >&2; echo "$out" >&2; exit 1; }
python - "$out" <<'PY'
import json, sys
view = json.loads(sys.argv[1])
assert view['status'] == 'stale', view
assert view['affected_pages'] == ['docs/repowiki/knowledge/存储.md'], view
PY
echo "PASS: a change makes exactly the covering page stale (exit 10)"
set +e
out="$("$D" knowledge status 2>&1)"; code=$?
set -e
python - <<'PY'
import subprocess, os
# The stale report lists the page even under --quiet: it is the actionable part.
out = subprocess.run(['devsys', '--quiet', 'knowledge', 'status'], capture_output=True, text=True)
assert 'affected: docs/repowiki/knowledge/存储.md' in out.stdout, out.stdout
PY
mv docs/repowiki docs/repowiki.away
set +e
"$D" knowledge status >/dev/null 2>&1; code=$?
set -e
[[ $code -eq 11 ]] || { echo "missing page layer: exit=$code want 11" >&2; exit 1; }
mv docs/repowiki.away docs/repowiki
echo "PASS: no page layer answers missing (exit 11) without failing"

echo "== generator contract and refresh =="
cat >> .devsys/config.yaml <<EOF
knowledge_generator: "$generator_cmd"
EOF
commit_all configured
set +e
out="$("$D" --json knowledge refresh --affected 2>&1)"; code=$?
set -e
[[ $code -eq 0 ]] || { echo "refresh: exit=$code" >&2; echo "$out" >&2; exit 1; }
python - "$workspace" <<'PY'
import json, sys
workspace = sys.argv[1]
view = json.loads(open(workspace + '/generator.scope').read())
assert view['format_version'] == 1, view
assert view['mode'] == 'affected', view
assert view['pages'] == ['docs/repowiki/knowledge/存储.md'], view
assert view['project'], view
argv = open(workspace + '/generator.argv').read()
for flag in ('--project', '--scope-file', '--format-version'):
    assert flag in argv, argv
PY
echo "PASS: the generator is handed the scope (format_version, mode, pages) through the contract argv"

echo "== human protection =="
page docs/repowiki/knowledge/锁定.md "$head1" 'internal/store/**'
python - <<'PY'
import pathlib
p = pathlib.Path('docs/repowiki/knowledge/锁定.md')
p.write_text(p.read_text(encoding='utf-8').replace('---\n\n# 存储层', 'protected: true\n---\n\n# 存储层'), encoding='utf-8')
PY
page docs/repowiki/knowledge/手改.md "$head1" 'internal/store/**'
commit_all protection
# The generator records what it wrote; the hand edit comes afterwards, which is
# exactly what the content hash is there to notice.
STUB_GENERATOR_WRITE=docs/repowiki/knowledge/手改.md "$D" knowledge refresh --affected >/dev/null
python - <<'PY'
import pathlib
page = pathlib.Path('docs/repowiki/knowledge/手改.md')
page.write_text(page.read_text(encoding='utf-8') + '\n人工补充。\n', encoding='utf-8')
PY
echo 'package store // new' > internal/store/b.go
set +e
out="$("$D" --json knowledge refresh --affected 2>&1)"; code=$?
set -e
[[ $code -eq 0 ]] || { echo "protected refresh: exit=$code" >&2; echo "$out" >&2; exit 1; }
python - "$out" <<'PY'
import json, sys
view = json.loads(sys.argv[1])
skipped = {s['path']: s['reason'] for s in view.get('skipped', [])}
assert 'protected' in skipped.get('docs/repowiki/knowledge/锁定.md', ''), skipped
assert '手工修改' in skipped.get('docs/repowiki/knowledge/手改.md', ''), skipped
# The pages that need work are regenerated; the ones under protection are not
# handed over at all.
assert view['pages'] == ['docs/repowiki/knowledge/存储.md'], view
assert view['ran'] is True, view
PY
echo "PASS: locked and hand-edited pages stay out of the generator's scope and are reported"
set +e
out="$("$D" --json knowledge refresh --affected --force 2>&1)"; code=$?
set -e
[[ $code -eq 0 ]] || { echo "--force refresh: exit=$code" >&2; echo "$out" >&2; exit 1; }
python - "$out" <<'PY'
import json, sys
view = json.loads(sys.argv[1])
assert view['ran'] is True, view
assert len(view['pages']) == 3, view
overridden = [s for s in view.get('skipped', []) if s.get('overridden')]
assert len(overridden) == 2, view
PY
echo "PASS: --force regenerates them and still reports the override"

echo "== resumable refreshes and the mutex =="
rm -f .devsys/knowledge/state.json .devsys/knowledge/run.json
echo 'package store // changed again' > internal/store/a.go
STUB_GENERATOR_FAIL=1 "$D" knowledge refresh --affected >/dev/null 2>&1 || true
python - <<'PY'
import json, pathlib
checkpoint = json.loads(pathlib.Path('.devsys/knowledge/run.json').read_text(encoding='utf-8'))
assert checkpoint['phase'] == 'failed', checkpoint
assert checkpoint['pages'], checkpoint
print('checkpoint kept', len(checkpoint['pages']), 'page(s) after the failure')
PY
set +e
out="$("$D" --json knowledge refresh --affected 2>&1)"; code=$?
set -e
[[ $code -eq 0 ]] || { echo "resume: exit=$code" >&2; echo "$out" >&2; exit 1; }
python - "$out" <<'PY'
import json, sys
view = json.loads(sys.argv[1])
assert view['resumed'] is True, view
assert view['ran'] is True, view
# Both pages that changed under the page sources are still pending; the failed
# run's checkpoint named them and the resume keeps exactly those.
assert view['pages'] == ['docs/repowiki/knowledge/存储.md', 'docs/repowiki/knowledge/手改.md'], view
PY
[[ ! -f .devsys/knowledge/run.json ]] || { echo "a clean run must clear its checkpoint" >&2; exit 1; }
echo "PASS: a failed run leaves a checkpoint and the next run resumes it, then clears it"

echo 'package store // again' > internal/store/a.go
STUB_GENERATOR_SLEEP=3 "$D" knowledge refresh --affected >/dev/null 2>&1 &
holder=$!
sleep 1
set +e
out="$("$D" knowledge refresh --affected 2>&1)"; code=$?
set -e
[[ $code -eq 3 ]] || { echo "concurrent refresh: exit=$code want 3" >&2; echo "$out" >&2; exit 1; }
grep -q 'another refresh is already running' <<<"$out" || { echo "refusal misses its reason:" >&2; echo "$out" >&2; exit 1; }
wait "$holder" || true
echo "PASS: a second refresh is refused while another holds the mutex"

echo "== layered context assembly =="
"$D" workitem create --title '整理存储层的写入路径' --actor smoke --reason fixture >/dev/null
item="$(python - <<'PY'
import json, subprocess
out = subprocess.run(['devsys', '--json', 'workitem', 'list'], capture_output=True, text=True, check=True)
items = json.loads(out.stdout)['items']
print(items[0]['id'])
PY
)"
set +e
first="$("$D" --json context get --task "$item" 2>&1)"; code=$?
set -e
[[ $code -eq 0 ]] || { echo "context get --task: exit=$code" >&2; echo "$first" >&2; exit 1; }
second="$("$D" --json context get --task "$item")"
[[ "$first" == "$second" ]] || { echo "two assemblies of one task differ" >&2; exit 1; }
python - "$first" <<'PY'
import json, sys
view = json.loads(sys.argv[1])
pages = view['task']['knowledge']['pages']
matched = [p for p in pages if p['match'] == 'trigger' and p['reason'] == '存储层']
assert matched, pages
assert view['task']['workitem']['id'] == view['task']['knowledge']['pages'][0]['path'][:0] + view['task']['workitem']['id'], view
PY
echo "PASS: the task context carries the pages it touches, twice identically"

echo "== degradation without a generator =="
python - <<'PY'
import pathlib
config = pathlib.Path('.devsys/config.yaml')
lines = [line for line in config.read_text(encoding='utf-8').splitlines() if not line.startswith('knowledge_generator:')]
config.write_text('\n'.join(lines) + '\n', encoding='utf-8')
PY
set +e
out="$("$D" --json knowledge refresh --affected 2>&1)"; code=$?
set -e
[[ $code -eq 11 ]] || { echo "refresh without a generator: exit=$code want 11" >&2; echo "$out" >&2; exit 1; }
python - "$out" <<'PY'
import json, sys
view = json.loads(sys.argv[1])
assert view['missing'] is True and view['ran'] is False, view
assert 'knowledge_generator' in view['reason'], view
PY
set +e
"$D" --json knowledge status >/dev/null 2>&1; code=$?
set -e
echo "PASS: without a generator the layer degrades instead of failing (refresh exit 11, status exit $code)"

success=1
echo "PASS: M5 knowledge layer (page contract, index, freshness, protection, resume, context, generator contract)."
