# distro-overlay

`distro-overlay` merges Rancher Desktop assets into a pristine openSUSE distro at
build time. The distro stays "just the OS"; everything Rancher Desktop specific
lives in this repository and is layered on from a single manifest.

## Usage

    distro-overlay --manifest manifest.yaml (--output out.raw | --in-place) [--source ./files] [--profile lima|wsl] [--mtime T] [--kernel-params P] <distro>

| Flag | Meaning |
|------|---------|
| `--manifest` | YAML manifest of entries to merge (required) |
| `--source` | Directory holding the file sources (default: the manifest's directory) |
| `--format` | `auto` (default), `raw`, or `tar`; `auto` detects by signature |
| `--output` | Write the result here, leaving the distro alone; the tool writes through to it, empties it when the run fails, and refuses one naming the distro |
| `--in-place` | Overlay the distro itself, modifying it; pass this or `--output`, never both |
| `--profile` | Build profile to overlay for, `lima` or `wsl`; applies only the entries for it, plus the entries naming no profile. Omitted, every entry applies |
| `--mtime` | Timestamp for every entry: Unix epoch seconds or RFC3339 (default: now) |
| `--kernel-params` | Parameters to append to every kernel command line; raw images only |

`<distro>` is an uncompressed tarball or raw image. Decompress it first and
recompress afterward; the tool works on the uncompressed artifact.

`--kernel-params` appends to the command line kiwi baked into the image, by
rewriting `/boot/grub2/grub.cfg`. It is for a parameter that suits one host and
the shipped image should not carry, such as a clock setting for a CI runner.

## Distro forms

The same manifest drives both forms:

- **WSL tarball** — the tool appends each entry to the tar, dropping any base
  path the manifest overrides so the overlay wins.
- **Lima raw image** — the tool writes each entry into the ext4 root partition
  through go-diskfs, with no `resize2fs` and no root on the build host. The image
  must reserve free space at build time (kiwi `<size additive>` in the
  rancher-desktop-opensuse `config.kiwi`); an overlay that exceeds the reserve
  fails rather than growing the filesystem, so raise the reserve and rebuild.
  Past about 768 MiB, read the `BLOCK_UNINIT` limit below before you do.

## Raw image limits

go-diskfs, which writes the ext4 image, mishandles the cases below, so the tool
refuses them. It checks each entry before writing it, but it can only find an
extent tree after go-diskfs has written one. A failed write therefore empties an
`--output`. With `--in-place` there is no copy to fall back on. The distro keeps
whatever the run wrote before it failed, and the only recovery is to start again
from the pristine image. So the tool modifies a distro only when you pass
`--in-place`.

- **Files needing an extent tree.** go-diskfs writes extent-tree blocks without
  their `metadata_csum` checksum, and an inode holds four extents before it
  needs a tree, so the tool refuses a file that needs a fifth. One extent spans
  at most 32768 blocks, 128 MiB at the distro's 4 KiB blocks, so a file up to
  that size fits in one extent given a free run that large. Between 128 and 256
  MiB it takes two to four runs, found from the start of the image. From 256 MiB
  up go-diskfs refuses the file outright, because it allocates at most 65535
  blocks at a time and 65536 of those 4 KiB blocks is exactly 256 MiB. The tool
  finds a tree by counting a file's blocks, so an external xattr block looks the
  same and is refused with it. Overriding a file the distro already has can need
  a fifth extent too, when the file is fragmented and the new contents are
  longer.
- **Shrinking a file.** go-diskfs ignores `O_TRUNC`, so the tool refuses to
  replace an image file with a shorter one.
- **Replacing a symlink.** go-diskfs writes through a symlink to its target, so
  the tool refuses a file entry where the image has a symlink.
- **Replacing anything with a symlink.** go-diskfs refuses a name that already
  exists, so a symlink entry must name a free path.
- **Hard-linked files.** The tool writes into the file's inode, which every
  other name on it shares. `/usr/bin` and `/usr/sbin` are full of busybox
  links, so replacing one there would replace them all.
- **Files with holes.** go-diskfs numbers a grown file's new extent from the
  blocks it already owns, so the contents land over the blocks before a hole.
- **Adding to an indexed directory.** go-diskfs corrupts an htree-indexed
  directory when it adds an entry. Large directories such as `/usr/bin` are
  indexed, so install under `/usr/local`.

The tool also refuses two kinds of image outright, before writing any entry. One
is an image with a `BLOCK_UNINIT` block group, because go-diskfs allocates into
one without clearing the flag and the kernel then reads those blocks as free.
mke2fs leaves such groups in any filesystem from about 768 MiB up that its data
does not reach, so a distro built with more free space can acquire one. No
mke2fs option suppresses them on a `metadata_csum` filesystem. `-O ^uninit_bg`
and `-E lazy_itable_init=0` both leave the group in place, and only dropping
`metadata_csum` clears it, which is not an option for this image. Giving mke2fs
enough `-d` data to reach every group does work, so the fix belongs in the
distro build. The other is a filesystem without the 64bit feature, whose
32-byte group descriptors crash go-diskfs.

The tarball backend handles the rest, and refuses hard links as well, so one
manifest drives both forms. Each refusal can go once go-diskfs fixes the bug
behind it.

## Manifest

See [`manifest.example.yaml`](manifest.example.yaml) for the schema and defaults:
files, directories, directory trees, and symlinks.

Every entry is stamped with one modification time from `--mtime` (default: the
tool's start time). Passing a deterministic value — for example a git commit
time — is what makes a rebuild reproducible.

## Tests

The tests in this package and in `pkg/overlay` that write a real ext4 image
shell out to `mke2fs`, `e2fsck`, and `debugfs`. Without those on `PATH` they
skip, and `go test` still prints `ok`, so the ext4 backend looks covered when
nothing exercised it. On macOS the Homebrew formula is keg-only, so its `sbin`
goes on `PATH` by hand:

    brew install e2fsprogs
    export PATH="$(brew --prefix)/opt/e2fsprogs/sbin:$PATH"

Set `RDD_REQUIRE_E2FSPROGS=true` to fail instead of skipping. CI sets it
everywhere but Windows, which has no e2fsprogs package and builds only the
tarball.
