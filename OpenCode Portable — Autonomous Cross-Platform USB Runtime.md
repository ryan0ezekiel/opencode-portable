# OpenCode Portable

## Mission

Build a **self-contained portable OpenCode environment that runs directly from a USB drive without installing OpenCode or its required runtime/dependencies into the host computer**.

The intended user experience is:

```text
1. Plug USB into computer
2. Launch "OpenCode Portable"
3. Launcher automatically detects the host environment
4. Launcher determines the best compatible OpenCode runtime
5. Launcher prepares a USB-local environment
6. Launcher starts OpenCode
7. User works normally
8. Remove USB when finished
```

The user should **not need to manually select**:

- Windows/Linux/macOS
- x86-64/ARM64
- OpenCode version
- runtime
- Node/Bun/etc.
- host vs USB dependencies

The launcher should make these decisions automatically.

---

# 1. Reference project

Study this project before implementation:

**OpenClaude-Portable**

https://github.com/techjarves/OpenClaude-Portable

Use it as an architectural/reference implementation for:

- USB-local application state
- portable runtime management
- determining the application's own USB location
- platform-specific startup
- first-run setup
- persistent configuration
- cache/data handling
- avoiding global installation
- portable runtime acquisition

However:

**DO NOT simply fork or copy OpenClaude-Portable.**

OpenClaude-Portable is a reference for the portable-application architecture.

This project must be built specifically around **OpenCode**.

Also inspect the current official OpenCode distribution/release mechanism and use official OpenCode artifacts wherever possible.

---

# 2. Fundamental requirement

The system must be **adaptive**.

Do NOT implement:

```text
Windows launcher → always use X
Linux launcher   → always use Y
macOS launcher   → always use Z
```

Instead implement:

```text
Host detection
      ↓
Capability detection
      ↓
Runtime compatibility analysis
      ↓
Runtime selection
      ↓
Dependency selection
      ↓
Environment construction
      ↓
OpenCode
```

The launcher should choose the **best available execution environment**.

---

# 3. What "portable" means

Portable means:

- No OpenCode installation into the host OS.
- No required global Node/Bun installation.
- No required global Git installation.
- No required package-manager installation.
- No modification of system-wide configuration.
- No permanent PATH modification.
- No requirement for administrator/root privileges for normal operation.
- Application configuration should be stored on the USB where technically practical.
- Application cache/state should be stored on the USB where technically practical.
- Dependencies not available on the host should be supplied from the USB where technically practical.

The project must not assume that the host is a development machine.

---

# 4. Supported host platforms

Initial supported targets:

```text
Windows x86-64
Windows ARM64

Linux x86-64
Linux ARM64

macOS x86-64
macOS ARM64
```

Design the architecture so additional platforms can be added later.

---

# 5. Native bootstrapper

Use **Go** for the bootstrapper.

The bootstrapper is a small native executable whose job is to:

1. Locate itself.
2. Locate the USB root.
3. Detect the host OS.
4. Detect CPU architecture.
5. Detect relevant runtime/ABI information.
6. Detect available tools.
7. Detect USB filesystem capabilities.
8. Evaluate available OpenCode runtimes.
9. Select the best runtime.
10. Construct a portable environment.
11. Launch OpenCode.
12. Forward command-line arguments.
13. Report useful diagnostics if something fails.

The bootstrapper itself must have native builds for the supported platforms.

---

# 6. USB layout

Design the final USB roughly as:

```text
OpenCodePortable/
│
├── OpenCodePortable.exe
├── OpenCodePortable
├── OpenCodePortable.app/
│
├── runtimes/
│   ├── windows/
│   │   ├── amd64/
│   │   └── arm64/
│   │
│   ├── linux/
│   │   ├── amd64/
│   │   └── arm64/
│   │
│   └── darwin/
│       ├── amd64/
│       └── arm64/
│
├── tools/
│   ├── windows/
│   ├── linux/
│   └── darwin/
│
├── config/
│
├── data/
│
├── cache/
│
├── logs/
│
├── downloads/
│
└── manifest.json
```

Do not assume the USB has a particular drive letter or mount path.

The launcher must determine its location dynamically.

---

# 7. Runtime philosophy

The project should support two levels of execution:

## A. Native runtime

Prefer a native OpenCode binary/runtime when it is compatible with the host.

Example:

```text
Linux x86-64
    ↓
compatible Linux x86-64 OpenCode
    ↓
use native runtime
```

## B. Portable fallback

If the native runtime cannot execute because of compatibility problems, use a USB-contained fallback runtime where feasible.

