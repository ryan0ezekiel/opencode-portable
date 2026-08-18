#!/usr/bin/env bash
# verify-artifacts.sh — verify every artifact referenced by a portable
# manifest against its recorded SHA-256 digest.
#
# Usage: ./scripts/verify-artifacts.sh <dir> [manifest.json] [platform]
#   dir: a directory tree containing downloads/ and tools-downloads/
#   platform (optional): linux-x64 | linux-arm64 | windows-x64 |
#     windows-arm64 | macos-x64 | macos-arm64 — verify only this platform.
set -euo pipefail

cd "$(dirname "$0")/.."

DIR="${1:?usage: verify-artifacts.sh <dir> [manifest.json] [platform]}"
MANIFEST="${2:-internal/app/default.json}"
PLATFORM="${3:-}"

command -v jq >/dev/null || { echo "error: jq is required" >&2; exit 1; }

KEY_FILTER=""
if [ -n "$PLATFORM" ]; then
  case "$PLATFORM" in
    linux-x64)   KEY_FILTER='select(. == "linux/amd64")' ;;
    linux-arm64) KEY_FILTER='select(. == "linux/arm64")' ;;
    windows-x64) KEY_FILTER='select(. == "windows/amd64")' ;;
    windows-arm64) KEY_FILTER='select(. == "windows/arm64")' ;;
    macos-x64)   KEY_FILTER='select(. == "darwin/amd64")' ;;
    macos-arm64) KEY_FILTER='select(. == "darwin/arm64")' ;;
    *) echo "error: unknown platform '$PLATFORM'" >&2; exit 1 ;;
  esac
fi

FAIL=0

check() {
  local name="$1" path="$2" sha="$3"
  if [ ! -f "$path" ]; then
    echo "MISSING $name ($path)"
    FAIL=1
    return
  fi
  if echo "$sha  $path" | sha256sum -c - >/dev/null 2>&1; then
    echo "OK      $name"
  else
    echo "FAIL    $name (digest mismatch)"
    FAIL=1
  fi
}

# Runtimes: the first (best) variant per platform is what fetch stages.
for KEY in $(jq -r ".runtimes | keys[] | $KEY_FILTER" "$MANIFEST"); do
  V=$(jq -r --arg k "$KEY" '.runtimes[$k].variants[0]' "$MANIFEST")
  check "runtime $KEY $(jq -r '.name' <<<"$V")" \
    "$DIR/downloads/$(jq -r '.artifact' <<<"$V")" \
    "$(jq -r '.sha256' <<<"$V")"
done

# Tools.
for KEY in $(jq -r ".tools | keys[] | $KEY_FILTER" "$MANIFEST"); do
  for TNAME in $(jq -r --arg k "$KEY" '.tools[$k].tools | keys[]' "$MANIFEST"); do
    T=$(jq -r --arg k "$KEY" --arg n "$TNAME" '.tools[$k].tools[$n]' "$MANIFEST")
    check "tool $KEY/$TNAME" \
      "$DIR/tools-downloads/$(jq -r '.artifact' <<<"$T")" \
      "$(jq -r '.sha256' <<<"$T")"
  done
done

if [ "$FAIL" -ne 0 ]; then
  echo "==> verification FAILED"
  exit 1
fi
echo "==> all artifacts verified"