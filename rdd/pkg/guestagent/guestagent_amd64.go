// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package guestagent

import _ "embed"

// guestAgentGZ is the gzipped Lima guest agent for an amd64 guest.
//
//go:embed lima-guestagent.amd64.gz
var guestAgentGZ []byte
