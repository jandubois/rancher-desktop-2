#!/bin/bash

# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: SUSE LLC
# SPDX-FileCopyrightText: The Rancher Desktop Authors

# Regenerate the nerdctl parse table (pkg/nerdctlstub). The generate stage
# only builds and runs on Linux, so elsewhere it is cross-compiled and run
# in a Linux container (docker, or $CONTAINER_ENGINE).

set -o errexit -o nounset

cd "$(dirname "$0")/../pkg/nerdctlstub/generate"

# The extract stage locates nerdctl's source via `go list -m -f {{.Dir}}`,
# which is empty until the module has been downloaded — building extract
# alone does not pull it in, since extract reads the source and never
# imports it.
go mod download

go run ./extract

goos=$(go env GOOS)
if [[ ${goos} == "linux" ]]; then
    go run .
else
    binary=generate-nerdctl.tmp
    trap 'rm -f "${binary}"' EXIT
    GOOS=linux go build -o "${binary}" .
    toplevel=$(git rev-parse --show-toplevel)
    # This runs without --user. Off Linux the engine's file sharing writes
    # the output as the host user anyway, and under a rootless engine the
    # host UID may not own the mounted directory.
    "${CONTAINER_ENGINE:-docker}" run --rm \
        --volume "${toplevel}:/work" \
        --workdir /work/rdd/pkg/nerdctlstub/generate \
        alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a \
        "./${binary}"
fi
