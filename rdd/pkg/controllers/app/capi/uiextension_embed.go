// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

//go:build with_uiextension && darwin

package capi

import (
	"embed"
	"io/fs"
)

//go:embed uiextension
var embeddedUIExtension embed.FS

func init() {
	files, err := fs.Sub(embeddedUIExtension, "uiextension")
	if err != nil {
		panic(err)
	}
	uiExtensionFiles = files
}
