# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: SUSE LLC
# SPDX-FileCopyrightText: The Rancher Desktop Authors

load '../../helpers/load'

# Test the `rdd set` command for App configuration.
# Tests run sequentially and reuse the App across tests to minimize
# App creations (each triggers a ~200MB VM image copy). Avoids
# setting running=true to keep tests fast, and passes --wait=false
# on every set so the command returns as soon as the patch is
# accepted: this suite runs with only the app controller (no lima,
# no engine), so the App reconciler fails to start and Settled is
# never written. The real wait semantics are covered by
# engine-docker.bats.

APP_NAME="app"

delete_app() {
    rdd ctl delete app "${APP_NAME}" --ignore-not-found
    rdd ctl wait --for=delete app/"${APP_NAME}" --timeout=30s 2>/dev/null || true
}

get_app_field() {
    rdd ctl get app "${APP_NAME}" -o jsonpath="{$1}"
}

local_setup_file() {
    setup_rdd_control_plane "app"
    delete_app
}

# --- Help (no App needed) ---

@test "rdd set --help shows usage and available properties" {
    run -0 rdd set --help
    assert_output --partial "PROPERTY=VALUE"
    assert_output --partial "Available properties:"
    assert_output --partial "running"
    assert_output --partial "containerEngine.name"
    assert_output --partial "kubernetes.enabled"
    assert_output --partial "kubernetes.version"
    refute_output --partial "namespace"
}

# --- ctl wait-condition (no App needed) ---

# The CRD stands in for app/app, which is also cluster-scoped but never
# gets conditions in this suite.
@test "ctl wait-condition waits on a cluster-scoped resource" {
    rdd ctl wait-condition \
        customresourcedefinition/apps.app.rancherdesktop.io \
        Established --timeout=10s
}

@test "ctl wait-condition resolves a resource short name" {
    rdd ctl wait-condition crd/apps.app.rancherdesktop.io Established --timeout=10s
}

# --- Error cases (no App mutation, validated before API call) ---

@test "rdd set rejects invalid arguments" {
    run -1 rdd set namespace=test
    assert_output --partial "unknown property"

    run -1 rdd set running
    assert_output --partial "expected PROPERTY=VALUE"

    run -1 rdd set nonexistent=value
    assert_output --partial "unknown property"
    assert_output --partial "valid properties"

    run -1 rdd set containerEngine=moby
    assert_output --partial "is an object"
    assert_output --partial "containerEngine.name"

    run -1 rdd set =value
    assert_output --partial "property name must not be empty"

    run -1 rdd set running=yes
    assert_output --partial "expected boolean"

    run -1 rdd set running=
    assert_output --partial "expected boolean"

    run -1 rdd set containerEngine.name=
    assert_output --partial "valid values"

    run -1 rdd set containerEngine.name=docker
    assert_output --partial "valid values"
    assert_output --partial "moby"
    assert_output --partial "containerd"
}

# --- Create + defaults (1 App creation) ---

@test "rdd set creates App with defaults" {
    rdd set --wait=false running=false

    run -0 get_app_field '.metadata.name'
    assert_output "${APP_NAME}"

    run -0 get_app_field '.spec.containerEngine.name'
    assert_output "moby"
}

# --- Update: reuses App from previous test ---

@test "rdd set updates App and preserves unchanged fields" {
    run -0 get_app_field '.spec.containerEngine.name'
    refute_output "containerd"

    rdd set --wait=false containerEngine.name=containerd

    run -0 get_app_field '.spec.containerEngine.name'
    assert_output "containerd"

    # running should still be false (unchanged).
    run -0 get_app_field '.spec.running'
    assert_output "false"

    run -0 get_app_field '.spec.kubernetes.enabled'
    refute_output "true"

    run -0 get_app_field '.spec.kubernetes.version'
    refute_output "1.32.2"

    rdd set --wait=false kubernetes.enabled=true kubernetes.version=1.32.2

    # containerEngine.name should still be containerd.
    run -0 get_app_field '.spec.containerEngine.name'
    assert_output "containerd"

    run -0 get_app_field '.spec.kubernetes.enabled'
    assert_output "true"

    run -0 get_app_field '.spec.kubernetes.version'
    assert_output "1.32.2"
}

# --- Clear: reuses App from previous test ---

@test "rdd set clears string field with empty value" {
    run -0 get_app_field '.spec.kubernetes.version'
    assert_output "1.32.2"

    # Clear both together: webhook rejects an empty version when enabled=true.
    rdd set --wait=false kubernetes.enabled=false kubernetes.version=

    run -0 get_app_field '.spec.kubernetes.version'
    assert_output ""
}

# --- Dry-run: reuses App from previous tests ---

