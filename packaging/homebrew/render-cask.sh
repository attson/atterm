#!/usr/bin/env bash
# packaging/homebrew/render-cask.sh — render atterm.rb.tmpl into a
# concrete Homebrew cask for one release, substituting the version and the
# two darwin-arch sha256 checksums pulled from that release's SHA256SUMS.
# Prints the rendered cask to stdout.
#
# A missing architecture is a hard failure, not a cask with an empty
# sha256: an empty sha256 is syntactically valid Ruby and would only break
# at `brew install` time, on a user's machine, long after CI passed.
set -euo pipefail

usage() {
  echo "usage: render-cask.sh <version> <sha256sums-file>" >&2
  echo "  <version>          release tag, e.g. v0.4.20 (v prefix optional)" >&2
  echo "  <sha256sums-file>  path to the release's SHA256SUMS" >&2
}

if [ "$#" -ne 2 ]; then
  usage
  exit 1
fi

version="$1"
sums_file="$2"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
template="$script_dir/atterm.rb.tmpl"

if [ ! -f "$sums_file" ]; then
  echo "ERROR: SHA256SUMS file not found: $sums_file" >&2
  exit 1
fi

if [ ! -f "$template" ]; then
  echo "ERROR: template not found: $template" >&2
  exit 1
fi

# The cask's `version` is interpolated into the url as v#{version} (the
# template hardcodes the "v"), so the value stored here must NOT carry one
# itself, or the rendered url would read ".../vv0.4.20/...".
version_no_v="${version#v}"

# SHA256SUMS is produced by sign-release-checksums.go in sha256sum text-mode
# format: "<hex>  <filename>" (two spaces). Match the filename in field 2
# exactly, so e.g. the arm64 .dmg line can't accidentally satisfy the zip
# lookup.
extract_sha() {
  local filename="$1"
  awk -v f="$filename" '$2 == f { print $1; exit }' "$sums_file"
}

sha_arm64="$(extract_sha "AT-Term-darwin-arm64.zip")"
sha_amd64="$(extract_sha "AT-Term-darwin-amd64.zip")"

missing=()
if [ -z "$sha_arm64" ]; then
  missing+=("AT-Term-darwin-arm64.zip")
fi
if [ -z "$sha_amd64" ]; then
  missing+=("AT-Term-darwin-amd64.zip")
fi

if [ "${#missing[@]}" -gt 0 ]; then
  echo "ERROR: $sums_file is missing checksum(s) for: ${missing[*]}" >&2
  exit 1
fi

sed \
  -e "s/__VERSION__/${version_no_v}/g" \
  -e "s/__SHA_ARM64__/${sha_arm64}/g" \
  -e "s/__SHA_AMD64__/${sha_amd64}/g" \
  "$template"
