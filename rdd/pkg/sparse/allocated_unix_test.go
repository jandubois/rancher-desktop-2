// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

//go:build unix

package sparse

import (
	"syscall"
	"testing"

	"gotest.tools/v3/assert"
)

// allocatedBytes returns the disk space allocated to the file at path.
//
// pkg/xz keeps its own copy, to assert that DecompressReader still writes
// through this package. Two of them beat a test-helper package that nothing
// else would use; a third caller is when to extract it.
func allocatedBytes(t *testing.T, path string) (int64, bool) {
	t.Helper()
	var st syscall.Stat_t
	assert.NilError(t, syscall.Stat(path, &st))
	// Linux and macOS both count st_blocks in 512-byte units.
	return int64(st.Blocks) * 512, true
}
