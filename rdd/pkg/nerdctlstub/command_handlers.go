// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package nerdctlstub

// This file contains handlers for the positional arguments of specific
// commands, ported from Rancher Desktop 1.x.

import (
	"slices"
	"strings"
)

// builderBuildHandler handles `nerdctl builder build` and `builder debug`.
func builderBuildHandler(args []string) ([]string, error) {
	// nerdctl builder build [flags] PATH
	// The first argument is the directory to build; the rest are ignored.
	if len(args) < 1 {
		// nerdctl will report the missing argument.
		return args, nil
	}
	newPath, err := fileOrURLOrStdinArgHandler(args[0])
	if err != nil {
		return nil, err
	}
	return append([]string{newPath}, args[1:]...), nil
}

// copySpecKind classifies one `nerdctl container cp` positional argument.
type copySpecKind int

const (
	// copySpecStdio is "-", for stdin or stdout.
	copySpecStdio copySpecKind = iota
	// copySpecInvalid starts with a colon.
	copySpecInvalid
	// copySpecHost is a host path.
	copySpecHost
	// copySpecAmbiguous has one letter before the first colon, which can
	// name a drive, a container ID prefix or a compose service.
	copySpecAmbiguous
	// copySpecContainer is a container path (name:/path).
	copySpecContainer
)

// classifyCopySpec returns the kind of `cp` spec that p is, as the stub sees
// it on Windows.
func classifyCopySpec(p string) copySpecKind {
	colon := strings.Index(p, ":")
	switch {
	case p == "-":
		return copySpecStdio
	case colon == 0:
		return copySpecInvalid
	case colon < 0, strings.HasPrefix(p, "."), strings.HasPrefix(p, "/"), strings.HasPrefix(p, `\`):
		// Like nerdctl, take a rooted path, or a relative path starting
		// with `.` even with a colon in it, as local.
		return copySpecHost
	case windowsDrive.MatchString(p) && strings.Contains(p, `\`):
		// A container ID prefix or a compose service name may be one
		// letter, but a Linux file name rarely contains a backslash, so
		// a backslash marks this as a host path.
		return copySpecHost
	case windowsDrive.MatchString(p):
		return copySpecAmbiguous
	default:
		return copySpecContainer
	}
}

// containerCopyHandler handles `nerdctl container cp` and `compose cp`.
func containerCopyHandler(args []string) ([]string, error) {
	// Every positional argument of `nerdctl container cp` is a path, in the
	// container or on the host, or "-" for stdin or stdout. After a `--`, a
	// path may start with "-".
	if len(args) != 2 {
		// nerdctl will report the wrong count, or print help for --help.
		return args, nil
	}

	// A pair that this loop does not rewrite reaches nerdctl unchanged, so
	// nerdctl interprets it as it would without the stub.
	kinds := []copySpecKind{classifyCopySpec(args[0]), classifyCopySpec(args[1])}
	for i, kind := range kinds {
		other := kinds[1-i]
		if (kind == copySpecHost && (other == copySpecContainer || other == copySpecAmbiguous)) ||
			(kind == copySpecAmbiguous && other == copySpecContainer) {
			newPath, err := filePathArgHandler(args[i])
			if err != nil {
				return nil, err
			}
			paths := slices.Clone(args)
			paths[i] = newPath
			return paths, nil
		}
	}
	return args, nil
}