Example:

```text
Linux x86-64
    ↓
native runtime incompatible
    ↓
USB portable runtime
    ↓
OpenCode
```

Do not build a massive operating system image unless technical investigation proves it necessary.

Prefer the smallest self-contained runtime that solves the compatibility problem.

---

# 8. Learn from OpenClaude-Portable

OpenClaude-Portable demonstrates a useful model:

```text
USB
│
├── application
├── runtime
├── tools
└── persistent data
```

It also demonstrates first-run runtime acquisition and USB-local environment management.

Use those ideas where appropriate.

However, improve upon it in these areas:

### OpenClaude-Portable style

```text
launcher
    ↓
known runtime
    ↓
application
```

### Desired OpenCode Portable

```text
launcher
    ↓
detect environment
    ↓
enumerate possible runtimes
    ↓
test compatibility
    ↓
score/select runtime
    ↓
select host/USB dependencies
    ↓
construct isolated environment
    ↓
OpenCode
```

---

# 9. Automatic OS detection

Detect at runtime:

```text
windows
linux
darwin
```

Do not rely solely on file extensions or environment variables.

Use Go/native OS APIs.

Unsupported OS:

```text
clean error
+
detected platform
+
supported platforms
```

Never crash with a Go stack trace.

---

# 10. Automatic CPU detection

Detect:

```text
amd64
arm64
```

Use native architecture information.

Do not infer architecture from the OS.

The selector must map:

```text
windows + amd64
windows + arm64
linux + amd64
linux + arm64
darwin + amd64
darwin + arm64
```

to the corresponding runtime.

---

# 11. Linux compatibility detection

Linux is the most important compatibility case.

Do not assume that:

```text
Linux + amd64
```

means every Linux OpenCode binary will execute.

Detect at least:

- kernel version
- libc implementation
- libc version
- CPU architecture
- executable permissions
- filesystem mount properties

Where possible, perform a safe compatibility probe.

For example:

```text
Can this runtime actually start?
```

If it cannot, do not select it.

Fall back to another compatible runtime.

---

# 12. Capability model

Create a structured capability model.

Example:

```go
type HostCapabilities struct {
    OS           string
    Architecture string
    OSVersion    string

    Kernel       string

    Libc         string
    LibcVersion  string

    RAMBytes     uint64

    HasGit       bool
    GitVersion   string

    HasRipgrep       bool
    RipgrepVersion   string

    HasNode       bool
    NodeVersion   string

    HasBun        bool
    BunVersion   string

    USBExecutable bool
    USBWritable   bool

    CPUFeatures []string
}
```

Extend this only when useful.

Do not collect unnecessary information.

---

# 13. Hardware detection

Hardware detection is primarily for **runtime compatibility and optimization**, not for surveillance or unnecessary system inventory.

Detect where practical:

- CPU architecture
- CPU feature set
- RAM
- GPU vendor/model where useful

Do not make GPU detection a hard requirement for launching OpenCode.

If hardware information does not influence runtime selection, it should not block startup.

---

# 14. Host dependency detection

Detect whether the host already has usable versions of:

- Git
- ripgrep
- Node
- Bun

Potentially add others later.

Do not require them.

The selection algorithm should work like:

```text
Is usable host dependency available?
        │
      YES
        ↓
Use host dependency if policy permits.

        OR

      NO
        ↓
Use USB-provided dependency.
```

Never install missing dependencies into the host.

---

# 15. Host vs USB dependency policy

Default policy:

```text
Prefer usable host dependency
Fallback to USB dependency
Never install globally
```

For example:

```text
Git available on host?
    YES → use host Git

Git unavailable?
    YES → use USB Git
```

But the policy must be configurable.

Provide:

```text
prefer_host_tools
fallback_to_usb
```

etc.

---

# 16. Runtime selection engine

Do not hard-code a simple OS→binary mapping.

Implement a runtime selector.

Conceptually:

```text
detect host
     ↓
list candidate runtimes
     ↓
remove incompatible candidates
     ↓
score candidates
     ↓
select best candidate
```

Candidate properties may include:

```text
OS
architecture
ABI
runtime type
OpenCode version
compatibility
native/portable
```

Native compatible runtimes should normally receive the highest priority.

Portable fallbacks should be lower priority.

---

# 17. Example runtime selection

Example:

```text
Host:

OS = Linux
Architecture = amd64
glibc = 2.31

Candidates:

1. linux/amd64/native
   requires glibc 2.39
   incompatible

2. linux/amd64/native-old
   requires glibc 2.28
   compatible

3. linux/amd64/portable
   compatible
```

