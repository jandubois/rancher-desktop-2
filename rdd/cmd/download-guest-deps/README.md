# download-guest-deps

`download-guest-deps` stages the guest dependencies for the rdd build: the
pristine distro image, the nerdctl-full tarball, and mkcert. The distro is the
raw disk image that Lima's `vz` and `qemu` drivers boot or, for a Windows
build, the rootfs tarball that WSL2 imports. `rdd/Makefile` writes mkcert and
the binaries from that tarball into the image with `distro-overlay` before the
build embeds it.

It reads `dependencies.yaml`, which `yarn rddepman guest` writes, picks the
asset for the build target, and checks the downloaded bytes against the sha256
checksum recorded there. It keeps downloads in a cache outside the source tree,
so every checkout and worktree shares one copy, and it skips a staged file that
already matches its checksum.

`make build-rdd` and `make build-lima-controller` run it before building
`bin/rdd` and `bin/lima-controller`, the two binaries that embed the image. The
default paths are relative to `rdd/`, so run it from there when you run it by
hand.

## Usage

```sh
go run ./cmd/download-guest-deps [--manifest FILE] [--dest DIR] [--cache DIR] [--os OS] [--arch ARCH] [--stage NAME=PATH]
```

| Option | Default | Meaning |
|---|---|---|
| `--manifest` | `dependencies.yaml` | The guest dependency manifest to read. |
| `--dest` | `overlay/build` | The directory to stage the assets into. |
| `--cache` | see [Cache](#cache) | The directory that keeps verified downloads. |
| `--os` | the host's | The operating system the build targets, as a `GOOS` value. |
| `--arch` | the host's | The architecture the build targets, as a `GOARCH` value. |
| `--stage` | none | Stage `NAME` from a local file instead of downloading it. Repeatable. |

A cross-compiling build passes `--os` and `--arch`. Setting `GOOS` or `GOARCH`
in the environment instead would cross-compile this command as well, and leave
nothing that runs on the build machine.

Each asset carries the `filename` it is staged under, so the command needs no
knowledge of any package: it stages every dependency the manifest lists, under
the name the manifest gives. Adding one is a change to
`scripts/dependencies/`, not to this command.

Every asset it picks is a Linux one for `--arch`, because the guest VM runs
Linux. Only the image variant follows the target: Windows gets the distro's
`tar` asset and everything else its `raw` one, while a dependency shipping one
artifact per architecture records no variant and suits either.

The command exits 0 once everything is staged, 1 on any failure, and 2 on a
usage error. Failing to prune the cache only prints a warning.

## Staging a file the build produced

`--stage NAME=PATH` copies `PATH` into the destination as `NAME`'s asset
instead of downloading it. The manifest's checksum belongs to the released
asset, so nothing checks these bytes, and the log line for the copy ends in
`unverified`. The command stages every other dependency as usual, checksum and
all.

This is how a distro is tested before it ships. The openSUSE image build
produces an image no release covers, and the reverse test has to boot rdd with
that image rather than the pinned one. `rdd/Makefile` passes `STAGE_DISTRO`
through as `--stage distro=PATH`. Every build recopies the image and redoes the
overlay, so set it only for a test build.

```sh
make build-rdd STAGE_DISTRO=/path/to/distro.raw.xz
```

A `NAME` the manifest does not list fails the run. Ignoring it would download
the release instead and leave a green build that never saw the file under test.

## Cache

The default cache is `rancher-desktop.guest-deps` in the user cache directory,
which is `~/Library/Caches` on macOS, `$XDG_CACHE_HOME` or `~/.cache` on Linux,
and `%LocalAppData%` on Windows. Each download is kept at
`<name>/v<version>/<file>`, for example
`distro/v0.2.7/distro.v0.2.7.arm64.raw.xz`, so a new version is fetched beside
the old one.

The command checks a cached file against the manifest before copying it, and
downloads it again if it does not match. Each run then removes the downloads no
run has used for seven days. The prune touches nothing outside those version
directories.

## Network

The command drops a transfer that goes 30 seconds without receiving data and
tries again. An asset gets four attempts, all within two hours. Between
attempts it waits for the delay the server names, or backs off from two
seconds when it names none, but never longer than 30 seconds. A checksum
mismatch fails the run at once, and so does a client error such as a 404. A
408, a 429, or any response that names a delay is retried instead. A long
download logs its progress every five seconds.

## Trying it out

These steps stage into a scratch directory with its own cache, so they leave
`overlay/build` and your real cache alone and the first run has to download.
Run them from `rdd/` in a POSIX shell.

```sh
scratch=$(mktemp -d)
go run ./cmd/download-guest-deps --dest "$scratch/staged" --cache "$scratch/cache"
```

The first run downloads the raw image for this machine's architecture, 200 to
250 MiB, plus the nerdctl tarball and mkcert, and stages all three under
`$scratch/staged`. Running the same command again reports them up to date and
does no network work.

```sh
go run ./cmd/download-guest-deps --dest "$scratch/staged" --cache "$scratch/cache"
```

Damage the staged image, and the next run copies it back from the cache, still
without downloading.

```sh
printf broken > "$scratch/staged/distro.raw.xz"
go run ./cmd/download-guest-deps --dest "$scratch/staged" --cache "$scratch/cache"
```

Staging for Windows downloads the rootfs tarball, about 180 MiB, and stages it
as `$scratch/staged/distro.tar.xz`.

```sh
go run ./cmd/download-guest-deps --dest "$scratch/staged" --cache "$scratch/cache" --os windows --arch amd64
```

Remove the scratch directory when you are done.

```sh
rm -rf "$scratch"
```
