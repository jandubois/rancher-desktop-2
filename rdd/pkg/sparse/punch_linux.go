// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package sparse

import (
	"os"

	"golang.org/x/sys/unix"
)

// punchHole deallocates length bytes of f at off where the file system
// supports it. XFS reserves space past the end of a growing file, and a later
// write beyond a skipped run leaves the reserved blocks in the run allocated
// as unwritten extents, so skipping is not enough there. ext4 and btrfs leave
// the run a hole either way. Where the call fails, the zeros stay allocated
// and the file intact.
func punchHole(f *os.File, off, length int64) {
	_ = unix.Fallocate(int(f.Fd()), unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE, off, length)
}
