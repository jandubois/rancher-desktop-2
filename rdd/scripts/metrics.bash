#!/usr/bin/env bash

# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: SUSE LLC
# SPDX-FileCopyrightText: The Rancher Desktop Authors

# Shared helpers for the BATS timing instrumentation, sourced by
# bats-with-timeout.sh and by the bats helpers so both append to one set of
# TSV files.
#
# The default directory is inside the tree the BATS workflow uploads as the
# rdd-logs artifact, so CI collects the measurements with the service logs.

# metrics_init <repo-root>
metrics_init() {
    : "${RDD_METRICS_DIR:=${1}/rdd-logs/_diag/metrics}"
    export RDD_METRICS_DIR
    mkdir -p "${RDD_METRICS_DIR}"
}

# Milliseconds since the epoch.
metrics_now_ms() {
    # EPOCHREALTIME (bash 5) has microsecond resolution; BSD date does not
    # have %N, so older bash falls back to whole seconds.
    if [[ -n "${EPOCHREALTIME:-}" ]]; then
        echo $((${EPOCHREALTIME/[.,]/} / 1000))
    else
        echo $(($(date +%s) * 1000))
    fi
}

# metrics_header <name> <column>... Writes the header only for a new file.
metrics_header() {
    local name=$1
    shift
    [[ -n "${RDD_METRICS_DIR:-}" ]] || return 0
    local path="${RDD_METRICS_DIR}/${name}.tsv"
    [[ -e "${path}" ]] && return 0
    local IFS=$'\t'
    printf '%s\n' "$*" >>"${path}"
}

# metrics_record <name> <field>... Appends one TSV row.
metrics_record() {
    local name=$1
    shift
    [[ -n "${RDD_METRICS_DIR:-}" ]] || return 0
    local IFS=$'\t'
    printf '%s\n' "$*" >>"${RDD_METRICS_DIR}/${name}.tsv"
}

# metrics_phase <phase> <start-ms> One row for a phase of the current file.
metrics_phase() {
    local phase=$1 start_ms=$2 end_ms
    end_ms=$(metrics_now_ms)
    metrics_header phase file phase start_ms end_ms elapsed_ms
    metrics_record phase \
        "$(basename "${BATS_TEST_FILENAME:-unknown}")" "${phase}" \
        "${start_ms}" "${end_ms}" "$((end_ms - start_ms))"
}

# metrics_timeout <seconds> <command>... Runs the command with a time limit.
metrics_timeout() {
    local seconds=$1
    shift
    # GNU timeout is absent from a stock macOS, so run the command bare when
    # it is missing.
    if command -v timeout >/dev/null 2>&1; then
        timeout --kill-after=1 "${seconds}" "$@"
    else
        "$@"
    fi
}
