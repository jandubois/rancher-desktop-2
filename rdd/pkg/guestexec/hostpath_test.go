// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package guestexec

import (
	"testing"

	"gotest.tools/v3/assert"
)

// withHostCwd pretends the host works in the given directory.
func withHostCwd(t *testing.T, cwd string) {
	t.Helper()
	saved := hostCwd
	hostCwd = func() (string, error) { return cwd, nil }
	t.Cleanup(func() { hostCwd = saved })
}

func TestTranslateHostPath(t *testing.T) {
	withHostCwd(t, `C:\work`)
	cases := []struct {
		arg  string
		want string
	}{
		{`C:\Users\jan\app`, "/mnt/c/Users/jan/app"},
		{`c:/foo/bar`, "/mnt/c/foo/bar"},
		{`C:\foo\..\bar\.\baz`, "/mnt/c/bar/baz"},
		{`D:\`, "/mnt/d/"},
		{`sub\dir`, "/mnt/c/work/sub/dir"},
		{`..\sibling`, "/mnt/c/sibling"},
		{`.`, "/mnt/c/work"},
		{`\\server\share\file`, "//server/share/file"},
		{`/already/posix`, "/already/posix"},
	}
	for _, tc := range cases {
		t.Run(tc.arg, func(t *testing.T) {
			got, err := TranslateHostPath(tc.arg)
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}
