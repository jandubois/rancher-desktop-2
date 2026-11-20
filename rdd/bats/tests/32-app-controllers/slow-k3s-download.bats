# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: SUSE LLC
# SPDX-FileCopyrightText: The Rancher Desktop Authors

load '../../helpers/load'

# Probe: on WSL, lima-template.yaml makes the lima-init boot run sleep 450
# seconds before its k3s download, so the run in the root network namespace
# gives up after its 30 attempts first. Check whether k3s still becomes ready.

local_setup_file() {
    rdd svc delete
    rdd set --wait=false running=true kubernetes.enabled=true kubernetes.version=1.32.0
}

@test "k3s becomes ready when the namespaced download outlasts the root-namespace retries" {
    rdd ctl wait --for=condition=KubernetesReady app/app --timeout=1500s
}
