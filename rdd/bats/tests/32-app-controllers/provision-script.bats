# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: SUSE LLC
# SPDX-FileCopyrightText: The Rancher Desktop Authors

load '../../helpers/load'

# Tests for virtualMachine.provisionScript: the script runs as root on every
# VM start, before the container engine, and a failure does not stop the VM.
# The file boots the VM twice. The first boot runs a script that writes
# markers and then fails; the second follows clearing the script. The markers
# are under /run, so every boot starts without them.

VM_NAME="rd"
USER_PROVISION_SCRIPT="/etc/rancher-desktop/user-provision.sh"

# No shebang, so bash runs it. The DNS lookup retries because the network can
# come up a moment after the address is assigned. The script fails at the end,
# so one boot shows both what the script sees and what a failure does, and
# the file avoids a VM restart.
MARKER_SCRIPT=$(
    cat <<'EOF'
echo "${CONTAINER_ENGINE}" >/run/provision-engine
echo '{{.Names}}' >/run/provision-braces
for _ in $(seq 30); do
    getent hosts get.k3s.io >/run/provision-dns && break
    sleep 1
done
exit 3
EOF
)

guest() {
    rdd limavm shell "${VM_NAME}" sudo "$@"
}

local_setup_file() {
    setup_rdd_control_plane
}

@test "start the VM with a provisioning script that writes markers and fails" {
    rdd set running=true "virtualMachine.provisionScript=${MARKER_SCRIPT}"
}

@test "container engine starts after a failing provisioning script" {
    # This can't use guest(), because --per-try-timeout runs the command under
    # timeout(1), which cannot run a shell function.
    try --max 10 --delay 3 --per-try-timeout 30s \
        -- rdd limavm shell "${VM_NAME}" sudo docker info
}

@test "provisioning journal records the script's exit status" {
    run -0 guest journalctl --boot --unit=rancher-desktop-provision --no-pager
    assert_output --partial "User provisioning script failed with exit status 3"
}

@test "provisioning script sees the unit's environment" {
    run -0 guest cat /run/provision-engine
    assert_output "moby"
}

@test "Go template braces in the provisioning script reach the guest unchanged" {
    run -0 guest cat /run/provision-braces
    assert_output "{{.Names}}"
}

@test "provisioning script can resolve external names" {
    run -0 guest cat /run/provision-dns
    assert_output --partial "get.k3s.io"
}

@test "clearing the provisioning script empties the guest file" {
    rdd set "virtualMachine.provisionScript="
    run -0 guest cat "${USER_PROVISION_SCRIPT}"
    refute_output
}

@test "a cleared provisioning script no longer runs" {
    run -1 guest test -e /run/provision-engine
}
