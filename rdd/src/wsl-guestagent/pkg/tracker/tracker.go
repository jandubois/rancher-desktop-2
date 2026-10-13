// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package tracker implements a tracking mechanism to keep track
// of the ports during various container event types e.g start, stop
package tracker

import "github.com/rancher-sandbox/rancher-desktop/src/ports"

// Tracker is the interface that includes all the functions that
// are used to keep track of the port mappings plus NetTracker methods
// that are used to keep track of the network listener creation and removal.
type Tracker interface {
	// Get returns a portMap using the containerID as a lookup Key.
	Get(containerID string) ports.PortMap

	// Add adds a portMap to the storage using the containerID as a Key.
	// It replaces all existing portMappings, without attempting to unbind listeners,
	// so the caller is responsible for calling Remove first if necessary.
	Add(containerID string, portMapping ports.PortMap) error

	// Remove removes a portMap using the containerID as a key.
	Remove(containerID string) error

	// RemoveAll removes all the available portMappings in the storage.
	RemoveAll() error
}
