# nerdctl Stub

The `rdd nerdctl` command runs nerdctl inside the Lima VM. nerdctl runs only
on Linux, so the binary lives in the guest; the stub rewrites path arguments
to where the guest mounts them, then invokes the guest binary with the
caller's stdio. The multicall dispatch in `cmd/rdd/multicall.go`
also accepts `nerdctl` as the invocation name, so a binlink can expose the
stub as plain `nerdctl`.

On macOS and Linux the guest mounts the host home directory at its original
path, so arguments pass through unchanged. On Windows the WSL2 guest
automounts drives under `/mnt`, so the stub rewrites `C:\src\app` to
`/mnt/c/src/app`. It first resolves a relative path against the host working
directory, and a drive-relative one such as `d:app` against the current
directory of drive D:, as Windows reports it through `GetFullPathName`.
Windows defines what `d:app` means, so the stub resolves it instead of
rejecting it as ambiguous. A rooted path without a drive, such as `\data` or
`/data`, takes the root of the working directory, its drive or its
`\\server\share`, as the "Rancher Desktop 1.x" stub did. The guest does not
mount UNC paths, so they pass through with forward slashes. A `--volume` or
`--mount` source that nerdctl reads as a volume name (`isNamedVolume` in
nerdctl's `pkg/mountutil`) passes through unchanged.

An argument that the stub rewrites always names a host path. With the working
directory on C:, `-v /mnt/c/src:/src` mounts `C:\mnt\c\src`, not the VM's
`/mnt/c/src`. To use a VM path in such an argument, run `rdd limavm shell rd
sudo nerdctl --address /run/k3s/containerd/containerd.sock` with the nerdctl
arguments instead. Flags that take a guest path, such as `--device`, pass
through unchanged.

## Runtime

`rdd nerdctl` disables cobra flag parsing and hands the raw arguments to
`pkg/nerdctlstub`, a port of the "Rancher Desktop 1.x" stub parser
(`src/go/nerdctl-stub/parse_args.go`). The parser must know, for every
nerdctl flag, whether it consumes a value (misjudging that shifts every
later argument) and whether to translate the value.

Two tables drive the parser:

| Table | Contents | Source |
| --- | --- | --- |
| command table | every command, its flags, whether each flag consumes a value | generated |
| overlay | the flags whose host paths the stub translates, with a handler kind for each | hand-curated |

The six handler kinds, one per nerdctl path syntax, are input path, input
path that may also be `-` for stdin or a URI (`compose --file`,
`--cosign-key`), comma-separated list of input paths (`run --env-file`),
output path, `--volume` (`host:container[:mode]`), and `--mount` (CSV with
`source=`).

The stub does not translate the root filesystem that `run --rootfs` and
`create --rootfs` take as their first positional argument, so name it by its
guest path, such as `/mnt/c/rootfs`. The same goes for a host path in the
value of a flag that the overlay leaves out, such as
`--security-opt seccomp=<path>` or `--log-opt log-path=<path>`.

A path inside a `--mount` value cannot contain a comma, since the value is
comma-separated and the stub splits it at every comma. The stub
expands a relative path to an absolute one, so a relative path fails the same
way when the working directory has a comma, such as `OneDrive - Acme, Inc`.

The parse ends at an option that neither the command nor an enclosing command
lists, because the stub cannot tell whether it takes a value. From there the
stub passes the option and every later argument through unchanged, logs that
at debug level, and leaves nerdctl to accept or reject the option.

The stub never starts Rancher Desktop. It fails unless the App runs the
containerd engine; `rdd run nerdctl` starts the App first. After parsing, it
connects to the guest over SSH through `pkg/guestexec`, as `rdd limavm shell`
does, changes to the guest path of the
host working directory, and runs `sudo nerdctl --address
/run/k3s/containerd/containerd.sock` with the rewritten arguments. nerdctl
needs root there. Run as any other user, it switches to rootless mode even
with an explicit `--address`. Relative paths inside compose files and build
contexts resolve in the guest because the working directory maps to the same
files; when the guest cannot see the working directory, the command runs from
the home directory instead.

The 1.x stub injects `--security-opt seccomp=/etc/rancher-desktop/seccomp.json`
into `run` and `create`; that profile is a 1.x distro asset which the
"Rancher Desktop 2" distro does not ship, so the port drops the injection.
