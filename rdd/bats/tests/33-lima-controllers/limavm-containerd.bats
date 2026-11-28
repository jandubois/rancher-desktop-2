# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: SUSE LLC
# SPDX-FileCopyrightText: The Rancher Desktop Authors

load '../../helpers/load'

# A LimaVM with containerd enabled must receive the nerdctl archive on its
# cidata disk, or Lima's 40-install-containerd.sh boot script fails at `tar`.
# The test uses a tiny fake archive instead of the real nerdctl-full download.
#
# The embedded distro ships its own nerdctl, so the boot script takes its
# upgrade path. That path runs containerd-rootless-setuptool.sh. The distro
# doesn't have it, and the script aborts before it extracts the archive. So
# the test reads the archive from the cidata disk instead of running the
# nerdctl that the script would install.
#
# On WSL2, Lima's driver runs boot.sh itself, and the failing boot script
# makes the hostagent stop. rdd then starts the VM again, and the script
# fails again, so the VM keeps restarting. That is why the test skips the
# wait for Running on Windows. Lima doesn't build a cidata disk for WSL2
# either. It writes the cidata files into a directory in the instance
# directory on the host, so the test reads the archive from there.

NAMESPACE="containerd-test-ns"
VM_NAME="test-containerd"
MARKER="fake-nerdctl-from-cidata"

local_setup_file() {
    setup_rdd_control_plane "lima"
    rdd ctl create namespace "${NAMESPACE}"
}

local_teardown_file() {
    if [[ -d "${RDD_LIMA_HOME}/${VM_NAME}" ]]; then
        rm -rf "${RDD_LIMA_HOME:?}/${VM_NAME}"
    fi
    # Use a timeout because wsl.exe --unregister can hang.
    if is_windows; then
        timeout 30 bash -c "MSYS_NO_PATHCONV=1 wsl.exe --unregister 'lima-${VM_NAME}'" 2>/dev/null || true
    fi
}

# Builds an archive whose bin/nerdctl prints the marker.
create_fake_nerdctl_archive() { # <archive>
    local root=${BATS_FILE_TMPDIR}/nerdctl-root
    mkdir -p "${root}/bin"
    printf '#!/bin/sh\necho %s\n' "${MARKER}" >"${root}/bin/nerdctl"
    chmod 755 "${root}/bin/nerdctl"
    # COPYFILE_DISABLE stops macOS tar from adding AppleDouble entries.
    COPYFILE_DISABLE=1 tar -C "${root}" -czf "$1" bin/nerdctl
}

assert_archive_on_cidata() {
    if is_windows; then
        run -0 tar -xOzf "${RDD_LIMA_HOME}/${VM_NAME}/cidata/nerdctl-full.tgz" bin/nerdctl
    else
        run -0 rdd lima shell "${VM_NAME}" \
            sudo tar -xOzf /mnt/lima-cidata/nerdctl-full.tgz bin/nerdctl
    fi
    assert_output --partial "${MARKER}"
}

@test "create template ConfigMap with containerd enabled" {
    archive=${BATS_FILE_TMPDIR}/nerdctl-fake.tar.gz
    create_fake_nerdctl_archive "${archive}"
    run -0 host_path "${archive}"
    containerd_body="  system: true
  user: false
  archives:
  - location: ${output}
    arch: ${ARCH}"
    rdd ctl create configmap "source-template" --namespace "${NAMESPACE}" \
        --from-literal="template=$(vm_template "${containerd_body}")"
}

@test "create running LimaVM" {
    rdd ctl apply -f - <<EOF
apiVersion: lima.rancherdesktop.io/v1alpha1
kind: LimaVM
metadata:
  name: ${VM_NAME}
  namespace: ${NAMESPACE}
spec:
  templateRef:
    name: source-template
    namespace: ${NAMESPACE}
  running: true
EOF
}

@test "wait for Running condition to become True/Started" {
    if is_windows; then
        skip "rdd restarts the VM after each failed boot on WSL2"
    fi
    rdd ctl wait-condition "limavm/${VM_NAME}" Running --namespace "${NAMESPACE}" \
        --reason=Started --timeout=300s
}

@test "the VM's cidata holds the nerdctl archive" {
    # Retry until the archive is in place. On VZ and QEMU it appears once the
    # guest mounts the cidata disk during boot. On WSL2 every hostagent start
    # deletes and rewrites the cidata directory, so the archive can be
    # missing for a moment.
    try --max 12 --delay 5 -- assert_archive_on_cidata
}

@test "stop the VM" {
    # On WSL2, the hostagent may still be importing the distro at this point.
    # If it is still running when the next test file's `rdd svc delete` shuts
    # down the service, the shutdown kills the hostagent but not its children.
    # The children inherit rdd's instance directory as their working
    # directory, and Windows can't delete a process's working directory, so
    # that `rdd svc delete` fails. Deleting the LimaVM doesn't prevent this,
    # because deletion kills the hostagent only when Lima reports the VM as
    # Running, and Lima doesn't report a distro that is still importing as
    # Running. A stop asks the hostagent to exit, and kills it together with
    # its children if it doesn't exit in time.
    rdd ctl patch limavm "${VM_NAME}" --namespace "${NAMESPACE}" \
        --type=merge --patch '{"spec":{"running":false}}'
    rdd ctl wait-condition "limavm/${VM_NAME}" Running=False --namespace "${NAMESPACE}" \
        --reason=Stopped --timeout=300s
}

@test "delete the LimaVM" {
    delete_limavm "${VM_NAME}" "${NAMESPACE}"
}
