// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"context"
	"fmt"
	"time"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestexec"
)

// probeTimeout bounds each probe of a node VM over ssh.
const probeTimeout = 15 * time.Second

// probeScript prints lima0's IPv4 addresses and, when BootstrapMarker exists,
// the line "bootstrapped". It exits 0 either way.
const probeScript = "ip -4 -o addr show dev lima0 2>/dev/null; if [ -e " + BootstrapMarker + " ]; then echo bootstrapped; fi"

// probeNodeOverSSH runs probeScript in the node VM vmName. The returned
// error can quote the VM's Lima template, which holds Rancher's registration
// token.
func probeNodeOverSSH(ctx context.Context, vmName string) (nodeStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	inst, err := guestexec.Inspect(ctx, vmName)
	if err != nil {
		return nodeStatus{}, err
	}
	cmd, err := guestexec.Command(ctx, inst, probeScript, guestexec.Options{})
	if err != nil {
		return nodeStatus{}, err
	}
	out, err := cmd.Output()
	if err != nil {
		return nodeStatus{}, fmt.Errorf("probe node VM %s: %w", vmName, err)
	}
	return parseProbeOutput(out), nil
}
