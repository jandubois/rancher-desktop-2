// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package managedlines

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

const (
	startMarker = "# TEST BLOCK START"
	endMarker   = "# TEST BLOCK END"
)

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	assert.NilError(t, err)
	return string(data)
}

func TestManageAddsBlockToNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	err := Manage(path, startMarker, endMarker, []string{"export PATH=x"}, true)
	assert.NilError(t, err)
	assert.Equal(t, read(t, path), startMarker+"\nexport PATH=x\n"+endMarker+"\n")
}

func TestManagePreservesSurroundingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	assert.NilError(t, os.WriteFile(path, []byte("before\n\nafter\n"), 0o644))
	err := Manage(path, startMarker, endMarker, []string{"line"}, true)
	assert.NilError(t, err)
	assert.Equal(t, read(t, path), "before\n\nafter\n\n"+startMarker+"\nline\n"+endMarker+"\n")
}

func TestManageIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	assert.NilError(t, os.WriteFile(path, []byte("keep\n"), 0o600))
	assert.NilError(t, Manage(path, startMarker, endMarker, []string{"line"}, true))
	first := read(t, path)
	info1, err := os.Stat(path)
	assert.NilError(t, err)
	assert.NilError(t, Manage(path, startMarker, endMarker, []string{"line"}, true))
	assert.Equal(t, read(t, path), first)
	info2, err := os.Stat(path)
	assert.NilError(t, err)
	// A no-op must not rewrite the file (mtime unchanged) or alter its mode.
	assert.Equal(t, info1.ModTime(), info2.ModTime())
	assert.Equal(t, info1.Mode(), info2.Mode())
}

func TestManageUpdatesExistingBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	assert.NilError(t, os.WriteFile(path,
		[]byte("keep\n"+startMarker+"\nold\n"+endMarker+"\ntail\n"), 0o644))
	err := Manage(path, startMarker, endMarker, []string{"new"}, true)
	assert.NilError(t, err)
	assert.Equal(t, read(t, path), "keep\n"+startMarker+"\nnew\n"+endMarker+"\ntail\n")
}

func TestManageRemovesBlockKeepingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	assert.NilError(t, os.WriteFile(path,
		[]byte("keep\n"+startMarker+"\nline\n"+endMarker+"\ntail\n"), 0o644))
	err := Manage(path, startMarker, endMarker, nil, false)
	assert.NilError(t, err)
	assert.Equal(t, read(t, path), "keep\ntail\n")
}

func TestManageEmptiesFileInsteadOfDeleting(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bash_profile")
	assert.NilError(t, os.WriteFile(path,
		[]byte(startMarker+"\nline\n"+endMarker+"\n"), 0o644))
	err := Manage(path, startMarker, endMarker, nil, false)
	assert.NilError(t, err)
	// The file stays put (it may be one the user already had); it's just emptied.
	assert.Equal(t, read(t, path), "")
}

func TestManageRemoveOnMissingFileIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bash_profile")
	err := Manage(path, startMarker, endMarker, nil, false)
	assert.NilError(t, err)
	_, statErr := os.Stat(path)
	assert.Assert(t, os.IsNotExist(statErr))
}

func TestManageDanglingMarkerErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	assert.NilError(t, os.WriteFile(path, []byte("keep\n"+startMarker+"\nline\n"), 0o644))
	err := Manage(path, startMarker, endMarker, []string{"x"}, true)
	assert.ErrorContains(t, err, "exactly one of the delimiter lines")
	// The error names the offending file so the user can find it.
	assert.ErrorContains(t, err, path)
}

// TestManageCollapsesDuplicateBlock checks that a file that ended up with our
// block twice is collapsed to one in a single pass, keeping the content between
// the blocks. Without collapsing, the first block matches what we want, so Manage
// would report no change and leave the directory on PATH twice forever.
func TestManageCollapsesDuplicateBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	block := startMarker + "\nexport PATH=x\n" + endMarker + "\n"
	assert.NilError(t, os.WriteFile(path, []byte("keep\n"+block+"mid\n"+block+"tail\n"), 0o644))

	assert.NilError(t, Manage(path, startMarker, endMarker, []string{"export PATH=x"}, true))
	// One block survives, at the first block's position, and the user's own
	// content (keep/mid/tail) is preserved.
	want := "keep\n" + startMarker + "\nexport PATH=x\n" + endMarker + "\nmid\ntail\n"
	assert.Equal(t, read(t, path), want)

	// Now a fixed point: re-applying changes nothing.
	before := read(t, path)
	assert.NilError(t, Manage(path, startMarker, endMarker, []string{"export PATH=x"}, true))
	assert.Equal(t, read(t, path), before)
}

// TestManageRemovesDuplicateBlocks checks that removal takes out every copy, not
// just the first.
func TestManageRemovesDuplicateBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	block := startMarker + "\nexport PATH=x\n" + endMarker + "\n"
	assert.NilError(t, os.WriteFile(path, []byte("keep\n"+block+block+"tail\n"), 0o644))

	assert.NilError(t, Manage(path, startMarker, endMarker, nil, false))
	assert.Equal(t, read(t, path), "keep\ntail\n")
}
