OpenCode Portable
=================

OpenCode Portable is a self-contained, portable OpenCode environment that
runs from a USB drive (or any folder you can carry around). It needs no
installation: no Go, no Node, no Bun, no npm on the host machine.

WHAT IS INSIDE
--------------
  OpenCodePortable    the launcher (or OpenCodePortable.exe on Windows)
  manifest.json       release manifest: versions, URLs, SHA-256 digests
  runtimes/           OpenCode CLI binaries, one folder per OS/architecture
  tools/              helper tools (ripgrep, MinGit on Windows)
  config/             portable configuration (OpenCode + launcher settings)
  data/               portable application data (sessions, auth, storage)
  cache/              portable cache (LSP servers, downloads, temp files)
  logs/               launcher logs
  downloads/          downloaded release archives awaiting install

HOW TO USE
----------
  1. Copy the CONTENTS of this folder onto your USB drive.
  2. Open a terminal in that folder.
  3. Run:

       ./OpenCodePortable            (Linux/macOS)
       OpenCodePortable.exe          (Windows)

  OpenCode starts with its configuration, data and cache stored on the
  USB drive. Nothing about your sessions is left on the host machine.

  Arguments are passed through unchanged, e.g.:

       ./OpenCodePortable --help
       ./OpenCodePortable run "explain this code"
       OpenCodePortable.exe C:\path\to\project

FIRST RUN
---------
  The first run downloads the OpenCode CLI (official release from
  github.com/anomalyco/opencode) onto the USB drive and verifies its
  SHA-256 digest. This requires an internet connection and a writable
  volume. Afterwards OpenCode works offline.

  If the volume is pre-populated (runtimes/ and tools/ already filled),
  first run needs no internet at all.

FINDING YOUR USB DRIVE
----------------------
  The launcher always works next to itself: wherever the
  OpenCodePortable folder lives (USB drive, external disk, home folder),
  that is the portable root. You can rename or move the folder freely.

ADAPTIVE RUNTIME SELECTION
--------------------------
  On each start the launcher inspects the host (OS, CPU architecture,
  libc, AVX2 support) and picks the best compatible OpenCode build:

    linux x64   native  -> baseline  -> musl  -> baseline-musl
    linux arm64 native  -> musl
    windows     native  -> baseline
    macos       native  -> baseline

  "baseline" builds run on CPUs without AVX2; "musl" builds run on
  systems without glibc. If the best build fails to execute, the launcher
  falls back to the next one automatically.

HELPER TOOLS
------------
  OpenCode uses git and ripgrep. The launcher prefers tools already
  installed on the host, and uses the USB-provided tools (tools/) only
  when the host lacks them. Put a portable config at config/portable.json
  to change this:

    { "tool_policy": "prefer_host_fallback_usb" }   (default)
    { "tool_policy": "usb_only" }
    { "tool_policy": "host_only" }

LAUNCHER COMMANDS
-----------------
  ./OpenCodePortable                     launch OpenCode (portable mode)
  ./OpenCodePortable --dry-run           show what would happen, do nothing
  ./OpenCodePortable --diagnose          print a full system report
  ./OpenCodePortable --update            update OpenCode to the latest version
  ./OpenCodePortable --update --all-platforms   update every platform's runtime

  All other flags and arguments are passed to OpenCode unchanged.

READ-ONLY VOLUMES
-----------------
  If the USB drive is mounted read-only, OpenCode still runs, but
  configuration and session data cannot be saved. Pre-populated runtimes
  make a read-only volume fully usable (the "read-only portable" mode).

  If the volume is read-only AND the runtime is missing, the launcher
  explains what is needed and exits. Run it once from a writable volume
  to prepare the drive.

WHY THIS EXISTS
---------------
  A portable OpenCode you can plug into any machine: no installers, no
  admin rights, no leftover files, no surprise upgrades, full control
  over which OpenCode version runs. Your configuration, credentials and
  history stay on your drive.

SECURITY NOTES
--------------
  * Every downloaded artifact is verified against the SHA-256 digest
    recorded in manifest.json before it is installed.
  * The launcher never executes shell commands; it runs binaries directly
    with a minimal environment.
  * Credentials and session data are stored only on the USB drive.
    Protect the drive accordingly.

TROUBLESHOOTING
---------------
  * "cannot execute binary" — the volume may be mounted with noexec.
    Remount with execution enabled or move the folder to another volume.
  * "USB volume is read-only" — remount read-write, or pre-populate the
    drive from a machine with internet access (see scripts/ in the
    source distribution).
  * Diagnose problems: ./OpenCodePortable --diagnose
  * Launcher logs live in logs/opencode-portable.log