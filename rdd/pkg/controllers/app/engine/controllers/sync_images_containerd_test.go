// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"k8s.io/apimachinery/pkg/util/validation"
)

func TestContainerdImageMirrorName(t *testing.T) {
	t.Run("always hashes, since refs are never valid object names", func(t *testing.T) {
		got := containerdImageMirrorName("default", "docker.io/library/busybox:latest")
		assert.Assert(t, strings.HasPrefix(got, "img-"))
		assert.Equal(t, len(validation.IsDNS1123Subdomain(got)), 0)
	})

	t.Run("distinguishes the same ref in different namespaces", func(t *testing.T) {
		ref := "docker.io/library/busybox:latest"
		assert.Assert(t, containerdImageMirrorName("default", ref) != containerdImageMirrorName("k8s.io", ref))
	})

	t.Run("separates the namespace from the ref", func(t *testing.T) {
		// Concatenated without a separator both of these are "abx".
		// We specifically do not test what the exact separator is.
		assert.Assert(t, containerdImageMirrorName("a", "bx") != containerdImageMirrorName("ab", "x"))
	})

	t.Run("is stable across calls", func(t *testing.T) {
		ref := "docker.io/library/busybox:latest"
		assert.Equal(t, containerdImageMirrorName("ns", ref), containerdImageMirrorName("ns", ref))
	})
}

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
