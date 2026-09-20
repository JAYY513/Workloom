#!/usr/bin/env bash
# install.sh: download a devsys release binary, verify sha256, install to --prefix.
# Falls back to `git clone --branch <tag> + go build` when download fails and Go exists.
# Respects HTTPS_PROXY/HTTP_PROXY via curl/wget natively.
# Usage: scripts/install.sh --tag <tag> [--repo JAYY513/Workloom] [--prefix ~/.local/bin]
set -euo pipefail
tag=""; repo="JAYY513/Workloom"; prefix="$HOME/.local/bin"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag) tag="${2:-}"; shift 2 ;;
    --repo) repo="${2:-}"; shift 2 ;;
    --prefix) prefix="${2:-}"; shift 2 ;;
    *) echo "usage: $0 --tag <tag> [--repo <owner/name>] [--prefix <dir>]" >&2; exit 2 ;;
  esac
done
[[ -z "$tag" ]] && { echo '--tag is required' >&2; exit 2; }
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux*) os="linux" ;; darwin*) os="darwin" ;;
  mingw*|msys*|cygwin*) os="windows" ;;
  *) echo "unsupported os: $os" >&2; exit 2 ;;
esac
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;; aarch64|arm64) arch="arm64" ;;
  *) echo "unsupported arch: $arch" >&2; exit 2 ;;
esac
asset="devsys-${os}-${arch}"
[[ "$os" == "windows" ]] && asset="${asset}.exe"
base="https://github.com/${repo}/releases/download/${tag}"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/devsys-install-XXXXXX")"
trap 'rm -rf -- "$tmp"' EXIT
download() { # download <url> <dest>
  if command -v curl >/dev/null; then curl -fsSL --retry 2 -o "$2" "$1"
  elif command -v wget >/dev/null; then wget -q -O "$2" "$1"
  else echo 'need curl or wget' >&2; return 1; fi
}
# A private repository's release assets need authentication: an anonymous
# curl/wget only sees a 404. `gh`, when installed and logged in, can fetch
# them, so try it first and fall back to the anonymous download otherwise.
gh_download() { # gh_download <asset-name> <dest>
  command -v gh >/dev/null || return 1
  gh auth status >/dev/null 2>&1 || return 1
  gh release download "$tag" --repo "$repo" --pattern "$1" --output "$2" --clobber >/dev/null 2>&1
}
fallback_build() {
  command -v go >/dev/null || { echo 'download failed and no Go for fallback build' >&2; return 1; }
  command -v git >/dev/null || { echo 'download failed and no git for fallback build' >&2; return 1; }
  echo "download failed; falling back to git clone + go build"
  git -c advice.detachedHead=false clone -q --branch "$tag" --depth 1 "https://github.com/${repo}.git" "$tmp/src"
  (cd "$tmp/src" && GOPROXY=off GOFLAGS=-mod=vendor go build -o "$tmp/$asset" ./cmd/devsys)
}
fetched=0
if gh_download "$asset" "$tmp/$asset" && gh_download "checksums.txt" "$tmp/checksums.txt"; then
  fetched=1
elif download "$base/$asset" "$tmp/$asset" && download "$base/checksums.txt" "$tmp/checksums.txt"; then
  fetched=1
fi
if [ "$fetched" != "1" ]; then
  fallback_build || exit 1
else
  # tr -d '\r': tolerate CRLF checksums.txt from older PS-built releases.
  # '[[:space:]]\*?' tolerates the shasum/MSYS binary marker (`<hash> *<file>`),
  # which the v0.1.0 release used; sha256sum -c accepts both dialects.
  verified=0
  if command -v sha256sum >/dev/null; then
    tr -d '\r' < "$tmp/checksums.txt" | grep -E "[[:space:]]\*?$asset\$" | (cd "$tmp" && sha256sum -c -) && verified=1
  elif command -v shasum >/dev/null; then
    tr -d '\r' < "$tmp/checksums.txt" | grep -E "[[:space:]]\*?$asset\$" | (cd "$tmp" && shasum -a 256 -c -) && verified=1
  else
    echo 'need sha256sum or shasum' >&2
  fi
  [ "$verified" = "1" ] || { echo 'checksum mismatch or no checker' >&2; fallback_build || exit 1; }
fi
mkdir -p -- "$prefix"
dest="$prefix/devsys"
[[ "$os" == "windows" ]] && dest="$prefix/devsys.exe"
cp -- "$tmp/$asset" "$dest"
[[ "$os" == "windows" ]] || chmod +x "$dest"
"$dest" --version
