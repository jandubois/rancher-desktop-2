#!/bin/bash

# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: SUSE LLC
# SPDX-FileCopyrightText: The Rancher Desktop Authors

# Restore the stock templates that Lima keeps as symlinks, such as
# _images/ubuntu.yaml, in a copy of the Lima module's templates directory.
# Go module zips omit symlinks, so the links are read from the module's git
# commit, and each one is written as a regular file holding a copy of its final
# target. The content always comes from the copy, and go.sum verifies the copy.
# The commit only supplies the link names and targets.
#
# Usage: restore-lima-template-aliases.sh TEMPLATES_COPY_DIR
# Run it from the directory holding go.mod.

set -o errexit -o nounset -o pipefail

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 TEMPLATES_COPY_DIR" >&2
    exit 1
fi
copy=$1
lima=github.com/lima-vm/lima/v2

die() {
    echo "$0: $*" >&2
    exit 1
}

# json_value KEY JSON prints the string value of KEY in go's indented JSON.
json_value() {
    sed -n -E "s/^[[:space:]]*\"$1\": \"([^\"]*)\".*/\\1/p" <<<"$2"
}

module=$(go list -m -f '{{with .Replace}}{{.Path}}@{{.Version}}{{else}}{{.Path}}@{{.Version}}{{end}}' "${lima}" | tr -d '\r')
[[ ${module} != *@ ]] || die "${lima} is replaced by a local directory (${module%@}); its template symlinks cannot be restored"
# go reports a failed download in the JSON's "Error" field on stdout.
if ! json=$(go mod download -json "${module}"); then
    die "go mod download failed for ${module}: ${json}"
fi
json=${json//$'\r'/}
vcs=$(json_value VCS "${json}")
url=$(json_value URL "${json}")
hash=$(json_value Hash "${json}")
[[ ${vcs} == git && -n ${url} && -n ${hash} ]] || die "go mod download does not report a git origin for ${module}"

tmp=$(mktemp -d)
trap 'rm -rf "${tmp:?}"' EXIT
git init --quiet "${tmp}"
git -C "${tmp}" fetch --quiet --depth=1 --filter=blob:none "${url}" "${hash}"

# normalize PATH prints PATH with its "." and ".." components resolved, and
# fails when ".." climbs above the repository root.
normalize() {
    local IFS=/
    local part
    local -a split
    local -a parts=()
    read -r -a split <<<"$1"
    for part in "${split[@]}"; do
        case ${part} in
        '' | .) ;;
        ..)
            [[ ${#parts[@]} -gt 0 ]] || die "a symlink points outside the repository: $1"
            unset 'parts[${#parts[@]}-1]'
            ;;
        *) parts+=("${part}") ;;
        esac
    done
    echo "${parts[*]}"
}

# resolve PATH follows the symlink chain at PATH in the fetched commit and
# prints the path of the regular file it ends at.
resolve() {
    local path=$1
    local hops
    local entry
    local blob
    local target
    for ((hops = 0; hops < 40; hops++)); do
        entry=$(git -C "${tmp}" ls-tree FETCH_HEAD -- "${path}")
        case ${entry} in
        "100644 blob "* | "100755 blob "*)
            echo "${path}"
            return
            ;;
        "120000 blob "*)
            blob=${entry%%$'\t'*}
            blob=${blob##* }
            target=$(git -C "${tmp}" cat-file -p "${blob}")
            path=$(normalize "${path%/*}/${target}")
            ;;
        *) die "${path} is not a file in ${module}" ;;
        esac
    done
    die "too many levels of symlinks at $1"
}

# Each link's own path must be under templates/ and must not exist in the copy
# yet. The module zip drops every symlink, so a link from the module's own
# commit is never at a path that the copy already holds.
links=$(git -C "${tmp}" ls-tree -r FETCH_HEAD -- templates/ | sed -n -E $'s/^120000 blob [0-9a-f]+\t//p')
while IFS= read -r link; do
    [[ -n ${link} ]] || continue
    alias=$(normalize "${link}")
    [[ ${alias} == templates/* ]] || die "symlink ${link} is outside templates/"
    [[ ! -e "${copy}/${alias#templates/}" && ! -L "${copy}/${alias#templates/}" ]] || die "symlink ${link} would overwrite a file in ${copy}"
    final=$(resolve "${link}")
    [[ ${final} == templates/* ]] || die "${link} points outside templates/: ${final}"
    [[ -f "${copy}/${final#templates/}" ]] || die "${link} points to ${final}, which is not in ${copy}"
    cp "${copy}/${final#templates/}" "${copy}/${alias#templates/}"
done <<<"${links}"
