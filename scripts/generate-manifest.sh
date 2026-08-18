#!/usr/bin/env bash
#
# generate-manifest.sh — Build the OpenCode Portable manifest from official
# release sources.
#
# Sources (official only):
#   - OpenCode CLI:    https://github.com/anomalyco/opencode/releases
#   - ripgrep:         https://github.com/BurntSushi/ripgrep/releases
#   - MinGit (Windows): https://github.com/git-for-windows/git/releases
#
# Usage:
#   scripts/generate-manifest.sh [--opencode-version vX.Y.Z] [--out FILE]
#
# The manifest is written to internal/app/default.json (embedded into the
# bootstrapper at build time). The packaging script also places a copy at
# the USB root as manifest.json.
#
# Security: OpenCode artifact digests come from the GitHub API's published
# sha256. Tool digests come from the API when published; otherwise the tool
# archive is downloaded and hashed locally and the digest is recorded in the
# manifest at packaging time (trust-on-first-use, recorded for all later
# runs).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${OUT:-$REPO_ROOT/internal/app/default.json}"
OPENCODE_REPO="${OPENCODE_REPO:-anomalyco/opencode}"
RIPGREP_REPO="${RIPGREP_REPO:-BurntSushi/ripgrep}"
GITWIN_REPO="${GITWIN_REPO:-git-for-windows/git}"
API_BASE="${API_BASE:-https://api.github.com/repos}"

TMPDIR_WORK="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_WORK"' EXIT

fetch_json() {
    local url="$1"
    curl -fsSL --connect-timeout 30 --max-time 120 --retry 3 \
        -H "Accept: application/vnd.github+json" -H "User-Agent: OpenCodePortable-manifest" "$url"
}

latest_tag() {
    local repo="$1"
    fetch_json "$API_BASE/$repo/releases/latest" | python3 -c 'import json,sys; print(json.load(sys.stdin)["tag_name"])'
}

# cache_release <repo> <tag> <name> — fetches the release JSON once and
# caches it; subsequent queries read from the cache (avoids rate limits).
cache_release() {
    local repo="$1" tag="$2" name="$3"
    if [[ ! -f "$TMPDIR_WORK/$name.json" ]]; then
        fetch_json "$API_BASE/$repo/releases/tags/$tag" > "$TMPDIR_WORK/$name.json"
    fi
}

# asset_field <name> <asset> <field> — reads a field from a cached release.
asset_field() {
    local cache="$1" asset="$2" field="$3"
    python3 -c '
import json, sys
cache, asset, field = sys.argv[1], sys.argv[2], sys.argv[3]
try:
    rel = json.load(open(cache))
except Exception:
    sys.exit(0)
for a in rel.get("assets", []):
    if a["name"] == asset:
        if field == "sha256":
            d = a.get("digest", "")
            if d.startswith("sha256:"):
                print(d[len("sha256:"):].strip().lower())
            sys.exit(0)
        v = a.get(field, "")
        if v is not None:
            print(v)
        sys.exit(0)
sys.exit(0)
' "$cache" "$asset" "$field"
}

asset_sha() {
    local cache="$1" asset="$2"
    asset_field "$cache" "$asset" "sha256"
}

asset_size() {
    local cache="$1" asset="$2"
    asset_field "$cache" "$asset" "size"
}

OPENCODE_TAG=""
if [[ "${1:-}" == "--opencode-version" && -n "${2:-}" ]]; then
    OPENCODE_TAG="$2"
else
    OPENCODE_TAG="$(latest_tag "$OPENCODE_REPO")"
fi
OPENCODE_VERSION="${OPENCODE_TAG#v}"

echo "OpenCode release: $OPENCODE_TAG"
RIPGREP_TAG="$(latest_tag "$RIPGREP_REPO")"
echo "ripgrep release:  $RIPGREP_TAG"
GITWIN_TAG="$(latest_tag "$GITWIN_REPO")"
echo "MinGit release:   $GITWIN_TAG"
# MinGit asset names drop the ".windows.N" suffix, e.g. tag
# "v2.55.0.windows.4" -> asset version "2.55.0.4".
GITWIN_ASSET_VER="${GITWIN_TAG#v}"
GITWIN_ASSET_VER="${GITWIN_ASSET_VER/.windows./.}"

