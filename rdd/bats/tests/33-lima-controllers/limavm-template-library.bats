load '../../helpers/load'

# Template library tests. The LimaVM defaulter resolves `base:` references
# while it admits a LimaVM, so these tests need only the control plane and
# never boot a VM.

# non-functional template, but passes Lima validation
TEMPLATE='images: [{"location":"https://foo.test."}]'

PATH_ANNOTATION=lima.rancherdesktop.io/template-path

local_setup_file() {
    setup_rdd_control_plane "lima"
    rdd ctl create namespace "library-ns"
    rdd ctl wait --for=create ValidatingWebhookConfiguration "limavm-template-library-validator" --timeout=60s
}

# create_library_configmap creates a library ConfigMap whose template is TEMPLATE.
create_library_configmap() { # <namespace> <name> <path>
    rdd ctl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: $2
  namespace: $1
  labels:
    lima.rancherdesktop.io/template-library: "true"
  annotations:
    ${PATH_ANNOTATION}: $3
data:
  template: '${TEMPLATE}'
EOF
}

# stock_template_name prints the name of the managed ConfigMap holding the stock template at PATH.
stock_template_name() { # <path>
    run -0 --separate-stderr rdd ctl get configmap --namespace lima-templates \
        --selector app.kubernetes.io/managed-by=rdd-lima-templates --output json
    jq --exit-status --raw-output --arg path "$1" \
        '.items[] | select(.metadata.annotations["'"${PATH_ANNOTATION}"'"] == $path) | .metadata.name' <<<"${output}"
}

assert_stock_template_contains() { # <path> <text>
    run -0 stock_template_name "$1"
    run -0 rdd ctl get configmap "${output}" --namespace lima-templates --output 'jsonpath={.data.template}'
    assert_output --partial "$2"
}

assert_embedded_template() { # <vm> <namespace>
    run -0 rdd ctl get configmap "$1-template" --namespace "$2" --output 'jsonpath={.data.template}'
    assert_output --partial "https://foo.test."
    refute_output --partial "base:"
}

@test "rdd writes the stock templates into lima-templates" {
    try --max 30 --delay 1 -- assert_stock_template_contains default.yaml "template:_images/ubuntu"
}

@test "template:default resolves through its chain of stock base templates" {
    rdd lima create "stock-vm" "template:default" --dry-run
}

@test "a library ConfigMap in lima-templates is reachable as a template: name" {
    create_library_configmap lima-templates "my-image" "my-image.yaml"
    rdd lima create "named-vm" "template:my-image" --namespace "library-ns"
    assert_embedded_template "named-vm" "library-ns"
}

@test "a relative base resolves to a library ConfigMap in the template's namespace" {
    create_library_configmap "library-ns" "common" "common.yaml"
    rdd ctl create configmap "relative" --namespace "library-ns" --from-literal="template=base: common.yaml"
    rdd lima create "relative-vm" "relative" --namespace "library-ns"
    assert_embedded_template "relative-vm" "library-ns"
}

@test "a second library ConfigMap with the same path is rejected" {
    run -1 create_library_configmap "library-ns" "common-copy" "common.yaml"
    assert_output --partial "denied the request"
    assert_output --partial 'already has'
}

@test "a restart undoes an edit to a stock template" {
    run -0 stock_template_name default.yaml
    name=${output}
    rdd ctl patch configmap "${name}" --namespace lima-templates --type=merge \
        --patch='{"data":{"template":"cpus: 1\n"}}'
    run -1 assert_stock_template_contains default.yaml "template:_images/ubuntu"

    rdd svc stop
    rdd svc start
    try --max 30 --delay 1 -- assert_stock_template_contains default.yaml "template:_images/ubuntu"

    # The sync leaves unmanaged ConfigMaps alone.
    rdd ctl get configmap "my-image" --namespace lima-templates
}