@test "rdd set --dry-run validates without persisting" {
    rdd set --dry-run containerEngine.name=moby

    # Value should not have changed.
    run -0 get_app_field '.spec.containerEngine.name'
    assert_output "containerd"
}

@test "rdd set --dry-run rejects invalid enum value" {
    run -1 rdd set --dry-run containerEngine.name=docker
    assert_output --partial "valid values"
}

# --- Wait timeout: reuses App from previous tests ---

@test "rdd set exits with the timeout code when the wait expires" {
    # Settled never turns True in this suite, so the deadline fires after
    # the watch has synced.
    run -4 rdd set --timeout=3s running=false
    assert_output --partial "timed out waiting for App state"
}

# --- Create with specified values (1 App creation) ---

@test "rdd set creates App with specified values" {
    delete_app
    rdd set --wait=false running=false containerEngine.name=containerd

    run -0 get_app_field '.spec.containerEngine.name'
    assert_output "containerd"
}

# --- Dry-run create (1 App creation) ---

@test "rdd set --dry-run creates App if absent" {
    delete_app
    rdd set --dry-run running=false

    # App should exist (created with defaults).
    run -0 get_app_field '.metadata.name'
    assert_output "${APP_NAME}"
}

# --- addPath (#1052): unset default, rdd set pins manual, CEL blocks going back ---

@test "rdd set stores addPath=manual when creating an App without naming it" {
    delete_app
    rdd set --wait=false running=false

    # The field defaults to unset, but rdd set pins a CLI-created instance to
    # manual so it never gets the app's first-run dialog.
    run -0 get_app_field '.spec.application.addPath'
    assert_output "manual"
}

@test "rdd run stores addPath=manual when it creates the App" {
    delete_app
    # rdd run has no --wait flag and this suite starts no VM, so timeout ends
    # the wait after rdd run has stored the App.
    run -124 timeout 10 rdd run true
    run -0 get_app_field '.spec.application.addPath'
    assert_output "manual"
}

@test "rdd set pins addPath=manual on an existing App that is still unset" {
    delete_app
    # Create the App without addPath, as the GUI does.
    rdd ctl apply -f - <<EOF
apiVersion: app.rancherdesktop.io/v1alpha1
kind: App
metadata:
  name: ${APP_NAME}
spec:
  running: false
EOF
    run -0 get_app_field '.spec.application.addPath'
    assert_output "unset"

    rdd set --wait=false running=false
    run -0 get_app_field '.spec.application.addPath'
    assert_output "manual"
}

@test "rdd run pins addPath=manual on an existing App that is still unset" {
    delete_app
    # Create the App without addPath, as the GUI does.
    rdd ctl apply -f - <<EOF
apiVersion: app.rancherdesktop.io/v1alpha1
kind: App
metadata:
  name: ${APP_NAME}
spec:
  running: false
EOF
    run -0 get_app_field '.spec.application.addPath'
    assert_output "unset"

    # A started App's settings are final, so rdd run pins addPath like rdd set.
    # timeout ends rdd run's wait, as in the creation test above.
    run -124 timeout 10 rdd run true
    run -0 get_app_field '.spec.application.addPath'
    assert_output "manual"
}

@test "rdd set leaves an existing addPath=front alone" {
    delete_app
    rdd set --wait=false running=false application.addPath=front
    run -0 get_app_field '.spec.application.addPath'
    assert_output "front"

    # A later set that does not name addPath must not overwrite front with manual.
    rdd set --wait=false running=false
    run -0 get_app_field '.spec.application.addPath'
    assert_output "front"
}

@test "the API server rejects changing addPath back to unset" {
    delete_app
    rdd set --wait=false running=false application.addPath=front
    run -0 get_app_field '.spec.application.addPath'
    assert_output "front"

    # Exit code 3 is cliexit.CodeRejected; the CEL rule forbids front -> unset.
    run -3 rdd set --wait=false application.addPath=unset
    assert_output --partial "addPath cannot be changed back to unset"

    # The rejected request leaves the value unchanged.
    run -0 get_app_field '.spec.application.addPath'
    assert_output "front"
}

@test "rdd stop leaves an existing unset App's addPath alone" {
    delete_app
    # Create the App without addPath, as the GUI does.
    rdd ctl apply -f - <<YAML
apiVersion: app.rancherdesktop.io/v1alpha1
kind: App
metadata:
  name: ${APP_NAME}
spec:
  running: false
YAML
    run -0 get_app_field '.spec.application.addPath'
    assert_output "unset"

    # rdd stop must not pin addPath, so the GUI's first-run signal survives.
    rdd stop --wait=false
    run -0 get_app_field '.spec.application.addPath'
    assert_output "unset"
}
