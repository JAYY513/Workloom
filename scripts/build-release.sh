#!/usr/bin/env bash
# build-release.sh: matrix-build workloom release binaries + checksums.txt.
# The devsys compatibility alias ships as install-time copy, not a separate asset.
# Repeatable. Requires Go and Git. Windows: run with Git Bash.
# Usage: scripts/build-release.sh [--version <v>] [--out dist]
#   --version defaults to `git describe --tags --always --dirty`.
set -euo pipefail
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
case "$(uname -s)" in
  MINGW*|MSYS*) export PATH="/c/Program Files/Go/bin:$PATH" ;;
esac
command -v go >/dev/null || { echo 'Go is required; on Windows use Git Bash.' >&2; exit 127; }
version=""
out="dist"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) version="${2:-}"; shift 2 ;;
    --out) out="${2:-}"; shift 2 ;;
    *) echo "usage: $0 [--version <v>] [--out <dir>]" >&2; exit 2 ;;
  esac
done
if [[ -z "$version" ]]; then
  version="$(git -C "$repo_root" describe --tags --always --dirty)"
fi
commit="$(git -C "$repo_root" rev-parse --short HEAD)"
ldflags="-s -w -X github.com/JAYY513/Workloom/internal/version.Version=${version} -X github.com/JAYY513/Workloom/internal/version.Commit=${commit}"
mkdir -p -- "$out"
# The unstripped build trips some endpoint protection (see smoke-m8.sh); -s -w stays.
while IFS=/ read -r goos goarch; do
  [[ -z "$goos" ]] && continue
  name="workloom-${goos}-${goarch}"
  [[ "$goos" == "windows" ]] && name="${name}.exe"
  (cd "$repo_root" && GOOS="$goos" GOARCH="$goarch" GOFLAGS=-mod=vendor go build -ldflags "$ldflags" -o "$out/$name" ./cmd/workloom)
  echo "built $out/$name"
done <<'MATRIX'
windows/amd64
windows/arm64
linux/amd64
linux/arm64
darwin/amd64
darwin/arm64
MATRIX
# The install scripts ship as release assets too: the "paste this prompt to
# your agent" flow downloads them straight from the release page, and manual
# users skip even the shallow clone (#347).
cp -- "$repo_root/scripts/install.sh" "$repo_root/scripts/install.ps1" "$out/"
# Text mode is forced: Windows Git Bash's sha256sum defaults to the binary
# marker "*<file>", which the strict check below (and the install scripts)
# reject — local builds must produce the same contract as CI.
(cd "$out" && sha256sum -t workloom-* install.sh install.ps1 > checksums.txt)
# Keep the file in GNU text mode: "<sha256>  <file>". macOS `shasum` writes
# "<sha256> *<file>", which `sha256sum -c` accepts but the install scripts
# (and this repo's release check) do not want — v0.1.0 shipped such a file.
if grep -Eq '^[0-9a-f]{64} \*' "$out/checksums.txt"; then
  echo "$out/checksums.txt is not in GNU text mode; regenerate it with sha256sum" >&2
  exit 1
fi
echo "wrote $out/checksums.txt ($(wc -l < "$out/checksums.txt") files)"
