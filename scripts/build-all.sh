#!/usr/bin/env bash
# build-all.sh — cross-compile the OpenCode Portable launcher for all
# supported platforms. Produces dist/launchers/<name>.
#
# Usage: ./scripts/build-all.sh [version]
#   version defaults to the contents of VERSION (or 0.1.0 if absent).
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${1:-$(cat VERSION 2>/dev/null || echo 0.1.0)}"
GO="${GO:-go}"
LDFLAGS="-s -w -X opencode-portable/internal/version.Version=${VERSION}"

OUT="dist/launchers"
mkdir -p "$OUT"

build() {
  local os="$1" arch="$2" name="$3"
  echo "==> building $os/$arch -> $OUT/$name"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    "$GO" build -trimpath -ldflags "$LDFLAGS" \
    -o "$OUT/$name" ./cmd/opencode-portable
}

build linux   amd64   OpenCodePortable-linux-x64
build linux   arm64   OpenCodePortable-linux-arm64
build windows amd64   OpenCodePortable-windows-x64.exe
build windows arm64   OpenCodePortable-windows-arm64.exe
build darwin  amd64   OpenCodePortable-macos-x64
build darwin  arm64   OpenCodePortable-macos-arm64

echo "==> all builds finished (version $VERSION)"