Selection:

```text
linux/amd64/native-old
```

If no compatible native runtime exists:

```text
linux/amd64/portable
```

The user should not need to understand any of this.

---

# 18. Portable environment

Before launching OpenCode, construct a process environment.

The environment should provide:

```text
OpenCode
PATH
configuration
cache
data
temporary directories
required USB tools
```

Do not permanently alter the host environment.

Only modify the environment of the OpenCode process and its children.

---

# 19. USB-local state

Prefer:

```text
OpenCodePortable/config/
OpenCodePortable/data/
OpenCodePortable/cache/
OpenCodePortable/logs/
```

for portable state.

Avoid unnecessarily writing:

```text
~/.config
~/.cache
~/.local
```

or equivalent host locations.

If OpenCode requires a host location for something, document the exception explicitly.

---

# 20. Project handling

The USB does NOT need to contain the user's source code.

OpenCode should operate on normal host project directories.

Support:

```text
OpenCodePortable <OpenCode arguments>
```

and allow normal OpenCode project/path arguments.

If launched from a project directory, preserve that context.

Do not copy projects onto the USB unless explicitly requested.

---

# 21. Argument forwarding

The launcher must transparently forward arguments.

Example:

```text
OpenCodePortable --help
```

must invoke the selected OpenCode binary with:

```text
--help
```

Likewise:

```text
OpenCodePortable <normal OpenCode arguments>
```

must behave like invoking OpenCode directly.

---

# 22. Diagnostics

Implement:

```text
OpenCodePortable --diagnose
```

Output:

```text
OpenCode Portable
────────────────────────

Host:
  OS:
  Architecture:
  OS version:
  Kernel:
  libc:

Hardware:
  RAM:
  CPU:
  GPU:

Tools:
  Git:
  ripgrep:
  Node:
  Bun:

USB:
  Path:
  Writable:
  Executable:

Runtimes:
  ...

Selected runtime:
  ...

Reason:
  ...
```

No secrets must ever appear.

---

# 23. Dry run

Implement:

```text
OpenCodePortable --dry-run
```

This must:

1. Detect host.
2. Evaluate runtimes.
3. Evaluate dependencies.
4. Select runtime.
5. Display what would happen.
6. NOT launch OpenCode.

---

# 24. Logging

Logs go to:

```text
logs/
```

Log:

- detection
- runtime selection
- dependency selection
- startup errors
- update errors

Never log:

- API keys
- passwords
- OAuth tokens
- credentials
- authentication cookies

---

# 25. Security

The launcher must be security-conscious.

Requirements:

- Do not use shell evaluation for constructed commands.
- Avoid command injection.
- Validate paths.
- Do not blindly add the entire USB to PATH.
- Only execute known runtime/tool paths.
- Verify downloaded binaries.
- Use checksums/signatures when provided.
- Never silently execute arbitrary downloads.
- Never expose credentials in process arguments if avoidable.
- Never write credentials into logs.
- Never modify host system configuration unnecessarily.

---

# 26. OpenCode acquisition

Create a build/update mechanism that retrieves OpenCode from the **official OpenCode release/distribution source**.

Do not manually embed a particular OpenCode version into source code.

Use a manifest.

Example:

```json
{
  "opencode_version": "...",
  "runtimes": {
    "windows/amd64": {
      "version": "...",
      "sha256": "..."
    },
    "linux/amd64": {
      "version": "...",
      "sha256": "..."
    }
  }
}
```

The exact release URL/API must be determined from the current official OpenCode project.

Do not invent release URLs.

---

# 27. Updating

Eventually implement:

```text
OpenCodePortable --update
```

Behavior:

```text
Check official release
        ↓
Determine required artifacts
        ↓
Download
        ↓
Verify
        ↓
Install to USB
        ↓
Update manifest
        ↓
Preserve data/config
```

Use atomic replacement where possible.

If an update fails, preserve the previously working version.

---

# 28. First-run behavior

If required runtimes/tools are absent:

```text
OpenCode Portable
Preparing first run...
```

Then:

```text
detect
↓
download required artifacts
↓
verify
↓
install into USB
↓
launch
```

No host installation.

The user should not need to manually install dependencies.

---

# 29. Read-only USB

Detect read-only USB.

If OpenCode can still run:

```text
Run in read-only portable mode
```

If required state cannot be persisted:

```text
Explain exactly what cannot be persisted.
```

Do not silently write to the host as a fallback.

If a host fallback is technically unavoidable, clearly document it.

---

# 30. noexec Linux USB

