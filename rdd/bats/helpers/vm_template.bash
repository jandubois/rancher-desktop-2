# Lima VM template for BATS tests. It boots the distro image embedded in rdd,
# as the App's template does, so no test downloads one.
#
# Usage in test files:
#   VM_TEMPLATE=$(vm_template)
# Supports RDD_VM_TYPE on Unix (expands to `vmType: <value>` when set).

vm_template() {
    if is_windows; then
        cat <<'YAML'
vmType: wsl2
images:
- location: embedded:///distro.tar.xz
mountType: wsl2
containerd:
  system: false
  user: false
YAML
    else
        cat <<YAML
images:
- location: embedded:///distro.raw.xz
${RDD_VM_TYPE:+vmType: ${RDD_VM_TYPE}}
containerd:
  system: false
  user: false
YAML
    fi
}
