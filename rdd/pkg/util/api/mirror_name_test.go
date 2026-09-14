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
	testCases := map[string]string{
		"hello":        "hello",
		"invalid_name": "zz-f123fd210ff35283833e7e27b312d585673b493a056396475c216d2891c235eb",
	}
	for input, expected := range testCases {
		t.Run(input, func(t *testing.T) {
			actual := api.MirrorName("zz", input)
			assert.Equal(t, actual, expected)
		})
	}
}
