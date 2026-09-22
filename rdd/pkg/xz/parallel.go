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
	"hash/crc64"
	"io"
	"os"
	"runtime"

	"github.com/ulikunitz/xz/lzma"
	"golang.org/x/sync/errgroup"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/sparse"
)

// xz writes each block's sizes into its header, so a stream produced by
// "xz --threads" can be split and decoded block by block. errNotSplittable
// reports a stream this decoder cannot split, which sends the caller back to
// the sequential path rather than failing the decode.
var errNotSplittable = errors.New("xz stream is not splittable")

const (
	streamHeaderSize = 12
	streamFooterSize = 12
	lzma2FilterID    = 0x21
	sizesPresent     = 0xc0 // block flags: both compressed and uncompressed
	filterCountMask  = 0x03
	indexIndicator   = 0x00
)

var (
	streamHeaderMagic = []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}
	streamFooterMagic = []byte{'Y', 'Z'}
)

// crc64CheckID is the check type this decoder verifies. xz can also emit none,
// CRC32 and SHA-256, and a stream carrying one of those falls back to the
// sequential decoder: ulikunitz verifies whatever check a stream declares, so
// decoding those here would accept corruption it would have caught.
const crc64CheckID = 0x04

// crc64CheckSize is the length of the check that follows each block.
const crc64CheckSize = 8

// block describes one block of a multi-block xz stream.
type block struct {
	compOffset int64
	compSize   int64
	uncompOff  int64
	uncompSize int64
	dictCap    int
	check      []byte
	// unpaddedSize is the block's length without its padding, as the index
	// records it.
	unpaddedSize int64
}

// readerAtSizer is the random access the parallel path needs. The embedded
// distro reaches DecompressReader as a *strings.Reader, which provides it.
type readerAtSizer interface {
	io.ReaderAt
	Size() int64
}

// uvarint decodes an xz multibyte integer from b at *i. xz caps the encoding
// at nine bytes, which keeps every value below 2^63.
func uvarint(b []byte, i *int) (int64, error) {
	var v int64
	for shift := 0; ; shift += 7 {
		if *i >= len(b) || shift > 56 {
			return 0, errNotSplittable
		}
		c := b[*i]
		*i++
		v |= int64(c&0x7f) << shift
		if c&0x80 == 0 {
			return v, nil
		}
	}
}

// crc32Matches reports whether sum holds the little-endian CRC32 of data, as
// xz stores it in its headers, index and footer.
func crc32Matches(data, sum []byte) bool {
	return crc32.ChecksumIEEE(data) == binary.LittleEndian.Uint32(sum)
}

// allZero reports whether b holds only zero bytes, which xz requires of its
// padding.
func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

