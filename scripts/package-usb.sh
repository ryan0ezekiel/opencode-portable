#!/usr/bin/env bash
# package-usb.sh — assemble a ready-to-copy USB bundle for one platform.
#
# Usage: ./scripts/package-usb.sh <platform> [staging-dir]
#   platform: linux-x64 | linux-arm64 | windows-x64 | windows-arm64 | macos-x64 | macos-arm64
#   staging-dir: dir produced by fetch-opencode.sh (default: dist/staging/<platform>)
#
# Produces: dist/OpenCodePortable-<platform>/  — copy the CONTENTS of this
# directory onto a USB drive (FAT32/exFAT/NTFS or any POSIX filesystem).
#
# Pre-populated artifacts are optional: the launcher downloads what is
# missing on first run from a writable volume. If the volume is read-only,
# the pre-populated artifacts are what make it usable.
set -euo pipefail

cd "$(dirname "$0")/.."

PLATFORM="${1:?usage: package-usb.sh <platform> [staging-dir]}"
STAGING="${2:-dist/staging/$PLATFORM}"
MANIFEST="internal/app/default.json"

case "$PLATFORM" in
  linux-x64)   KEY=linux/amd64;   LAUNCHER=dist/launchers/OpenCodePortable-linux-x64 ;;
  linux-arm64) KEY=linux/arm64;   LAUNCHER=dist/launchers/OpenCodePortable-linux-arm64 ;;
  windows-x64) KEY=windows/amd64; LAUNCHER=dist/launchers/OpenCodePortable-windows-x64.exe ;;
  windows-arm64) KEY=windows/arm64; LAUNCHER=dist/launchers/OpenCodePortable-windows-arm64.exe ;;
  macos-x64)   KEY=darwin/amd64;  LAUNCHER=dist/launchers/OpenCodePortable-macos-x64 ;;
  macos-arm64) KEY=darwin/arm64;  LAUNCHER=dist/launchers/OpenCodePortable-macos-arm64 ;;
  *) echo "error: unknown platform '$PLATFORM'" >&2; exit 1 ;;
esac

[ -f "$LAUNCHER" ] || { echo "error: launcher missing ($LAUNCHER) — run build-all.sh first" >&2; exit 1; }
[ -f "$MANIFEST" ] || { echo "error: manifest missing ($MANIFEST)" >&2; exit 1; }
[ -d "$STAGING" ] || { echo "error: staging dir missing ($STAGING) — run fetch-opencode.sh first" >&2; exit 1; }
[ -f "README.txt" ] || { echo "error: README.txt missing" >&2; exit 1; }

OUT="dist/OpenCodePortable-$PLATFORM"
rm -rf "$OUT"
mkdir -p "$OUT"

echo "==> assembling $OUT"

# Launcher binary + manifest.
case "$PLATFORM" in
  windows-*) cp "$LAUNCHER" "$OUT/OpenCodePortable.exe" ;;
  *)         cp "$LAUNCHER" "$OUT/OpenCodePortable" ;;
esac
cp "$MANIFEST" "$OUT/manifest.json"

# Standard empty directories (so the USB layout is obvious on first browse).
mkdir -p "$OUT/config" "$OUT/data" "$OUT/cache" "$OUT/logs" "$OUT/downloads"

# Pre-populated runtime (best variant for this platform), if staged.
V=$(jq -r --arg k "$KEY" '.runtimes[$k].variants[0]' "$MANIFEST") \
  || { echo "error: no runtime variants for $KEY in $MANIFEST" >&2; exit 1; }
ART=$(jq -r '.artifact' <<<"$V")
BIN=$(jq -r '.binary' <<<"$V")
VNAME=$(jq -r '.name' <<<"$V")

if [ -f "$STAGING/downloads/$ART" ]; then
  echo "==> extracting runtime ($VNAME) into $OUT/runtimes/"
  mkdir -p "$OUT/runtimes/$KEY/$VNAME"
  case "$ART" in
    *.zip)    unzip -q "$STAGING/downloads/$ART" -d "$OUT/runtimes/$KEY/$VNAME" ;;
    *.tar.gz) tar -xzf "$STAGING/downloads/$ART" -C "$OUT/runtimes/$KEY/$VNAME" ;;
    *) echo "warning: unknown archive type for $ART" >&2 ;;
  esac
  chmod +x "$OUT/runtimes/$KEY/$VNAME/$BIN" 2>/dev/null || true
else
  echo "==> runtime not staged (downloads on first run)"
fi

# Pre-populated tools, if staged.
for TNAME in $(jq -r --arg k "$KEY" '.tools[$k].tools | keys[]' "$MANIFEST"); do
  T=$(jq -r --arg k "$KEY" --arg n "$TNAME" '.tools[$k].tools[$n]' "$MANIFEST")
  TART=$(jq -r '.artifact' <<<"$T")
  if [ -f "$STAGING/tools-downloads/$TART" ]; then
    echo "==> extracting tool ($TNAME) into $OUT/tools/"
    TDEST="$OUT/tools/$KEY/$TNAME"
    mkdir -p "$TDEST"
    case "$TART" in
      *.zip)    unzip -q "$STAGING/tools-downloads/$TART" -d "$TDEST" ;;
      *.tar.gz) tar -xzf "$STAGING/tools-downloads/$TART" -C "$TDEST" ;;
    esac
    # The layout on disk mirrors the manifest fields exactly:
    # tools/<os>/<arch>/<tool>/<root_dir>/<bin_dir>/<binary>.
    # Nothing is flattened; the launcher resolves paths from the manifest.
    chmod +x "$TDEST/$(jq -r '.binary' <<<"$T")" 2>/dev/null || true
  fi
done

cp README.txt "$OUT/README.txt"

echo "==> bundle ready: $OUT"
echo "    copy the contents of this directory onto a USB drive"