If the USB is mounted with:

```text
noexec
```

detect it.

Do not attempt to circumvent the security restriction.

Explain:

```text
The USB filesystem is mounted with execution disabled.
OpenCode Portable cannot execute binaries directly from this mount.
```

Offer only legitimate alternatives.

---

# 31. Build system

Provide reproducible build scripts.

Required targets:

```text
Windows amd64
Windows arm64

Linux amd64
Linux arm64

macOS amd64
macOS arm64
```

Build outputs should be placed in:

```text
dist/
```

Provide scripts for:

```text
build-all
fetch OpenCode
verify artifacts
package USB
generate manifest
```

---

# 32. Packaging

Produce a final USB package:

```text
dist/OpenCodePortable/
```

with:

```text
OpenCodePortable.exe
OpenCodePortable
OpenCodePortable.app/

runtimes/
tools/
config/
data/
cache/
logs/

manifest.json
README.txt
```

The package should be directly copyable onto a USB drive.

---

# 33. Testing

Test at minimum:

### Windows

- x86-64
- ARM64 if available

### Linux

- Arch/CachyOS
- Ubuntu/Debian-family system
- older glibc environment
- x86-64
- ARM64

### macOS

- Apple Silicon
- Intel Mac if available

Test:

- USB path with spaces
- USB path with Unicode
- launcher started from another working directory
- missing dependencies
- incompatible native runtime
- portable fallback
- read-only USB
- noexec USB
- corrupted manifest
- invalid checksum
- missing runtime
- unsupported architecture
- unsupported OS
- argument forwarding
- OpenCode exit codes

---

# 34. Development strategy

Do NOT implement everything at once.

Implement in these milestones.

## Milestone 1 — Minimal working launcher

Support:

```text
Linux amd64
```

with:

```text
launcher
+
USB-local OpenCode
```

Requirements:

- locate launcher
- detect OS
- detect architecture
- launch OpenCode
- forward arguments

---

## Milestone 2 — Cross-platform

Add:

```text
Windows amd64
Linux arm64
macOS amd64
macOS arm64
Windows arm64
```

---

## Milestone 3 — Capability detection

Add:

- libc
- kernel
- host tools
- USB execution
- USB write capability

---

## Milestone 4 — Runtime selection

Add:

```text
native runtime
portable runtime
compatibility testing
runtime scoring
```

---

## Milestone 5 — USB dependencies

Add:

```text
Git
ripgrep
Node/Bun if actually required
```

using host-first/USB-fallback behavior.

---

## Milestone 6 — Portable state

Move appropriate:

```text
config
cache
data
logs
```

to USB.

---

## Milestone 7 — Update system

Implement verified OpenCode updates.

---

## Milestone 8 — Polish

Add:

- friendly startup UI
- diagnostics
- dry run
- robust errors
- documentation
- packaging

---

# 35. Critical implementation rule

Before implementing any runtime mechanism, inspect the **current OpenCode release architecture**.

Determine:

1. Is OpenCode currently distributed as a standalone executable?
2. What runtimes does each official distribution require?
3. Which platforms/architectures are officially supported?
4. Does the standalone distribution already eliminate Node/Bun requirements?
5. What dependencies are actually required at runtime?
6. What configuration/data directories does OpenCode use?
7. Can those directories be redirected cleanly?
8. What are the actual Linux ABI requirements?
9. What is the official release/download mechanism?

Do not assume answers based on older versions.

Build against the current OpenCode release architecture.

---

# 36. Important architectural principle

Do not unnecessarily reproduce the architecture of OpenClaude-Portable.

If OpenCode's current standalone distribution already packages its required runtime, use that.

The purpose of this project is:

```text
OpenCode official binary
        +
portable orchestration
        +
automatic platform detection
        +
automatic runtime selection
        +
USB-local state
```

not:

```text
OpenCode
+
unnecessarily bundled Node
+
unnecessarily bundled package manager
```

Minimize the amount of software carried on the USB.

---

# 37. Final acceptance criterion

The project succeeds when a user can take the same USB and move it between supported machines:

```text
CachyOS PC
Windows PC
MacBook Apple Silicon
Intel Mac
ARM64 Linux machine
```

and perform:

```text
Plug USB
↓
Launch OpenCode Portable
↓
Wait for automatic detection
↓
OpenCode appears
```

without manually selecting a platform, architecture, runtime, or dependency.

The user should perceive the system as:

> **"I carry OpenCode with me on this USB. I plug it into a compatible computer, launch it, and it figures everything else out."**

That is the product being built.