// parseBlocks walks the block headers of a single-stream xz file and checks
// them against the stream's index and footer. A second stream, stream padding,
// a block header without both sizes, more than one filter, or a filter other
// than LZMA2 returns errNotSplittable. So does damaged metadata, which the
// sequential decoder then reports.
func parseBlocks(r readerAtSizer) ([]block, error) {
	size := r.Size()
	header := make([]byte, streamHeaderSize)
	if _, err := r.ReadAt(header, 0); err != nil {
		return nil, errNotSplittable
	}
	flags := header[6:8]
	if !bytes.Equal(header[:6], streamHeaderMagic) ||
		!bytes.Equal(flags, []byte{0, crc64CheckID}) ||
		!crc32Matches(flags, header[8:]) {
		return nil, errNotSplittable
	}

	var blocks []block
	var uncompOff int64
	// A block header never exceeds 1024 bytes, so one read covers it.
	buf := make([]byte, 1024)
	off := int64(streamHeaderSize)
	for {
		n, err := r.ReadAt(buf, off)
		if n == 0 && err != nil {
			return nil, errNotSplittable
		}
		b := buf[:n]
		if b[0] == indexIndicator {
			break
		}
		headerSize := (int64(b[0]) + 1) * 4
		if headerSize > int64(len(b)) || !crc32Matches(b[:headerSize-4], b[headerSize-4:headerSize]) {
			return nil, errNotSplittable
		}
		b = b[:headerSize-4]
		blockFlags := b[1]
		if blockFlags&sizesPresent != sizesPresent || blockFlags&filterCountMask != 0 {
			return nil, errNotSplittable
		}
		i := 2
		compSize, err := uvarint(b, &i)
		if err != nil {
			return nil, err
		}
		uncompSize, err := uvarint(b, &i)
		if err != nil {
			return nil, err
		}
		filterID, err := uvarint(b, &i)
		if err != nil {
			return nil, err
		}
		if filterID != lzma2FilterID {
			return nil, errNotSplittable
		}
		if _, err := uvarint(b, &i); err != nil { // property length, always 1
			return nil, err
		}
		if i >= len(b) {
			return nil, errNotSplittable
		}
		bits := int(b[i] & 0x3f)
		if bits > 40 {
			return nil, errNotSplittable
		}

		dataOff := off + headerSize
		dataEnd := dataOff + compSize
		checkOff := (dataEnd + 3) &^ 3
		if checkOff+crc64CheckSize > size {
			return nil, errNotSplittable
		}
		// Zero padding takes the block data to a multiple of four bytes before
		// the check.
		tail := make([]byte, checkOff+crc64CheckSize-dataEnd)
		if _, err := r.ReadAt(tail, dataEnd); err != nil {
			return nil, errNotSplittable
		}
		padding, check := tail[:checkOff-dataEnd], tail[checkOff-dataEnd:]
		if !allZero(padding) {
			return nil, errNotSplittable
		}
		blocks = append(blocks, block{
			compOffset:   dataOff,
			compSize:     compSize,
			uncompOff:    uncompOff,
			uncompSize:   uncompSize,
			dictCap:      (2 | (bits & 1)) << (bits/2 + 11),
			check:        check,
			unpaddedSize: headerSize + compSize + crc64CheckSize,
		})
		uncompOff += uncompSize
		off = checkOff + crc64CheckSize
	}
	// One block decodes no faster in parallel, and the sequential path already
	// streams it without holding the compressed bytes in memory.
	if len(blocks) < 2 {
		return nil, errNotSplittable
	}
	if err := checkStreamEnd(r, off, flags, blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

// checkStreamEnd checks the index at off against blocks, and that the stream
// footer after the index ends the file. A second stream, stream padding or
// trailing data means the file does not end with this stream's footer, so each
// returns errNotSplittable.
func checkStreamEnd(r readerAtSizer, off int64, flags []byte, blocks []block) error {
	footerOff := r.Size() - streamFooterSize
	footer := make([]byte, streamFooterSize)
	if _, err := r.ReadAt(footer, footerOff); err != nil {
		return errNotSplittable
	}
	if !bytes.Equal(footer[10:], streamFooterMagic) ||
		!bytes.Equal(footer[8:10], flags) ||
		!crc32Matches(footer[4:10], footer[:4]) {
		return errNotSplittable
	}
	// The footer stores the index length in 4-byte units, less one.
	indexSize := (int64(binary.LittleEndian.Uint32(footer[4:8])) + 1) * 4
	if off+indexSize != footerOff {
		return errNotSplittable
	}
	index := make([]byte, indexSize)
	if _, err := r.ReadAt(index, off); err != nil {
		return errNotSplittable
	}
	body := index[:indexSize-4]
	if !crc32Matches(body, index[indexSize-4:]) {
		return errNotSplittable
	}
	i := 1 // past the index indicator
	count, err := uvarint(body, &i)
	if err != nil {
		return err
	}
	if count != int64(len(blocks)) {
		return errNotSplittable
	}
	for _, blk := range blocks {
		unpaddedSize, err := uvarint(body, &i)
		if err != nil {
			return err
		}
		uncompSize, err := uvarint(body, &i)
		if err != nil {
			return err
		}
		if unpaddedSize != blk.unpaddedSize || uncompSize != blk.uncompSize {
			return errNotSplittable
		}
	}
	// Zero padding takes the index to a multiple of four bytes before its CRC32.
	if (i+3)&^3 != len(body) || !allZero(body[i:]) {
		return errNotSplittable
	}
	return nil
}

// decodeBlock decompresses one block into dst and verifies its CRC64.
func decodeBlock(ctx context.Context, r io.ReaderAt, dst *os.File, blk block, checkTable *crc64.Table) error {
	comp := make([]byte, blk.compSize)
	if _, err := r.ReadAt(comp, blk.compOffset); err != nil {
		return fmt.Errorf("reading block at %d: %w", blk.uncompOff, err)
	}
	lr, err := lzma.Reader2Config{DictCap: blk.dictCap}.NewReader2(bytes.NewReader(comp))
	if err != nil {
		return fmt.Errorf("initializing block at %d: %w", blk.uncompOff, err)
	}

	w := sparse.NewWriterAt(dst, blk.uncompOff)
	digest := crc64.New(checkTable)
	n, err := io.Copy(io.MultiWriter(w, digest), &ctxReader{ctx: ctx, r: lr})
	if err != nil {
		return fmt.Errorf("decompressing block at %d: %w", blk.uncompOff, err)
	}
	if n != blk.uncompSize {
		return fmt.Errorf("block at %d decoded %d bytes, want %d", blk.uncompOff, n, blk.uncompSize)
	}
	if err := w.Finish(); err != nil {
		return err
	}
	// The check is stored little-endian, and parseBlocks accepts CRC64 alone,
	// so it is always eight bytes.
	var want uint64
	for i := 7; i >= 0; i-- {
		want = want<<8 | uint64(blk.check[i])
	}
	if digest.Sum64() != want {
		return fmt.Errorf("block at %d: CRC64 %#x, want %#x", blk.uncompOff, digest.Sum64(), want)
	}
	return nil
}

// decompressParallel decodes each block of r concurrently into the open file
// dst, and returns errNotSplittable when the stream cannot be split.
func decompressParallel(ctx context.Context, r readerAtSizer, dst *os.File) error {
	blocks, err := parseBlocks(r)
	if err != nil {
		return err
	}
	total := blocks[len(blocks)-1].uncompOff + blocks[len(blocks)-1].uncompSize
	// Give every worker its region up front: a hole punched past the end of
	// the file does nothing, and the blocks finish out of order.
	if err := dst.Truncate(total); err != nil {
		return err
	}

	// Each worker holds its block's compressed bytes and its own LZMA2
	// dictionary, so the worker count bounds memory as much as it bounds CPU.
	workers := min(runtime.NumCPU(), len(blocks))
	table := crc64.MakeTable(crc64.ECMA)
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(workers)
	for _, blk := range blocks {
		group.Go(func() error {
			return decodeBlock(groupCtx, r, dst, blk, table)
		})
	}
	return group.Wait()
}
