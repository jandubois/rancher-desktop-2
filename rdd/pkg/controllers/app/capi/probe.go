// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"bufio"
	"bytes"
	"net/netip"
	"slices"
	"strings"
)

// nodeStatus is what a probe reads from a running node VM.
type nodeStatus struct {
	// address is lima0's IPv4 address without its prefix length, or empty
	// when lima0 doesn't have one.
	address string
	// bootstrapped tells whether BootstrapMarker exists.
	bootstrapped bool
}

// parseProbeOutput reads the output of probeScript. It takes the node's
// address from the first line with an IPv4 address, and it ignores lines
// that it does not recognize.
func parseProbeOutput(out []byte) nodeStatus {
	var status nodeStatus
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "bootstrapped" {
			status.bootstrapped = true
			continue
		}
		if status.address != "" {
			continue
		}
		fields := strings.Fields(line)
		i := slices.Index(fields, "inet")
		if i < 0 || i+1 >= len(fields) {
			continue
		}
		prefix, err := netip.ParsePrefix(fields[i+1])
		if err == nil && prefix.Addr().Is4() {
			status.address = prefix.Addr().String()
		}
	}
	return status
}
