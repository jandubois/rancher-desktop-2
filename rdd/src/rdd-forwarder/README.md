# rdd-forwarder

A small Windows executable that forwards to a bundled tool. The instance bin
directory (`~/.rd<n>/bin`) publishes one per tool on Windows, where a standard
user can make neither symlinks nor hardlinks to binaries under Program Files.
Each forwarder reads its target from a sibling `<tool>.shim` file and execs it,
so `kubectl.exe` → `rdd.exe` runs rdd's multicall `kubectl`. The consumer is
`rdd/pkg/binlinks` (`forwarders.go`), which documents the name/format contract.

## Origin and license

Imported from [ScoopInstaller/Shim](https://github.com/ScoopInstaller/Shim)
under its **Unlicense** (public-domain dedication) — that is how the upstream
code is licensed *to us*, and it permits releasing under a different license. It
is now part of Rancher Desktop under the project's regular **Apache-2.0**
license, which is how it is licensed *to our users*. The upstream repository,
commit, and version are recorded in `UPSTREAM`.

## Build

Built from source by `make build-forwarder` (see `rdd/Makefile`), Windows-only,
natively on the Windows runners under MSYS2 bash — using the same C toolchain
`make build-rdd`'s cgo already uses there: mingw `g++` (x86_64) and `clang++`
(arm64), no new dependency. Override `FORWARDER_CXX` to cross-build elsewhere
(e.g. `FORWARDER_CXX=x86_64-w64-mingw32-g++` with a Linux mingw-w64 toolchain).
The output `rdd-forwarder.exe` is staged into `resources/windows/bin/` by
`packaging/electron-builder.yml`.

To update, re-copy `shim.cpp` from a newer upstream commit, update `UPSTREAM`,
re-apply Rancher Desktop's local changes, and restore the Apache-2.0 header.
