// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package xz

import "testing"

// allocatedBytes returns false on Windows, because NTFS makes holes only in a
// file flagged sparse, and the decoder never sets that flag.
func allocatedBytes(*testing.T, string) (int64, bool) {
	return 0, false
}
