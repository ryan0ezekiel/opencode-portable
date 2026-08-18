#!/usr/bin/env bash
# fetch-opencode.sh — download OpenCode CLI artifacts + helper tools for a
# target platform from official releases, verifying SHA-256 digests against
# the portable manifest. Produces a staging directory ready for packaging.
#
# Usage: ./scripts/fetch-opencode.sh <platform> <dest-dir> [manifest.json]
#   platform: linux-x64 | linux-arm64 | windows-x64 | windows-arm64 | macos-x64 | macos-arm64
#
# The manifest entry for the platform is read to determine exact URLs,
# artifact names and expected digests; nothing is downloaded blind.
set -euo pipefail

cd "$(dirname "$0")/.."

PLATFORM="${1:?usage: fetch-opencode.sh <platform> <dest-dir> [manifest.json]}"
DEST="${2:?usage: fetch-opencode.sh <platform> <dest-dir> [manifest.json]}"
MANIFEST="${3:-internal/app/default.json}"

command -v jq >/dev/null || { echo "error: jq is required" >&2; exit 1; }

# Map script platform names to manifest os/arch keys.
case "$PLATFORM" in
  linux-x64)   KEY=linux/amd64 ;;
  linux-arm64) KEY=linux/arm64 ;;
  windows-x64) KEY=windows/amd64 ;;
  windows-arm64) KEY=windows/arm64 ;;
  macos-x64)   KEY=darwin/amd64 ;;
  macos-arm64) KEY=darwin/arm64 ;;
  *) echo "error: unknown platform '$PLATFORM'" >&2; exit 1 ;;
esac

jq -e --arg k "$KEY" '.runtimes[$k]' "$MANIFEST" >/dev/null \
  || { echo "error: no runtime for $KEY in $MANIFEST" >&2; exit 1; }

mkdir -p "$DEST/downloads" "$DEST/tools-downloads"

fetch() {
  local what="$1" url="$2" sha="$3" path="$4"
  if [ -f "$path" ]; then
    if echo "$sha  $path" | sha256sum -c - >/dev/null 2>&1; then
      echo "    $what already present (digest OK)"
      return
    fi
    echo "    $what present but digest mismatch, re-downloading..."
    rm -f "$path"
  fi
  curl -fL --retry 3 --connect-timeout 30 --max-time 1200 -o "$path" "$url"
  echo "$sha  $path" | sha256sum -c - >/dev/null \
    || { echo "error: downloaded $what digest mismatch" >&2; exit 1; }
  echo "    $what fetched and verified"
}

# Runtime variants: fetch the first (best) variant only; the launcher can
# fetch the fallbacks itself on demand.
VARIANT=$(jq -r --arg k "$KEY" '.runtimes[$k].variants[0]' "$MANIFEST")
VNAME=$(jq -r '.name' <<<"$VARIANT")
echo "==> $KEY: runtime '$VNAME'"
fetch "runtime $VNAME" \
  "$(jq -r '.url' <<<"$VARIANT")" \
  "$(jq -r '.sha256' <<<"$VARIANT")" \
  "$DEST/downloads/$(jq -r '.artifact' <<<"$VARIANT")"

# Helper tools (ripgrep everywhere, git/MinGit on Windows).
TOOL_NAMES=$(jq -r --arg k "$KEY" '.tools[$k].tools | keys[]' "$MANIFEST")
for TNAME in $TOOL_NAMES; do
  T=$(jq -r --arg k "$KEY" --arg n "$TNAME" '.tools[$k].tools[$n]' "$MANIFEST")
  echo "==> $KEY: tool '$TNAME'"
  fetch "tool $TNAME" \
    "$(jq -r '.url' <<<"$T")" \
    "$(jq -r '.sha256' <<<"$T")" \
    "$DEST/tools-downloads/$(jq -r '.artifact' <<<"$T")"
done

echo "==> fetch complete: $DEST"