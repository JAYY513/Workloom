#!/usr/bin/env bash
# publish-npm.sh: publish already-packed workloom tarballs to the npm registry.
# Does not build, does not pack, does not download.
# Platform packages are published before the wrapper. A version already on the
# registry is skipped, so re-running after a partial failure is safe.
#
# Usage:
#   scripts/publish-npm.sh --dir <tgz dir> [--version vX.Y.Z]
#                          [--platforms win32-x64] [--dry-run] [--otp <code>]
#
# The default platform set is the published one: Windows x64 only. The other
# platform tarballs stay attached to the GitHub Release until they are published
# too; the wrapper pins them all at its own version, so publishing them later
# makes the already-published wrapper usable on those platforms.
set -euo pipefail

usage="usage: $0 --dir <tgz dir> [--version vX.Y.Z] [--platforms win32-x64] [--dry-run] [--otp <code>]"

dir=""
version=""
platforms="win32-x64"
dry_run=""
otp=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir) dir="${2:-}"; shift 2 ;;
    --version) version="${2:-}"; shift 2 ;;
    --platforms) platforms="${2:-}"; shift 2 ;;
    --dry-run) dry_run="yes"; shift ;;
    --otp) otp="${2:-}"; shift 2 ;;
    *) echo "$usage" >&2; exit 2 ;;
  esac
done
if [[ -z "$dir" ]]; then
  echo "$usage" >&2
  exit 2
fi
if [[ ! -d "$dir" ]]; then
  echo "no such tgz directory: $dir" >&2
  exit 1
fi
dir="$(cd -- "$dir" && pwd)"

command -v npm >/dev/null || { echo "npm is required to publish; this script does not build" >&2; exit 127; }

if [[ -z "$version" ]]; then
  set -- "$dir"/kaki317-workloom-[0-9]*.tgz
  if [[ ! -f "$1" ]]; then
    echo "cannot derive the version: no kaki317-workloom-<X.Y.Z>.tgz in $dir" >&2
    exit 1
  fi
  base="$(basename -- "$1")"
  base="${base#kaki317-workloom-}"
  version="v${base%.tgz}"
fi
npm_version="${version#v}"
if ! [[ "$npm_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "version must be X.Y.Z (got ${version})" >&2
  exit 2
fi

# Publish order: platform packages first, the wrapper last.
pkgs=()
IFS=',' read -r -a wanted <<<"$platforms"
for platform in "${wanted[@]}"; do
  platform="${platform//[[:space:]]/}"
  [[ -z "$platform" ]] && continue
  pkgs+=("@kaki317/workloom-${platform}" "kaki317-workloom-${platform}-${npm_version}.tgz")
done
pkgs+=("@kaki317/workloom" "kaki317-workloom-${npm_version}.tgz")

published=0
skipped=0
for ((i = 0; i < ${#pkgs[@]}; i += 2)); do
  name="${pkgs[i]}"
  file="${dir}/${pkgs[i + 1]}"
  if [[ ! -f "$file" ]]; then
    echo "missing ${file}" >&2
    echo "pack it first (scripts/pack-npm.sh --version ${version} --dist <dist dir>) or download the Release asset. This script does not download." >&2
    exit 1
  fi
  if [[ -z "$dry_run" ]]; then
    if npm view "${name}@${npm_version}" version >/dev/null 2>&1; then
      echo "skip ${name}@${npm_version}: already on the registry"
      skipped=$((skipped + 1))
      continue
    fi
  fi
  args=(publish "$file" --access public --ignore-scripts)
  [[ -n "$otp" ]] && args+=(--otp "$otp")
  [[ -n "$dry_run" ]] && args+=(--dry-run)
  echo "publish ${name}@${npm_version}"
  npm "${args[@]}"
  published=$((published + 1))
done

if [[ -n "$dry_run" ]]; then
  echo "dry run: ${published} package(s) would be published, ${skipped} skipped"
else
  echo "published ${published}, skipped ${skipped} (already present)"
  echo "install with: npm install -g @kaki317/workloom"
fi
