# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: SUSE LLC
# SPDX-FileCopyrightText: The Rancher Desktop Authors

load '../../helpers/load'

# Containerd engine tests: verify that the engine controller mirrors
# containerd namespaces into ContainerNamespace resources. Tests build on
# each other in file order.

VM_NAME="rd"

local_setup_file() {
    if is_windows; then
        skip "containerd mirroring is not supported on Windows yet"
    fi
    rdd svc delete
    rdd set containerEngine.name=containerd running=true
    # Mirror resources live in App.spec.namespace. Override RDD_NAMESPACE
    # to whatever the App was created with so the test queries the same
    # namespace the engine controller uses, regardless of CRD defaults.
    run -0 rdd ctl get app app -o jsonpath='{.spec.namespace}'
    RDD_NAMESPACE=${output}
    export RDD_NAMESPACE
}

# nerdctl runs the given command inside the VM; host-side nerdctl wiring
# is out of scope in v1. sudo is required because non-root nerdctl
# insists on rootless mode, even with an explicit --address.
nerdctl() {
    rdd limavm shell "${VM_NAME}" sudo nerdctl \
        --address /run/k3s/containerd/containerd.sock "$@"
}

@test "containerd engine reports ContainerEngineReady with reason Connected" {
    rdd ctl wait --for=condition=ContainerEngineReady app/app --timeout=60s
    run -0 rdd ctl get app app \
        -o jsonpath='{.status.conditions[?(@.type=="ContainerEngineReady")].reason}'
    assert_output "Connected"
}

# limavm shell runs as the same unprivileged user as Lima's SSH forward, so
# reading the mode through it also proves the /run/k3s directories are
# traversable. 666 is the permissions drop-in's chmod; containerd itself
# creates the socket root-only.
assert_containerd_socket_open() {
    run -0 rdd limavm shell "${VM_NAME}" stat --format=%a /run/k3s/containerd/containerd.sock
    assert_output 666
}

@test "containerd socket is forwarded to the host" {
    # Wait for containerd to create the socket and the drop-in to open it up.
    try --max 10 --delay 3 -- assert_containerd_socket_open

    run -0 rdd svc paths containerd_socket
    socket_path=${output}
    assert_exists "${socket_path}"

    # containerd's gRPC server answers a plain-HTTP client with an HTTP/2
    # GOAWAY frame; --http0.9 lets curl accept those raw bytes and exit 0.
    # A broken forward or unreachable guest socket exits nonzero instead.
    curl --unix-socket "${socket_path}" --http0.9 --max-time 5 --silent \
        --output /dev/null http://localhost/
}

# --- Namespace lifecycle ---

@test "creating a containerd namespace creates its mirror" {
    # An empty namespace has nothing to sync, so the mirror can only come
    # from the namespace event itself.
    nerdctl namespace create mirror-ns

    rdd ctl wait --for=create --namespace="${RDD_NAMESPACE}" \
        ContainerNamespace/mirror-ns --timeout=30s
}

@test "removing a containerd namespace removes its mirror" {
    # wait --for=delete passes immediately for an object that never existed,
    # so prove the mirror is there before removing the namespace.
    rdd ctl get --namespace="${RDD_NAMESPACE}" ContainerNamespace/mirror-ns

    nerdctl namespace remove mirror-ns

    rdd ctl wait --for=delete --namespace="${RDD_NAMESPACE}" \
        ContainerNamespace/mirror-ns --timeout=30s
}
