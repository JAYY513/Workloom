#!/usr/bin/env bash
# M1.7: isolated, repeatable lifecycle. Requires Go and Git.
# Windows: run with Git Bash, not a WSL shell without Go installed.
set -euo pipefail
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
case "$(uname -s)" in
  MINGW*|MSYS*) export PATH="/c/Program Files/Go/bin:$PATH" ;;
esac
command -v go >/dev/null || { echo 'Go is required; on Windows use Git Bash or install Go inside WSL.' >&2; exit 127; }
keep=0
case "${1:-}" in
  --keep) keep=1 ;;
  '') ;;
  *) echo 'usage: smoke-m1.sh [--keep]' >&2; exit 2 ;;
esac
workspace="$(mktemp -d "${TMPDIR:-/tmp}/smoke-m1-XXXXXX")"
project="$workspace/demo-project"
success=0
cleanup() {
  if [[ "$success" == 1 && "$keep" == 1 ]]; then
    rm -f -- "$workspace/workloom.exe" "$workspace/smoke-helper.exe"
    rm -rf -- "$workspace/config"
    printf 'KEPT_PROJECT=%s\n' "$project"
  else
    rm -rf -- "$workspace"
  fi
}
trap cleanup EXIT
mkdir -p -- "$project"
export DEVSYS_CONFIG_DIR="$workspace/config"
(cd "$repo_root" && go build -o "$workspace/workloom.exe" ./cmd/workloom && go build -o "$workspace/smoke-helper.exe" ./scripts/smoke-m1-helper.go)
git init -q "$project"
(
  cd "$project"
  "$workspace/workloom.exe" init
  "$workspace/smoke-helper.exe" "$project"
  "$workspace/workloom.exe" --json config check
  result="$("$workspace/workloom.exe" --json search M1)"
  printf '%s\n' "$result"
  [[ "$result" == *'"path":"workitems/WLM-1.yaml"'* ]] || { echo 'search did not return the work item' >&2; exit 1; }
  git -c core.autocrlf=false add -- .devsys
  git -c core.quotepath=false status --porcelain
  "$workspace/smoke-helper.exe" --verify "$project"
)
success=1
echo 'PASS: M1 create -> record -> complete; all tracked state is UTF-8 text.'
