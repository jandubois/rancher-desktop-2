// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package limatemplates provides Lima's stock templates. The rdd Makefile copies
// them out of the pinned Lima module.
package limatemplates

import (
	"embed"
	"io/fs"
)

// templates holds the copied tree. The "all:" prefix keeps the _images and
// _default directories, which go:embed would otherwise skip.
//
//go:embed all:templates
var templates embed.FS

// FS returns the stock templates, rooted at Lima's templates directory, so a
// template's path matches its `template:` name plus the extension, e.g.
// "_images/ubuntu-24.04.yaml".
func FS() fs.FS {
	sub, err := fs.Sub(templates, "templates")
	if err != nil {
		// fs.Sub fails only for an invalid directory name.
		panic(err)
	}
	return sub
}
