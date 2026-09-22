// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors
package xz

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"math"
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

// decode runs DecompressReader and returns what it wrote.
func decode(t *testing.T, r io.Reader) ([]byte, error) {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "out.raw")
	if err := DecompressReader(t.Context(), r, dst); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(dst)
	assert.NilError(t, err)
	return b, nil
}

// sequential hides ReadAt, which keeps DecompressReader off the parallel path.
// Embedding *bytes.Reader instead would promote ReadAt and defeat it.
func sequential(compressed []byte) io.Reader {
	return struct{ io.Reader }{bytes.NewReader(compressed)}
}

func TestUvarintStopsAtNineBytes(t *testing.T) {
	i := 0
	v, err := uvarint(append(bytes.Repeat([]byte{0xff}, 8), 0x7f), &i)
	assert.NilError(t, err)
	assert.Equal(t, v, int64(math.MaxInt64))

	// A tenth byte could set the sign bit, and decodeBlock panics when make
	// gets a negative compressed size.
	i = 0
	v, err = uvarint(append(bytes.Repeat([]byte{0xff}, 9), 0x01), &i)
	assert.Assert(t, errors.Is(err, errNotSplittable), "got %d, %v", v, err)
}

func TestParallelMatchesSequential(t *testing.T) {
	compressed := multiblockFixture(t)

	blocks, err := parseBlocks(bytes.NewReader(compressed))
	assert.NilError(t, err)
	assert.Equal(t, len(blocks), 3)

	got, err := decode(t, bytes.NewReader(compressed))
	assert.NilError(t, err)
	want, err := decode(t, sequential(compressed))
	assert.NilError(t, err)
	assert.Equal(t, bytes.Equal(got, want), true, "parallel output differs from sequential")
}

// The parallel path takes only a file holding one intact stream. Each case
// alters the fixture outside its compressed data, where the per-block CRC64
// cannot see the change, so parseBlocks must decline it and DecompressReader
// must then match the sequential decode.
func TestParallelFallsBackUnlessOneIntactStream(t *testing.T) {
	// Unlike multiblock.xz, this fixture pads its index as well as its blocks.
	fixture, err := os.ReadFile(filepath.Join("testdata", "multiblock-padded-index.xz"))
	assert.NilError(t, err)
	blocks, err := parseBlocks(bytes.NewReader(fixture))
	assert.NilError(t, err)
	footer := len(fixture) - streamFooterSize
	// The footer's backward size gives the length of the index before it.
	index := footer - int(binary.LittleEndian.Uint32(fixture[footer+4:])+1)*4
	// resealIndex recomputes the index CRC32 after an edit, so only the check
	// on the edited field can catch it.
	resealIndex := func(b []byte) []byte {
		binary.LittleEndian.PutUint32(b[footer-4:], crc32.ChecksumIEEE(b[index:footer-4]))
		return b
	}

	for _, tc := range []struct {
		name   string
		mutate func(b []byte) []byte
	}{
		{"second stream", func(b []byte) []byte { return append(b, fixture...) }},
		{"stream padding", func(b []byte) []byte { return append(b, 0, 0, 0, 0) }},
		{"trailing data", func(b []byte) []byte { return append(b, "junk"...) }},
		{"stream header magic", func(b []byte) []byte { b[0] ^= 0x01; return b }},
		{"stream header checksum", func(b []byte) []byte { b[streamHeaderSize-1] ^= 0xff; return b }},
		{"block header checksum", func(b []byte) []byte { b[blocks[1].compOffset-1] ^= 0xff; return b }},
		{"block padding", func(b []byte) []byte { b[blocks[0].compOffset+blocks[0].compSize] ^= 0x01; return b }},
		{"index checksum", func(b []byte) []byte { b[footer-1] ^= 0xff; return b }},
		{"footer checksum", func(b []byte) []byte { b[footer] ^= 0xff; return b }},
		{"footer magic", func(b []byte) []byte { b[len(b)-1] ^= 0x01; return b }},
		{"record count", func(b []byte) []byte { b[index+1]--; return resealIndex(b) }},
		{"index record", func(b []byte) []byte {
			// Change the first block's uncompressed size.
			i := index + 1
			for range 2 { // the record count, then the first unpadded size
				_, err := uvarint(b, &i)
				assert.NilError(t, err)
			}
			b[i] ^= 0x01
			return resealIndex(b)
		}},
		{"index padding", func(b []byte) []byte { b[footer-5] ^= 0x01; return resealIndex(b) }},
		{"footer flags", func(b []byte) []byte {
			// Declare CRC32 in the footer and reseal it, so only the comparison
			// with the stream header can catch it.
			b[footer+9] = 0x01
			binary.LittleEndian.PutUint32(b[footer:], crc32.ChecksumIEEE(b[footer+4:footer+10]))
			return b
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compressed := tc.mutate(bytes.Clone(fixture))
			_, err := parseBlocks(bytes.NewReader(compressed))
			assert.Assert(t, errors.Is(err, errNotSplittable), "got %v", err)

			want, wantErr := decode(t, sequential(compressed))
			got, gotErr := decode(t, bytes.NewReader(compressed))
			assert.Equal(t, gotErr != nil, wantErr != nil, "parallel: %v; sequential: %v", gotErr, wantErr)
			assert.Assert(t, bytes.Equal(got, want), "decoded %d bytes, want %d", len(got), len(want))
		})
	}
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
