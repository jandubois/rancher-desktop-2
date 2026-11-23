# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: SUSE LLC
# SPDX-FileCopyrightText: The Rancher Desktop Authors

load '../../helpers/load'

# Cluster API infrastructure provider tests. Cluster API isn't installed in
# the App's cluster, so a minimal Machine CRD, a Machine and a bootstrap
# Secret stand in for it. The tests check that the provider installs its CRDs
# once the Machine CRD exists and boots a node VM for a LimaMachine, and that
# the node VM runs the install script once. The node VM stops with the App and
# goes away with its LimaMachine.

APP_NAME="app"
GUEST_NAMESPACE="default"
MACHINE="bats-node"

local_setup_file() {
    is_macos || skip "the Cluster API provider runs only on macOS"

    # Isolate ~/.kube/config so tests never touch the developer's real
    # kubeconfig. The service inherits KUBECONFIG, so export it before the
    # service starts.
    KUBECONFIG="${BATS_FILE_TMPDIR}/kube/config"
    export KUBECONFIG
    mkdir -p "${BATS_FILE_TMPDIR}/kube"

    rdd svc delete
    rdd set --wait=false running=true kubernetes.enabled=true
    # VM boot plus k3s startup can take several minutes on a slow CI runner.
    rdd ctl wait --for=condition=KubernetesReady app/"${APP_NAME}" --timeout=900s
}

guest_kubectl() {
    rdd kubectl --kubeconfig="${RDD_K3S_CONFIG}" "$@"
}

assert_provider_crds() {
    run_e -0 guest_kubectl get crd --selector=cluster.x-k8s.io/v1beta2 --output=name
    assert_output - <<'EOF'
customresourcedefinition.apiextensions.k8s.io/limaclusters.infrastructure.cluster.x-k8s.io
customresourcedefinition.apiextensions.k8s.io/limamachines.infrastructure.cluster.x-k8s.io
customresourcedefinition.apiextensions.k8s.io/limamachinetemplates.infrastructure.cluster.x-k8s.io
EOF
}

# The install script appends a line on each run.
assert_install_script_ran_once() {
    run_e -0 rdd limavm shell "${MACHINE}" cat /var/lib/bats-capi/install-runs
    assert_output "ran"
}

@test "the provider leaves a cluster without Cluster API alone" {
    # The log line proves that a reconcile ran while Kubernetes was ready.
    try --max 24 --delay 5 -- assert_file_contains "${RDD_STDERR_LOG}" \
        'Cluster API is not installed in the guest cluster'
    run_e -0 guest_kubectl get crd --selector=cluster.x-k8s.io/v1beta2 --output=name
    refute_output
}

@test "the provider installs its CRDs once Cluster API's Machine CRD exists" {
    guest_kubectl apply --filename=- <<'EOF'
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: machines.cluster.x-k8s.io
spec:
  group: cluster.x-k8s.io
  names:
    kind: Machine
    listKind: MachineList
    plural: machines
    singular: machine
  scope: Namespaced
  versions:
  - name: v1beta2
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        x-kubernetes-preserve-unknown-fields: true
EOF
    guest_kubectl wait --for=condition=Established crd/machines.cluster.x-k8s.io --timeout=60s
    # The provider looks for the Machine CRD every 30 seconds while idle.
    try --max 24 --delay 5 -- assert_provider_crds
    guest_kubectl wait --for=condition=Established \
        crd/limamachines.infrastructure.cluster.x-k8s.io --timeout=60s
}

