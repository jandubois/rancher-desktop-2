// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package guestexec

// This file rewrites Windows host paths to the /mnt/<drive> paths where
// the WSL2 guest automounts each drive. The functions parse plain strings
// instead of using path/filepath, whose rules follow the build platform,
// so they behave the same everywhere and their tests run on every
// platform. Callers apply them only on Windows.

import (
	"os"
	"path"
	"strings"
)

// hostCwd returns the host working directory used to resolve relative
// paths; tests replace it.
var hostCwd = os.Getwd

// TranslateHostPath rewrites one host path to where the guest mounts it,
// e.g. `C:\Users\jan\app` to `/mnt/c/Users/jan/app`. A relative path
// resolves against the host working directory. UNC and POSIX-style
// absolute paths only get their backslashes turned into forward slashes.
func TranslateHostPath(arg string) (string, error) {
	slashed := strings.ReplaceAll(arg, `\`, "/")
	if drive, rest, ok := splitDrive(slashed); ok {
		return "/mnt/" + drive + path.Clean("/"+rest), nil
	}
	if strings.HasPrefix(slashed, "/") {
		// UNC (//server/share) or POSIX-style; nothing we can map.
		return slashed, nil
	}
	// A relative path resolves against the working directory.
	cwd, err := hostCwd()
	if err != nil {
		return "", err
	}
	base, err := TranslateHostPath(cwd)
	if err != nil {
		return "", err
	}
	return path.Join(base, slashed), nil
}

// splitDrive splits `c:/Users/jan` into "c" and "/Users/jan". Only
// drive-absolute paths match; drive-relative ones (`c:foo`) do not.
func splitDrive(slashed string) (drive, rest string, ok bool) {
	if len(slashed) < 3 || slashed[1] != ':' || slashed[2] != '/' {
		return "", "", false
	}
	letter := slashed[0]
	if (letter < 'a' || letter > 'z') && (letter < 'A' || letter > 'Z') {
		return "", "", false
	}
	return strings.ToLower(slashed[:1]), slashed[2:], true
}
