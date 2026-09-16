// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package sparse

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gotest.tools/v3/assert"
)

// imageLike returns data, 4 MiB of zeros, more data, and 1 MiB of zeros, like a
// disk image with free space. Each data run ends partway into a 4 KiB block,
// and the trailing zeros check that the output keeps its full length.
func imageLike() []byte {
	data := bytes.Repeat([]byte("sparse writer fixture.\n"), 435)
	return slices.Concat(data, make([]byte, 4<<20), data, make([]byte, 1<<20))
}

// Writes that begin and end partway into a block must still go to the right
// offsets and leave the zero runs between them unallocated.
func TestWriterUnalignedWrites(t *testing.T) {
	want := imageLike()
	path := filepath.Join(t.TempDir(), "image")
	f, err := os.Create(path)
	assert.NilError(t, err)

	w := NewWriter(f)
	for chunk := range slices.Chunk(want, 1000) {
		_, writeErr := w.Write(chunk)
		assert.NilError(t, writeErr)
	}
	assert.NilError(t, w.Finish())
	// APFS counts the zeros it allocates in a gap only once the file is closed.
	assert.NilError(t, f.Close())

	got, err := os.ReadFile(path)
	assert.NilError(t, err)
	assert.Assert(t, bytes.Equal(got, want), "got %d bytes, want %d", len(got), len(want))
	if allocated, ok := allocatedBytes(t, path); ok {
		assert.Assert(t, allocated < int64(len(want))/8, "%d of %d bytes allocated", allocated, len(want))
	}
}
