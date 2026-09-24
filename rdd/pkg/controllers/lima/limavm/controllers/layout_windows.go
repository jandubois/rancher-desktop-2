//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import "os"

// allocatedBytes reports the file's size, because os.FileInfo does not
// expose a block count on Windows.
func allocatedBytes(info os.FileInfo, _ string) (int64, error) {
	return info.Size(), nil
}
