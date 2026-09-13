// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

//go:build !darwin && !linux

package xz

import "os"

// punchHole does nothing outside macOS and Linux. On Windows, NTFS makes holes
// only in a file flagged sparse, and the decoder never sets that flag.
func punchHole(*os.File, int64, int64) {}
