# Rancher Desktop Daemon Development

For design details, please refer to [design/](design/).

## Prerequisites

This section only applies to building (and testing) Rancher Desktop Daemon; for
prerequisites for running it, please refer to user documentation.

We of course require all the things running RDD does (e.g. the ability to run
VMs).

On all platforms, we expect at least:
- Git
- GNU Make 4 or higher. (macOS ships with 3.81, which is too old.)
- GNU coreutils, and other basic tools like `gawk`, `gzip`, and `sed`.
- GNU bash version 5 or higher. (macOS ships with 3.2, which is too old.)
- Golang compiler as listed in `go.mod` (from go.dev, not the GCC toolchain or
  others).
- jq
- Perl (for check-spelling only).

We currently only support building from a checked-out source tree (i.e. with the
`.git` directory available, including any tags).

For all platforms, we only support whichever OS version we are using, which is
generally the latest release versions.

### macOS

On macOS, we expect Xcode command line tools to be available.

### Windows

Development is supported under WSL2 and MSYS2.  Development using `cmd.exe`,
PowerShell, Git Bash, or Cygwin is not supported.

All RDD processes run as Win32 executables.  Developing for Linux on a Windows
host is only supported when using a full Linux VM, whether or not WSL
integration is enabled.

#### WSL2

WSL interop must be enabled (we test for `winver.exe` being around).

#### MSYS2

Install MSYS2, Git, and Go natively on Windows (e.g. via scoop) rather than
through pacman; the MSYS2 versions may behave differently from what CI uses.

```
scoop install msys2 git go
```

Then install build dependencies inside MSYS2:

```bash
pacman --sync --needed jq make mingw-w64-x86_64-gcc openbsd-netcat openssh
```

The BATS test harness exports `MSYS_NO_PATHCONV=1` to prevent MSYS2 from
converting URL-like arguments (e.g. `/passthrough/demo/hello`) into Windows
paths.  The `rdd()` wrapper in `bats/helpers/commands.bash` handles explicit
path conversion for arguments that need it.

## Building against a Lima fork

rdd links Lima from the `github.com/lima-vm/lima/v2` module and builds the
guest agent it embeds from the same module, so a fork changes both the host and
the guest. To build against changes upstream has not merged yet, replace the
module in `go.mod`:

```bash
go mod edit -replace=github.com/lima-vm/lima/v2=github.com/rancher-sandbox/lima/v2@<commit>
go mod tidy
```

`go mod tidy` turns the commit into a pseudo-version, and `go list -m -f
'{{.Dir}}' github.com/lima-vm/lima/v2` prints the source directory the build
uses. Add a comment above the `replace` naming the upstream pull requests it
waits for. Once they have all merged, remove the `replace` and require an
upstream version that contains them; the `require` line still names the version
from before the fork, and the build goes back to it. The WSL guest agent in
`src/wsl-guestagent` is a separate module with its own Lima requirement and
`replace`, so the `replace` in the main `go.mod` does not apply to it.

A fork misses every upstream fix made after its base, security fixes included,
so rebase it onto upstream whenever you update it, and check
[Lima's security advisories](https://github.com/lima-vm/lima/security/advisories).
Lima squashes pull requests and backports fixes to release branches as new
commits, so `git cherry` and `git merge-base --is-ancestor` miss fork commits
that did land upstream; compare the code instead.
