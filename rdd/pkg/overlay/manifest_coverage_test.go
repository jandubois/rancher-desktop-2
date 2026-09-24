// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package overlay

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// TestManifestCoversMovedGuestPaths guards the move of the openSUSE guest assets
// (rancher-desktop-2#805). The overlay is now the only source of the files the
// distro's root/ tree used to carry, so a path dropped from overlay/manifest.yaml
// would be a guest that will not boot rather than a failing build. testdata holds
// the paths the root/ tree installed at the pre-cut commit; this asserts the
// shipped manifest still has an entry for each.
func TestManifestCoversMovedGuestPaths(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	assert.Assert(t, ok, "cannot locate the test file")
	pkgDir := filepath.Dir(testFile)

	m, err := LoadManifest(filepath.Join(pkgDir, "..", "..", "overlay", "manifest.yaml"))
	assert.NilError(t, err)

	entries := make(map[string]bool, len(m.Entries))
	for i := range m.Entries {
		entries[m.Entries[i].Path] = true
	}

	want := readGuestPaths(t, filepath.Join(pkgDir, "testdata", "opensuse-guest-paths.txt"))
	assert.Assert(t, len(want) > 0, "expected at least one guest path in testdata")

	var missing []string
	for _, p := range want {
		if !entries[p] {
			missing = append(missing, p)
		}
	}
	assert.Assert(t, len(missing) == 0,
		"overlay/manifest.yaml is missing entries for guest paths the openSUSE "+
			"root/ tree used to install:\n\t%s", strings.Join(missing, "\n\t"))
}

// readGuestPaths returns the non-comment, non-blank lines of the testdata file.
func readGuestPaths(t *testing.T, name string) []string {
	t.Helper()
	f, err := os.Open(name)
	assert.NilError(t, err)
	defer f.Close()

	var paths []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		paths = append(paths, line)
	}
	assert.NilError(t, scanner.Err())
	return paths
}
