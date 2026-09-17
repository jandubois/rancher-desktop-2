// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package sparse

import (
	"os"

	"golang.org/x/sys/unix"
)

// punchHole deallocates length bytes of f at off where the file system
// supports it. APFS fills a short gap between written blocks with allocated
// zeros, so skipping a run of zeros is not enough there. HFS+ and exFAT reject
// the call with ENOTTY, which leaves the zeros allocated and the file intact.
func punchHole(f *os.File, off, length int64) {
	// golang.org/x/sys/unix does not define fpunchhole_t, but the first four
	// fields of Fstore_t have the same layout.
	_ = unix.FcntlFstore(f.Fd(), unix.F_PUNCHHOLE, &unix.Fstore_t{Offset: off, Length: length})
}
