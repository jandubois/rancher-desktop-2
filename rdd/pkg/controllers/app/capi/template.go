// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"encoding/base64"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lima-vm/lima/v2/pkg/limatype"
	"github.com/lima-vm/lima/v2/pkg/limatype/dirnames"
	"github.com/lima-vm/lima/v2/pkg/limatype/filenames"
	"github.com/lima-vm/lima/v2/pkg/limayaml"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/capi/v1alpha1"
)

// BootstrapMarker is the guest file that the guard script creates after the
// install script succeeds. It is outside /var/lib/rancher, where Rancher's
// system agent and k3s keep their state.
const BootstrapMarker = bootstrapDir + "/bootstrapped"

const bootstrapDir = "/var/lib/lima-capi"

const (
	defaultCPUs = 2
	// installScriptData is the guest file that holds the install script as
	// base64. Lima runs the content and script of every provision entry
	// through Go's text/template, and it logs the whole text when that fails.
	// Base64 text never contains "{", so text/template leaves the encoded
	// script unchanged and never fails on it, and Lima never logs the script,
	// which holds Rancher's token.
	installScriptData = installScriptPath + ".b64"
	// maxSocketPath is macOS's limit on a Unix socket path, terminator included.
	maxSocketPath = 104
)

var defaultMemory = resource.MustParse("3Gi")

// guardScript runs the install script on each boot until the install succeeds
// once. Lima runs system provision scripts on every boot, so the script exits
// early when BootstrapMarker exists. A failed install exits before it creates
// the marker, so the next boot tries again. Rancher's installer needs openssl,
// and the App's distro does not include it.
//
// The decoded script holds Rancher's token, so umask 077 keeps it readable by
// root only. The subshell keeps that umask away from the installer, which
// creates files that other users must be able to read.
const guardScript = `#!/bin/sh
set -eu
[ -e ` + BootstrapMarker + ` ] && exit 0
command -v openssl >/dev/null || zypper --non-interactive install --no-recommends openssl-3
(umask 077 && base64 -d ` + installScriptData + ` >` + installScriptPath + `)
sh ` + installScriptPath + `
mkdir -p ` + bootstrapDir + `
touch ` + BootstrapMarker + `
`

// LimaTemplate returns the Lima template for the node VM of a LimaMachine,
// running installScript on its first boot.
func LimaTemplate(spec v1alpha1.LimaMachineSpec, installScript []byte) ([]byte, error) {
	cpus := defaultCPUs
	if spec.CPUs != 0 {
		cpus = spec.CPUs
	}
	memory := defaultMemory
	if spec.Memory != nil {
		memory = *spec.Memory
	}
	y := limatype.LimaYAML{
		Images: []limatype.Image{{File: limatype.File{Location: "embedded:///distro.raw.xz"}}},
		VMType: new(limatype.VZ),
		CPUs:   &cpus,
		// Lima parses sizes with go-units, which takes a plain byte count.
		Memory: new(strconv.FormatInt(memory.Value(), 10)),
		Containerd: limatype.Containerd{
			System: new(false),
			User:   new(false),
		},
		Networks: []limatype.Network{{VZNAT: new(true), GuestToGuest: new(true)}},
		PortForwards: []limatype.PortForward{{
			GuestIP:           net.IPv4zero,
			GuestIPMustBeZero: new(false),
			Proto:             limatype.ProtoAny,
			Ignore:            true,
		}},
		Provision: []limatype.Provision{
			{
				Mode: limatype.ProvisionModeData,
				ProvisionData: limatype.ProvisionData{
					Path:        new(installScriptData),
					Permissions: new("0600"),
					Content:     new(base64.StdEncoding.EncodeToString(installScript)),
				},
			},
			{Mode: limatype.ProvisionModeSystem, Script: new(guardScript)},
		},
	}
	if spec.Disk != nil {
		y.Disk = new(strconv.FormatInt(spec.Disk.Value(), 10))
	}
	return limayaml.Marshal(&y, false)
}

// ValidateMachineName checks that a LimaMachine name works as the name of a
// Lima instance under limaHome, and that "lima-<name>" works as the guest's
// hostname.
func ValidateMachineName(limaHome, name string) error {
	if err := dirnames.ValidateInstName(name); err != nil {
		return err
	}
	// k3s names the Node after the guest's hostname, and the provider ID
	// assumes that hostname is "lima-<name>".
	if strings.ContainsAny(name, "._") {
		return fmt.Errorf("name %q must not contain \".\" or \"_\", because Lima replaces them with \"-\" in the guest's hostname", name)
	}
	if hostname := "lima-" + name; len(hostname) > 63 {
		return fmt.Errorf("hostname %q is longer than 63 characters", hostname)
	}
	// filenames.LongestSock (lima pkg/limatype/filenames/filenames.go:102-110)
	// is the longest socket name that Lima creates in an instance directory.
	// Lima checks the socket path the same way in pkg/instance/create.go:41-46,
	// but with the host's limit, which is 108 on Linux and Windows.
	if sock := filepath.Join(limaHome, name, filenames.LongestSock); len(sock) >= maxSocketPath {
		return fmt.Errorf("instance name %q is too long, because socket path %q must be shorter than %d characters", name, sock, maxSocketPath)
	}
	return nil
}
