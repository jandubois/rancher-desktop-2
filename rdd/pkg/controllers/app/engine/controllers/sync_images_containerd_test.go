// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestContainerdImageRefs(t *testing.T) {
	// The three names containerd's CRI plugin registers for one pull, in the
	// order image_pull.go writes them.
	const (
		configDigest = "sha256:1b7c1c6bd9a34ff7bfa4e6b62c1bd83bd9c8f0f7e8b0e2ab6a26e7f6e4b7c1a2"
		repoTag      = "docker.io/rancher/mirrored-pause:3.6"
		repoDigest   = "docker.io/rancher/mirrored-pause@sha256:74bf6fc6be13c4ec53a86a5acf9fdbc6787b176db0693659ad6ac89f115e182c"
	)

	tests := []struct {
		name       string
		record     string
		wantTag    string
		wantDigest string
	}{
		{"a config digest carries no reference", configDigest, "", ""},
		{"a repo tag is a tag", repoTag, repoTag, ""},
		{"a repo digest is a digest", repoDigest, "", repoDigest},
		{"a short tag is a tag", "busybox:latest", "busybox:latest", ""},
		{"an untagged name is a tag", "docker.io/library/busybox", "docker.io/library/busybox", ""},
		// nerdctl accepts a name containerd stores verbatim; anything the
		// reference parser rejects is still reported rather than dropped.
		{"an unparsable name is a tag", "NOT A REFERENCE", "NOT A REFERENCE", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotTag, gotDigest := containerdImageRefs(tc.record)
			assert.Equal(t, gotTag, tc.wantTag)
			assert.Equal(t, gotDigest, tc.wantDigest)
		})
	}
}
