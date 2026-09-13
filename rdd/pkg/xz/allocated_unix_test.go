// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

//go:build unix

package xz

import (
	"syscall"
	"testing"

	"gotest.tools/v3/assert"
)

// allocatedBytes returns the disk space allocated to the file at path.
func allocatedBytes(t *testing.T, path string) (int64, bool) {
	t.Helper()
	var st syscall.Stat_t
	assert.NilError(t, syscall.Stat(path, &st))
	// Linux and macOS both count st_blocks in 512-byte units.
	return int64(st.Blocks) * 512, true
}
