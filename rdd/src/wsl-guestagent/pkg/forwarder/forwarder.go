// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package forwarder implements a forwarding mechanism to forward
// port mappings over the network.
package forwarder

import (
	"github.com/rancher-sandbox/rancher-desktop/src/ports"
)

// Forwarder is the interface that wraps the Send method which
// to forward the port mappings.
type Forwarder interface {
	// Send sends the give port mappings to the Peer via
	// a tcp connection.
	Send(portMapping ports.PortMapping) error
}
