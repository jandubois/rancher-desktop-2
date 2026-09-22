// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package api_test

import (
	"testing"

	"gotest.tools/v3/assert"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/util/api"
)

func TestMirrorName(t *testing.T) {
	runTest := func(name, input, expected string) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			actual := api.MirrorName("zz", input)
			assert.Equal(t, actual, expected)
		})
	}

	runTest("simple names do not need encoding", "hello", "hello")
	runTest("invalid name is encoded", "invalid_name",
		"zz-f123fd210ff35283833e7e27b312d585673b493a056396475c216d2891c235eb")
	runTest("encoded-looking names are re-encoded",
		"zz-0000000000000000000000000000000000000000000000000000000000000000",
		"zz-d5e794ce4cbeebf77541e17e0c4d1837cc4f8dbdbf776307e2c4075f3b0dd7da")
	runTest("encoded name with wrong prefix not encoded",
		"zzz-0000000000000000000000000000000000000000000000000000000000000000",
		"zzz-0000000000000000000000000000000000000000000000000000000000000000")
	runTest("encoded name with suffix not encoded",
		"zz-0000000000000000000000000000000000000000000000000000000000000000-suffix",
		"zz-0000000000000000000000000000000000000000000000000000000000000000-suffix")

	t.Run("extra arguments are incorporated", func(t *testing.T) {
		withoutExtra := api.MirrorName("zz", "invalid_name")
		withExtra := api.MirrorName("zz", "invalid_name", "extra1", "extra2")
		assert.Assert(t, withoutExtra != withExtra)
	})

	t.Run("extra arguments are separated", func(t *testing.T) {
		one := api.MirrorName("zz", "aaabbb", "ccc")
		two := api.MirrorName("zz", "aaa", "bbbccc")
		assert.Assert(t, one != two, "%s <-> %s", one, two)
	})
}
