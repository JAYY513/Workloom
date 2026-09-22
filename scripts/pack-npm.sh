#!/usr/bin/env bash
# pack-npm.sh: assemble npm tarballs from already-built workloom binaries.
# Does not download. Does not publish.
# Usage: scripts/pack-npm.sh --version <vX.Y.Z> --dist <dir> [--out <dir>]
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
version=""
dist=""
out=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) version="${2:-}"; shift 2 ;;
    --dist) dist="${2:-}"; shift 2 ;;
    --out) out="${2:-}"; shift 2 ;;
    *) echo "usage: $0 --version <vX.Y.Z> --dist <dir> [--out <dir>]" >&2; exit 2 ;;
  esac
done
if [[ -z "$version" || -z "$dist" ]]; then
  echo "usage: $0 --version <vX.Y.Z> --dist <dir> [--out <dir>]" >&2
  exit 2
fi
npm_version="${version#v}"
if ! [[ "$npm_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "npm version must be X.Y.Z (got ${version}); this script does not publish" >&2
  exit 2
fi
command -v node >/dev/null || { echo "node is required to pack; this script does not download binaries" >&2; exit 127; }
command -v npm >/dev/null || { echo "npm is required to pack; this script does not download binaries" >&2; exit 127; }
if [[ -z "$out" ]]; then
  out="${dist}/npm"
fi
mkdir -p -- "$out"
out="$(cd -- "$out" && pwd)"
dist="$(cd -- "$dist" && pwd)"

stage="$(mktemp -d)"
trap 'rm -rf -- "$stage"' EXIT

stamp() {
  FILE="$1" VERSION="$npm_version" node <<'NODE'
const fs = require("fs");
const file = process.env.FILE;
const version = process.env.VERSION;
const doc = JSON.parse(fs.readFileSync(file, "utf8"));
doc.version = version;
if (doc.optionalDependencies) {
  for (const key of Object.keys(doc.optionalDependencies)) {
    doc.optionalDependencies[key] = version;
  }
}
if (doc.scripts && doc.scripts.postinstall) {
  console.error(file + " has a postinstall script; refusing to pack");
  process.exit(1);
}
fs.writeFileSync(file, JSON.stringify(doc, null, 2) + "\n");
NODE
}

mkdir -p -- "$stage/workloom"
cp -R -- "$repo_root/npm/workloom/." "$stage/workloom/"
stamp "$stage/workloom/package.json"

while read -r asset npm_id bin_name; do
  [[ -z "$asset" ]] && continue
  src="$dist/$asset"
  if [[ ! -f "$src" ]]; then
    echo "missing ${src}; build the release matrix first (scripts/build-release.sh). This script does not download." >&2
    exit 1
  fi
  dest="$stage/$npm_id"
  mkdir -p -- "$dest"
  cp -- "$repo_root/npm/platforms/$npm_id/package.json" "$dest/package.json"
  cp -- "$src" "$dest/$bin_name"
  if [[ "$bin_name" != *.exe ]]; then
    chmod +x -- "$dest/$bin_name"
  fi
  stamp "$dest/package.json"
  (cd -- "$dest" && npm pack --ignore-scripts --pack-destination "$out" >/dev/null)
  echo "packed $npm_id"
done <<'MATRIX'
workloom-darwin-arm64 darwin-arm64 workloom
workloom-darwin-amd64 darwin-x64 workloom
workloom-linux-arm64 linux-arm64 workloom
workloom-linux-amd64 linux-x64 workloom
workloom-windows-arm64.exe win32-arm64 workloom.exe
workloom-windows-amd64.exe win32-x64 workloom.exe
MATRIX

(cd -- "$stage/workloom" && npm pack --ignore-scripts --pack-destination "$out" >/dev/null)
echo "packed @kaki317/workloom ${npm_version} into ${out}"
