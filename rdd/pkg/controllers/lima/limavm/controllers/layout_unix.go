//go:build unix

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"fmt"
	"os"
	"syscall"
)

// allocatedBytes reports the bytes a file occupies on disk, which is less
// than its size when the writer punched holes.
func allocatedBytes(info os.FileInfo, path string) (int64, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("no stat information for %q", path)
	}
	return st.Blocks * 512, nil
}
