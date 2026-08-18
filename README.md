# OpenCode Portable

A self-contained, portable [OpenCode](https://github.com/anomalyco/opencode)
environment that runs from a USB drive — no installation, no admin rights,
no leftover files on the host machine.

## What it does

`OpenCodePortable` is a small launcher that lives on the USB drive next to
a release manifest. On every start it:

1. **Finds itself** — the portable root is wherever the launcher lives
   (USB drive, external disk, any folder). Move it freely.
2. **Detects the host** — OS, CPU architecture, libc (glibc version or
   musl), AVX2 support, GPU, available tools.
3. **Selects the best OpenCode runtime** from the official releases —
   `native` → `baseline` (non-AVX2) → `musl` (non-glibc) → `baseline-musl`.
   Statically incompatible candidates are filtered, then the chosen binary
   is *executed* to verify it really runs (the probe is the final
   authority). If it fails, the launcher falls back to the next candidate.
4. **Builds a portable environment** — `XDG_CONFIG_HOME`/`XDG_DATA_HOME`/
   `XDG_CACHE_HOME` (and `APPDATA`/`LOCALAPPDATA` on Windows) point into
   the USB tree; `OPENCODE_DISABLE_AUTOUPDATE=1` prevents surprise
   upgrades; temp files go to the USB; only the selected tool directories
   are prepended to `PATH`.
5. **Launches** OpenCode with every argument forwarded unchanged.

Nothing about your sessions, credentials or history is left on the host.

## Layout

```
OpenCodePortable/
├── OpenCodePortable        the launcher (OpenCodePortable.exe on Windows)
├── manifest.json           release manifest: versions, URLs, SHA-256 digests
├── runtimes/<os>/<arch>/   installed OpenCode binaries (per variant)
├── tools/<os>/<arch>/      USB-provided helper tools (ripgrep, MinGit)
├── config/                 portable configuration (incl. portable.json)
├── data/                   portable application data (sessions, auth)
├── cache/                  portable cache (LSP servers, temp files)
├── logs/                   launcher logs (opencode-portable.log)
├── downloads/              verified release archives awaiting install
└── README.txt              end-user documentation (copied to the USB)
```

## Usage

```
./OpenCodePortable                 launch OpenCode (portable mode)
./OpenCodePortable --dry-run       show the plan without launching
./OpenCodePortable --diagnose      print a full system report
./OpenCodePortable --update        update OpenCode to the latest release
./OpenCodePortable --update --all-platforms   update every platform
./OpenCodePortable --verbose       verbose logging
./OpenCodePortable run "…"         everything else is forwarded to OpenCode
```

Tool policy (`config/portable.json`):

```json
{ "tool_policy": "prefer_host_fallback_usb" }   // default
{ "tool_policy": "usb_only" }
{ "tool_policy": "host_only" }
```

## Design

- **Adaptive, not hard-coded**: the selector never maps OS→binary. It
  filters by static requirements (libc, min glibc, AVX2) and verifies by
  executing each candidate (`--version`, 15 s timeout). Scoring prefers
  probed, installed, native builds.
- **Verified downloads**: every artifact is checked against the SHA-256
  digest published in the official release (and recorded in the manifest)
  before it is installed. Archives are extracted with traversal
  protection (zip-slip/absolute paths rejected).
- **Atomic updates**: downloads go to `downloads/`, installs stage into
  `.staging` directories and swap into place; the manifest is the commit
  point. A failed update keeps the previous version untouched.
- **Failure tolerance**:
  - read-only USB + runtime present → warn and run
  - read-only USB + runtime missing → clear explanation, exit 1
  - noexec volume → explanation with legitimate alternatives
  - corrupted/absent manifest → embedded default manifest
  - unsupported platform → clean error listing supported platforms
- **Security**: no shell is ever invoked for constructed commands; the
  child runs with a minimal environment; PATH is deduplicated.

## Building

Requires Go ≥ 1.26 and `jq` (for scripts).

```
make build        # all 6 launchers -> dist/launchers/
make fetch        # download artifacts for all platforms -> dist/staging/
make package      # assemble USB bundles -> dist/OpenCodePortable-<platform>/
make test         # go vet + unit tests
make verify       # verify staged artifacts against manifest digests
make bundle       # build + fetch + verify + package (one platform: PLATFORM=linux-x64)
```

## Scripts

| Script | Purpose |
| ------ | ------- |
| `scripts/build-all.sh` | cross-compile launchers for all 6 platforms |
| `scripts/generate-manifest.sh` | regenerate `internal/app/default.json` from official releases |
| `scripts/fetch-opencode.sh` | download+verify artifacts for one platform into a staging dir |
| `scripts/verify-artifacts.sh` | verify staged artifacts against manifest digests |
| `scripts/package-usb.sh` | assemble a ready-to-copy USB bundle from staging |

## Testing

- `go test ./...` — unit tests: version comparison, static compatibility,
  zip/tar traversal rejection, digest verification, environment
  construction, argument parsing, manifest validation.
- The update path is integration-tested against a local HTTP server
  serving a GitHub-shaped release JSON (`OPENCODE_PORTABLE_RELEASE_URL`).

## Status

Launcher v0.1.0, implemented and tested on Linux (glibc host); cross
compiles for all 6 supported platforms (Linux/macOS/Windows × amd64/arm64).
Windows/macOS runtime behavior is compiled in but not yet exercised on real
hardware.

## Disclaimer

OpenCode Portable is an independent launcher. OpenCode itself is the
product of its own maintainers; all runtime binaries are downloaded from
the official releases with checksums verified against the official
release metadata.
