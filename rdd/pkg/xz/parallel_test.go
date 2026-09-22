// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors
package xz

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	ulikunitz "github.com/ulikunitz/xz"
	"gotest.tools/v3/assert"
)

// multiblockFixture holds three blocks of multi-threaded xz output, whose
// headers record both sizes as the parallel path needs. It is committed rather
// than generated, because ulikunitz writes no sizes into its block headers and
// a test that skips without the xz CLI would never run on Windows CI.
func multiblockFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "multiblock.xz"))
	assert.NilError(t, err)
	return b
}

// paddedIndexFixture is a three-block stream that, unlike multiblockFixture,
// pads its index as well as its blocks.
func paddedIndexFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "multiblock-padded-index.xz"))
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

// indexOffset returns where the index of a single-stream file starts, which
// the footer's backward size gives.
func indexOffset(b []byte) int {
	footer := len(b) - streamFooterSize
	return footer - int(binary.LittleEndian.Uint32(b[footer+4:])+1)*4
}

// resealIndex recomputes the index CRC32 after an edit, so only the check on
// the edited field can catch it.
func resealIndex(b []byte) []byte {
	footer := len(b) - streamFooterSize
	binary.LittleEndian.PutUint32(b[footer-4:], crc32.ChecksumIEEE(b[indexOffset(b):footer-4]))
	return b
}

// resealFirstHeader does the same for the first block's header, which ends
// where that block's compressed data begins.
func resealFirstHeader(b []byte, first block) []byte {
	end := first.compOffset
	binary.LittleEndian.PutUint32(b[end-4:], crc32.ChecksumIEEE(b[streamHeaderSize:end-4]))
	return b
}

// firstProp returns the offset of the first block's LZMA2 property byte, which
// follows the header size, the flags, both sizes, the filter ID and the
// one-byte property length.
func firstProp(t *testing.T, b []byte) int {
	t.Helper()
	i := streamHeaderSize + 2
	for range 3 { // both sizes and the filter ID
		_, err := uvarint(b, &i)
		assert.NilError(t, err)
	}
	return i + 1
}

func TestUvarintStopsAtNineBytes(t *testing.T) {
	i := 0
	v, err := uvarint(append(bytes.Repeat([]byte{0xff}, 8), 0x7f), &i)
	assert.NilError(t, err)
	assert.Equal(t, v, int64(math.MaxInt64))

	// A tenth byte could set the sign bit and make a size negative.
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
	fixture := paddedIndexFixture(t)
	blocks, err := parseBlocks(bytes.NewReader(fixture))
	assert.NilError(t, err)
	footer := len(fixture) - streamFooterSize
	index := indexOffset(fixture)
	prop := firstProp(t, fixture)

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
		{"reserved block flag", func(b []byte) []byte { b[streamHeaderSize+1] |= 0x04; return resealFirstHeader(b, blocks[0]) }},
		{"filter property length", func(b []byte) []byte { b[prop-1] = 2; return resealFirstHeader(b, blocks[0]) }},
		{"two-byte property length", func(b []byte) []byte {
			// Spell the length 1 as the multibyte integer 0x81 0x00, which moves
			// the property byte into the padding.
			b[prop-1], b[prop], b[prop+1] = 0x81, 0x00, b[prop]
			return resealFirstHeader(b, blocks[0])
		}},
		{"reserved dictionary bits", func(b []byte) []byte { b[prop] |= 0x40; return resealFirstHeader(b, blocks[0]) }},
		{"block header padding", func(b []byte) []byte { b[prop+1] = 0x01; return resealFirstHeader(b, blocks[0]) }},
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

// The LZMA2 data must fill the compressed size its block header declares, as
// the sequential decoder requires. Growing that size by one claims a zero
// padding byte as data, which parseBlocks has no way to see.
func TestParallelRejectsBytesAfterLZMA2Data(t *testing.T) {
	fixture := paddedIndexFixture(t)
	blocks, err := parseBlocks(bytes.NewReader(fixture))
	assert.NilError(t, err)

	compressed := bytes.Clone(fixture)
	compressed[streamHeaderSize+2]++ // the low byte of the first compressed size
	resealFirstHeader(compressed, blocks[0])
	compressed[indexOffset(compressed)+2]++ // and of that block's unpadded size
	resealIndex(compressed)
	_, err = parseBlocks(bytes.NewReader(compressed))
	assert.NilError(t, err)

	_, err = decode(t, sequential(compressed))
	assert.Assert(t, err != nil, "the sequential decode accepted it")
	_, err = decode(t, bytes.NewReader(compressed))
	assert.ErrorContains(t, err, "data follows its LZMA2 end marker")
}

// Dictionary code 40 means 4 GiB - 1, a size parseBlocks cannot express, so
// the stream must take the sequential path. The test stops at parseBlocks,
// because decoding the stream allocates the whole dictionary.
func TestParallelFallsBackOn4GiBDictionary(t *testing.T) {
	fixture := paddedIndexFixture(t)
	blocks, err := parseBlocks(bytes.NewReader(fixture))
	assert.NilError(t, err)

	compressed := bytes.Clone(fixture)
	compressed[firstProp(t, compressed)] = 40
	_, err = parseBlocks(bytes.NewReader(resealFirstHeader(compressed, blocks[0])))
	assert.Assert(t, errors.Is(err, errNotSplittable), "got %v", err)
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

	want, err := io.ReadAll(mustSequential(t, compressed))
	assert.NilError(t, err)

	dst := filepath.Join(t.TempDir(), "crc32.raw")
	assert.NilError(t, DecompressReader(t.Context(), bytes.NewReader(compressed), dst))
	got, err := os.ReadFile(dst)
	assert.NilError(t, err)
	assert.Equal(t, bytes.Equal(got, want), true)

	// Flip the last byte of the last block's check, just before the index. The
	// compressed data still decodes, so only the sequential path's checksum
	// can refuse it.
	corrupt := bytes.Clone(compressed)
	corrupt[indexOffset(corrupt)-1] ^= 0xff
	err = DecompressReader(t.Context(), bytes.NewReader(corrupt), filepath.Join(t.TempDir(), "badcrc.raw"))
	assert.ErrorContains(t, err, "checksum error")
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
	assert.ErrorContains(t, err, fmt.Sprintf("block at %d", blocks[1].uncompOff))
}

// A canceled context must also abort the parallel decode.
func TestParallelCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	dst := filepath.Join(t.TempDir(), "canceled.raw")
	err := DecompressReader(ctx, bytes.NewReader(multiblockFixture(t)), dst)
	assert.Assert(t, errors.Is(err, context.Canceled), "got %v", err)
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