# Fetch each release JSON exactly once and cache it.
cache_release "$OPENCODE_REPO" "$OPENCODE_TAG" "opencode"
cache_release "$RIPGREP_REPO" "$RIPGREP_TAG" "ripgrep"
cache_release "$GITWIN_REPO" "$GITWIN_TAG" "gitwin"
OPENCODE_CACHE="$TMPDIR_WORK/opencode.json"
RIPGREP_CACHE="$TMPDIR_WORK/ripgrep.json"
GITWIN_CACHE="$TMPDIR_WORK/gitwin.json"

# ---------------------------------------------------------------------------
# Runtime variant definitions: artifact naming follows the official release
# conventions. requires hints are conservative; the execution probe is the
# final authority.
# ---------------------------------------------------------------------------

runtime_variant() { # <platform> <name> <artifact> <libc> <minlibc> <cpu> <note>
    local platform="$1" name="$2" artifact="$3" libc="$4" minlibc="$5" cpu="$6" note="$7"
    local sha size url
    sha="$(asset_sha "$OPENCODE_CACHE" "$artifact")"
    size="$(asset_size "$OPENCODE_CACHE" "$artifact")"
    if [[ -z "$sha" ]]; then
        echo "  !! $platform/$name: artifact $artifact has no published sha256 digest — skipping" >&2
        return 1
    fi
    url="https://github.com/$OPENCODE_REPO/releases/download/$OPENCODE_TAG/$artifact"
    local archive binary
    case "$artifact" in
        *.zip)   archive="zip" ;;
        *.tar.gz) archive="tar.gz" ;;
    esac
    case "$platform" in
        windows/*) binary="opencode.exe" ;;
        *)         binary="opencode" ;;
    esac
    {
        echo "        {"
        echo "          \"name\": \"$name\","
        echo "          \"artifact\": \"$artifact\","
        echo "          \"url\": \"$url\","
        echo "          \"sha256\": \"$sha\","
        echo "          \"size\": $size,"
        echo "          \"archive\": \"$archive\","
    # Assemble requires fields with explicit comma handling: any subset of
    # libc/min_libc/cpu/note may be present, and the JSON must stay valid.
    local req_fields=()
    [[ -n "$libc" ]]    && req_fields+=("\"libc\": \"$libc\"")
    [[ -n "$minlibc" ]] && req_fields+=("\"min_libc\": \"$minlibc\"")
    [[ -n "$cpu" ]]     && req_fields+=("\"cpu\": [\"$cpu\"]")
    [[ -n "$note" ]]    && req_fields+=("\"note\": \"$note\"")
    if [[ ${#req_fields[@]} -gt 0 ]]; then
        echo "          \"binary\": \"$binary\","
        echo "          \"requires\": {"
        local first=1
        for f in "${req_fields[@]}"; do
            if [[ $first -eq 1 ]]; then
                echo "            $f"
                first=0
            else
                echo "            ,$f"
            fi
        done
        echo "          }"
    else
        echo "          \"binary\": \"$binary\""
    fi
        echo "        },"
    }
}

write_runtimes() {
    local ok=1

    echo "      \"windows/amd64\": { \"variants\": ["
    runtime_variant "windows/amd64" "native"   "opencode-windows-x64.zip"           "" "" "avx2" "modern CPU builds" || ok=0
    runtime_variant "windows/amd64" "baseline" "opencode-windows-x64-baseline.zip"  "" "" ""     "older CPU builds" || ok=0
    echo "      ] },"

    echo "      \"windows/arm64\": { \"variants\": ["
    runtime_variant "windows/arm64" "native"   "opencode-windows-arm64.zip"         "" "" "" "arm64 builds" || ok=0
    echo "      ] },"

    echo "      \"linux/amd64\": { \"variants\": ["
    runtime_variant "linux/amd64" "native"        "opencode-linux-x64.tar.gz"              "glibc" "2.17" "avx2" "modern CPU, glibc" || ok=0
    runtime_variant "linux/amd64" "baseline"      "opencode-linux-x64-baseline.tar.gz"     "glibc" "2.17" ""     "older CPU, glibc" || ok=0
    runtime_variant "linux/amd64" "musl"          "opencode-linux-x64-musl.tar.gz"         "musl"  ""     "avx2" "self-contained, any libc" || ok=0
    runtime_variant "linux/amd64" "baseline-musl" "opencode-linux-x64-baseline-musl.tar.gz" "musl" ""     ""     "self-contained, older CPU" || ok=0
    echo "      ] },"

    echo "      \"linux/arm64\": { \"variants\": ["
    runtime_variant "linux/arm64" "native" "opencode-linux-arm64.tar.gz"      "glibc" "2.17" "" "glibc" || ok=0
    runtime_variant "linux/arm64" "musl"   "opencode-linux-arm64-musl.tar.gz" "musl"  ""     "" "self-contained, any libc" || ok=0
    echo "      ] },"

    echo "      \"darwin/amd64\": { \"variants\": ["
    runtime_variant "darwin/amd64" "native"   "opencode-darwin-x64.zip"          "" "" "avx2" "modern Intel CPUs" || ok=0
    runtime_variant "darwin/amd64" "baseline" "opencode-darwin-x64-baseline.zip" "" "" ""     "older Intel CPUs" || ok=0
    echo "      ] },"

    echo "      \"darwin/arm64\": { \"variants\": ["
    runtime_variant "darwin/arm64" "native" "opencode-darwin-arm64.zip" "" "" "" "Apple Silicon" || ok=0
    echo "      ] }"

    if [[ $ok -eq 0 ]]; then return 1; fi
    return 0
}

# ---------------------------------------------------------------------------
# Tools: ripgrep (all platforms), MinGit (Windows).
# ---------------------------------------------------------------------------

tool_entry() { # <platform> <name> <cache> <tag> <artifact> <archive> <rootdir> <bindir> <binary> [executables...]
    local platform="$1" name="$2" cache="$3" tag="$4" artifact="$5" archive="$6" rootdir="$7" bindir="$8" binary="$9"
    shift 9
    local sha size url
    sha="$(asset_sha "$cache" "$artifact")"
    size="$(asset_size "$cache" "$artifact")"
    url="https://github.com/$GITWIN_REPO/releases/download/$tag/$artifact"
    if [[ "$name" == "ripgrep" ]]; then
        url="https://github.com/$RIPGREP_REPO/releases/download/$tag/$artifact"
    fi

    # If the API has no digest, download and hash the artifact locally and
    # record that digest (verified for every later install).
    if [[ -z "$sha" ]]; then
        echo "  !! $platform/$name: no API digest; hashing locally" >&2
        local tmp="$TMPDIR_WORK/$artifact"
        if ! curl -fsSL --connect-timeout 30 --max-time 1200 --retry 3 -o "$tmp" "$url" 2>/dev/null; then
            echo "  !! $platform/$name: cannot download $artifact" >&2
            return 1
        fi
        sha="$(sha256sum "$tmp" | awk '{print $1}')"
        if [[ -z "$size" ]]; then
            size="$(stat -c %s "$tmp")"
        fi
    fi

    {
        echo "          \"$name\": {"
        echo "            \"name\": \"$name\","
        echo "            \"artifact\": \"$artifact\","
        echo "            \"url\": \"$url\","
        echo "            \"sha256\": \"$sha\","
        echo "            \"size\": $size,"
        echo "            \"archive\": \"$archive\","
        [[ -n "$rootdir" ]] && echo "            \"root_dir\": \"$rootdir\","
        [[ -n "$bindir" ]]  && echo "            \"bin_dir\": \"$bindir\","
        echo "            \"binary\": \"$binary\""
        if [[ $# -gt 0 ]]; then
            echo "            ,\"executables\": ["
            local first=1
            for exe in "$@"; do
                if [[ $first -eq 1 ]]; then
                    echo "              \"$exe\""
                    first=0
                else
                    echo "              ,\"$exe\""
                fi
            done
            echo "            ]"
        fi
        echo "          },"
    }
}

write_tools() {
    local ok=1
    local rg="${RIPGREP_TAG#v}"

    echo "      \"linux/amd64\": { \"tools\": {"
    tool_entry "linux/amd64" "ripgrep" "$RIPGREP_CACHE" "$RIPGREP_TAG" "ripgrep-$rg-x86_64-unknown-linux-musl.tar.gz" "tar.gz" "ripgrep-$rg-x86_64-unknown-linux-musl" "" "rg" || ok=0
    echo "      } },"
    echo "      \"linux/arm64\": { \"tools\": {"
    tool_entry "linux/arm64" "ripgrep" "$RIPGREP_CACHE" "$RIPGREP_TAG" "ripgrep-$rg-aarch64-unknown-linux-musl.tar.gz" "tar.gz" "ripgrep-$rg-aarch64-unknown-linux-musl" "" "rg" || ok=0
    echo "      } },"
    echo "      \"darwin/amd64\": { \"tools\": {"
    tool_entry "darwin/amd64" "ripgrep" "$RIPGREP_CACHE" "$RIPGREP_TAG" "ripgrep-$rg-x86_64-apple-darwin.tar.gz" "tar.gz" "ripgrep-$rg-x86_64-apple-darwin" "" "rg" || ok=0
    echo "      } },"
    echo "      \"darwin/arm64\": { \"tools\": {"
    tool_entry "darwin/arm64" "ripgrep" "$RIPGREP_CACHE" "$RIPGREP_TAG" "ripgrep-$rg-aarch64-apple-darwin.tar.gz" "tar.gz" "ripgrep-$rg-aarch64-apple-darwin" "" "rg" || ok=0
    echo "      } },"
    echo "      \"windows/amd64\": { \"tools\": {"
    tool_entry "windows/amd64" "ripgrep" "$RIPGREP_CACHE" "$RIPGREP_TAG" "ripgrep-$rg-x86_64-pc-windows-msvc.zip" "zip" "" "" "rg.exe" || ok=0
    tool_entry "windows/amd64" "git" "$GITWIN_CACHE" "$GITWIN_TAG" "MinGit-${GITWIN_ASSET_VER}-64-bit.zip" "zip" "MinGit-${GITWIN_ASSET_VER}-64-bit" "cmd" "git.exe" || ok=0
    echo "      } },"
    echo "      \"windows/arm64\": { \"tools\": {"
    tool_entry "windows/arm64" "ripgrep" "$RIPGREP_CACHE" "$RIPGREP_TAG" "ripgrep-$rg-aarch64-pc-windows-msvc.zip" "zip" "" "" "rg.exe" || ok=0
    tool_entry "windows/arm64" "git" "$GITWIN_CACHE" "$GITWIN_TAG" "MinGit-${GITWIN_ASSET_VER}-arm64.zip" "zip" "MinGit-${GITWIN_ASSET_VER}-arm64" "cmd" "git.exe" || ok=0
    echo "      } }"
    if [[ $ok -eq 0 ]]; then return 1; fi
    return 0
}

# ---------------------------------------------------------------------------
# Assemble. Failures inside the writers are captured explicitly rather than
# aborting under set -e, so the JSON can still be assembled and validated.
# ---------------------------------------------------------------------------

RUNTIMES_OK=0
TOOLS_OK=0

{
    echo "{"
    echo "  \"schema_version\": 1,"
    echo "  \"source\": \"$OPENCODE_REPO\","
    echo "  \"opencode_version\": \"$OPENCODE_TAG\","
    echo "  \"runtimes\": {"
    write_runtimes
    RUNTIMES_OK=$?
    echo "  },"
    echo "  \"tools\": {"
    write_tools
    TOOLS_OK=$?
    echo "  }"
    echo "}"
} > "$OUT"

# Remove the trailing commas emitted before closing brackets.
perl -0pi -e 's/        },\n      \] \}/        }\n      ] }/g; s/          },\n      \} \}/          }\n      } }/g' "$OUT"

python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$OUT" || { echo "ERROR: generated manifest is invalid JSON" >&2; exit 1; }
if [[ $RUNTIMES_OK -ne 0 ]]; then
    echo "ERROR: some OpenCode runtime artifacts could not be recorded; the manifest would be incomplete." >&2
    exit 1
fi
if [[ $TOOLS_OK -ne 0 ]]; then
    echo "WARNING: some helper tool artifacts could not be recorded; the USB will still work but may lack those tools." >&2
fi
echo "Manifest written to $OUT (OpenCode $OPENCODE_VERSION)"
