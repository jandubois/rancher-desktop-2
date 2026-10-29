// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package nerdctlstub

// This file contains handlers for the positional arguments of specific
// commands, ported from Rancher Desktop 1.x.

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
