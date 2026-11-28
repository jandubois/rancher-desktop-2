// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestParseProbeOutput(t *testing.T) {
	const ipLine = "2: lima0    inet 192.168.105.3/24 brd 192.168.105.255 scope global dynamic lima0\\       valid_lft 86393sec preferred_lft 86393sec\n"
	for _, tc := range []struct {
		name string
		out  string
		want nodeStatus
	}{
		{"bootstrapped", ipLine + "bootstrapped\n", nodeStatus{address: "192.168.105.3", bootstrapped: true}},
		{"no marker", ipLine, nodeStatus{address: "192.168.105.3"}},
		{"empty", "", nodeStatus{}},
		{"IPv6", "2: lima0    inet fd00::3/64 scope global\n", nodeStatus{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, parseProbeOutput([]byte(tc.out)), tc.want)
		})
	}
}
