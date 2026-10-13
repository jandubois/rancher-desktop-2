// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package vsock provides constants and utilities for vsock communication between host and VM.
package vsock

// vsock constants used for handshake and signaling between host and VM.
const (
	SignaturePhrase = "github.com/rancher-sandbox/rancher-desktop-opensuse/src/go/networking"
	ReadySignal     = "READY"
)

// Default vsock ports for communication between host and VM. The VM dials the
// host switch's listen port; both sides use the same handshake port.
const (
	DefaultHandshakePort = 6670
	DefaultListenPort    = 6657
)