@test "a LimaMachine boots a node VM with 1 CPU and 1 GiB" {
    # Bootstrap data in the shape that Rancher generates: a cloud-config that
    # writes a gzipped install script and runs it.
    script="${BATS_FILE_TMPDIR}/install.sh"
    cat >"${script}" <<'EOF'
mkdir -p /var/lib/bats-capi
echo ran >>/var/lib/bats-capi/install-runs
EOF
    gzip --stdout "${script}" >"${script}.gz"
    run -0 base64 <"${script}.gz"
    encoded=${output//$'\n'/}
    cloud_config="${BATS_FILE_TMPDIR}/cloud-config"
    cat >"${cloud_config}" <<EOF
#cloud-config
write_files:
- path: /usr/local/custom_script/install.sh
  encoding: gzip+b64
  content: ${encoded}
runcmd:
- sh /usr/local/custom_script/install.sh
EOF
    guest_kubectl create secret generic "${MACHINE}-bootstrap" \
        --namespace="${GUEST_NAMESPACE}" --from-file=value="${cloud_config}"

    guest_kubectl apply --filename=- <<EOF
apiVersion: cluster.x-k8s.io/v1beta2
kind: Machine
metadata:
  name: ${MACHINE}
  namespace: ${GUEST_NAMESPACE}
spec:
  clusterName: bats-cluster
  version: v1.32.0+k3s1
  bootstrap:
    dataSecretName: ${MACHINE}-bootstrap
EOF
    run_e -0 guest_kubectl get machine "${MACHINE}" --namespace="${GUEST_NAMESPACE}" \
        --output=jsonpath='{.metadata.uid}'
    machine_uid=${output}

    guest_kubectl apply --filename=- <<EOF
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: LimaMachine
metadata:
  name: ${MACHINE}
  namespace: ${GUEST_NAMESPACE}
  ownerReferences:
  - apiVersion: cluster.x-k8s.io/v1beta2
    kind: Machine
    name: ${MACHINE}
    uid: ${machine_uid}
spec:
  cpus: 1
  memory: 1Gi
EOF
    # The first boot decompresses the distro and installs openssl.
    guest_kubectl wait --for=condition=Ready limamachine/"${MACHINE}" \
        --namespace="${GUEST_NAMESPACE}" --timeout=900s

    run_e -0 rdd limavm shell "${MACHINE}" nproc
    assert_output "1"
    # The default node VM has 3 GiB, so this bound shows that the spec applied.
    # shellcheck disable=SC2016 # $2 is awk's field
    run_e -0 rdd limavm shell "${MACHINE}" awk '/^MemTotal:/ { print $2 }' /proc/meminfo
    assert_output_lt $((1048576 + 1))
}

@test "the LimaMachine reports the provisioned node" {
    run_e -0 guest_kubectl get limamachine "${MACHINE}" --namespace="${GUEST_NAMESPACE}" --output=json
    machine_json=${output}
    assert_jq_raw \
        '.status.initialization.provisioned' 'true' \
        '.spec.providerID' "k3s://lima-${MACHINE}" \
        '.status.addresses[] | select(.type == "Hostname") | .address' "lima-${MACHINE}" \
        "${machine_json}"

    run -0 jq --raw-output '.status.addresses[] | select(.type == "InternalIP") | .address' <<<"${machine_json}"
    assert_regex "${output}" '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$'
    address=${output}
    run_e -0 rdd limavm shell "${MACHINE}" ip -4 -o addr show dev lima0
    assert_output --partial "inet ${address}/"

    # The input ConfigMap holds the registration token, so rdd deletes it once
    # the node VM exists.
    run_e -0 rdd ctl get configmap "${MACHINE}-source" --namespace="${RDD_APP_NAMESPACE}" \
        --ignore-not-found --output=name
    refute_output
}

@test "the node VM ran the install script" {
    assert_install_script_ran_once
}

@test "stopping the App stops the node VM" {
    rdd set running=false
    rdd ctl wait --for=condition=Running=false limavm/"${MACHINE}" \
        --namespace="${RDD_APP_NAMESPACE}" --timeout=300s
}

@test "the install script does not run again when the node VM restarts" {
    rdd set --wait=false running=true
    rdd ctl wait --for=condition=KubernetesReady app/"${APP_NAME}" --timeout=900s
    # restartCount counts the times that the node VM reached Running. Lima
    # reports Running once the boot scripts finish, or once it stops waiting
    # for them. So at 2, the restarted VM has usually finished them.
    rdd ctl wait --for=jsonpath='{.status.restartCount}'=2 limavm/"${MACHINE}" \
        --namespace="${RDD_APP_NAMESPACE}" --timeout=600s
    assert_install_script_ran_once
}

@test "deleting the LimaMachine deletes its node VM" {
    guest_kubectl delete limamachine "${MACHINE}" --namespace="${GUEST_NAMESPACE}" --wait=false
    rdd ctl wait --for=delete limavm/"${MACHINE}" --namespace="${RDD_APP_NAMESPACE}" --timeout=300s
    guest_kubectl wait --for=delete limamachine/"${MACHINE}" \
        --namespace="${GUEST_NAMESPACE}" --timeout=60s
    assert_dir_not_exists "${RDD_LIMA_HOME}/${MACHINE}"
}
