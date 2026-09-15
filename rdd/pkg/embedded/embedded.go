// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package embedded holds the guest distro image baked into bin/rdd and
// bin/lima-controller.
//
// `make build-rdd` and `make build-lima-controller` stage the image in this
// directory and build with the with_distro tag. Other builds, such as go test
// and lint, need no image.
package embedded

// Distro is the xz-compressed guest distro image, either the raw disk image
// that Lima's vz and qemu drivers boot or, on Windows, the rootfs tarball that
// WSL2 imports. It is empty in a build without the with_distro tag.
var Distro string
