// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors
package xz

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	ulikunitz "github.com/ulikunitz/xz"
	"gotest.tools/v3/assert"
)

// multiblockFixture is xz --threads output: three blocks whose headers carry
// both sizes, which is what the parallel path needs. It is committed rather
// than generated, because no Go encoder produces a multi-block stream and a
// test that skips without the xz CLI would never run on Windows CI.
func multiblockFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "multiblock.xz"))
	assert.NilError(t, err)
	return b
}

func TestParallelMatchesSequential(t *testing.T) {
	compressed := multiblockFixture(t)

	blocks, err := parseBlocks(bytes.NewReader(compressed))
	assert.NilError(t, err)
	assert.Equal(t, len(blocks), 3)

	parallel := filepath.Join(t.TempDir(), "parallel.raw")
	assert.NilError(t, DecompressReader(t.Context(), bytes.NewReader(compressed), parallel))

	// An io.Reader with no ReaderAt keeps DecompressReader on the sequential
	// path, so this decodes the same bytes the other way.
	sequential := filepath.Join(t.TempDir(), "sequential.raw")
	assert.NilError(t, DecompressReader(t.Context(), struct{ *bytes.Reader }{bytes.NewReader(compressed)}, sequential))

	want, err := os.ReadFile(sequential)
	assert.NilError(t, err)
	got, err := os.ReadFile(parallel)
	assert.NilError(t, err)
	assert.Equal(t, bytes.Equal(got, want), true, "parallel output differs from sequential")
}

func TestParallelLeavesHoles(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "holes.raw")
	assert.NilError(t, DecompressReader(t.Context(), bytes.NewReader(multiblockFixture(t)), dst))

	info, err := os.Stat(dst)
	assert.NilError(t, err)
	allocated, supported := allocatedBytes(t, dst)
	if !supported {
		t.Skip("no block count on this platform")
	}
	assert.Assert(t, allocated < info.Size(),
		"allocated %d is not less than size %d", allocated, info.Size())
}

func TestParallelFallsBackOnSingleBlock(t *testing.T) {
	// The Go encoder writes one block and omits the sizes, so this is both of
	// the shapes the parallel path declines.
	var buf bytes.Buffer
	w, err := ulikunitz.NewWriter(&buf)
	assert.NilError(t, err)
	_, err = w.Write(samplePlaintext())
	assert.NilError(t, err)
	assert.NilError(t, w.Close())

	_, err = parseBlocks(bytes.NewReader(buf.Bytes()))
	assert.Assert(t, errors.Is(err, errNotSplittable), "got %v", err)

	// The caller still decodes it, through the sequential path.
	dst := filepath.Join(t.TempDir(), "single.raw")
	assert.NilError(t, DecompressReader(t.Context(), bytes.NewReader(buf.Bytes()), dst))
	got, err := os.ReadFile(dst)
	assert.NilError(t, err)
	assert.Equal(t, bytes.Equal(got, samplePlaintext()), true)
}

// A stream whose blocks carry any check but CRC64 must take the sequential
// path. That decoder verifies whatever check the stream declares, and this one
// verifies CRC64 alone, so splitting a CRC32 stream would accept corruption the
// decoder it replaces catches.
func TestParallelFallsBackOnNonCRC64Check(t *testing.T) {
	compressed, err := os.ReadFile(filepath.Join("testdata", "multiblock-crc32.xz"))
	assert.NilError(t, err)

	// The stream is splittable in every other way: three blocks, both sizes.
	_, err = parseBlocks(bytes.NewReader(compressed))
	assert.Assert(t, errors.Is(err, errNotSplittable), "got %v", err)

	// Corrupting the stored check leaves the compressed data decodable, so only
	// a checksum can catch it. The sequential path must still refuse it.
	want, err := io.ReadAll(mustSequential(t, compressed))
	assert.NilError(t, err)

	dst := filepath.Join(t.TempDir(), "crc32.raw")
	assert.NilError(t, DecompressReader(t.Context(), bytes.NewReader(compressed), dst))
	got, err := os.ReadFile(dst)
	assert.NilError(t, err)
	assert.Equal(t, bytes.Equal(got, want), true)
}

// mustSequential decodes through ulikunitz, which is what the fallback uses.
func mustSequential(t *testing.T, compressed []byte) io.Reader {
	t.Helper()
	r, err := ulikunitz.NewReader(bytes.NewReader(compressed))
	assert.NilError(t, err)
	return r
}

func TestParallelDetectsCorruptBlock(t *testing.T) {
	compressed := multiblockFixture(t)
	blocks, err := parseBlocks(bytes.NewReader(compressed))
	assert.NilError(t, err)

	// Flip a byte inside the second block so the first block still decodes and
	// the failure has to come from the second.
	corrupt := bytes.Clone(compressed)
	corrupt[blocks[1].compOffset+64] ^= 0xff

	err = DecompressReader(t.Context(), bytes.NewReader(corrupt), filepath.Join(t.TempDir(), "corrupt.raw"))
	assert.Assert(t, err != nil, "corrupt block decoded without error")
}

func TestParallelChecksCRC(t *testing.T) {
	compressed := multiblockFixture(t)
	blocks, err := parseBlocks(bytes.NewReader(compressed))
	assert.NilError(t, err)

	// Corrupt the stored check rather than the compressed data, which the LZMA
	// decoder rejects on its own. Only the CRC comparison can catch this.
	corrupt := bytes.Clone(compressed)
	blk := blocks[1]
	checkOffset := (blk.compOffset + blk.compSize + 3) &^ 3
	corrupt[checkOffset] ^= 0xff

	err = DecompressReader(t.Context(), bytes.NewReader(corrupt), filepath.Join(t.TempDir(), "badcrc.raw"))
	assert.ErrorContains(t, err, "CRC64")
}

func TestParallelRejectsTruncated(t *testing.T) {
	compressed := multiblockFixture(t)
	_, err := parseBlocks(bytes.NewReader(compressed[:len(compressed)/2]))
	assert.Assert(t, errors.Is(err, errNotSplittable), "got %v", err)
}
