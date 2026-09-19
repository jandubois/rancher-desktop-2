// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package xz

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc64"
	"io"
	"os"
	"runtime"

	"github.com/ulikunitz/xz/lzma"
	"golang.org/x/sync/errgroup"
)

// xz writes each block's sizes into its header, so a stream produced by
// "xz --threads" can be split and decoded block by block. errNotSplittable
// reports a stream this decoder cannot split, which sends the caller back to
// the sequential path rather than failing the decode.
var errNotSplittable = errors.New("xz stream is not splittable")

const (
	streamHeaderSize = 12
	lzma2FilterID    = 0x21
	sizesPresent     = 0xc0 // block flags: both compressed and uncompressed
	filterCountMask  = 0x03
	indexIndicator   = 0x00
)

// checkSizes maps an xz check-type ID to the length of the check that follows
// each block. The IDs xz can emit are none, CRC32, CRC64 and SHA-256.
var checkSizes = map[byte]int{0: 0, 1: 4, 4: 8, 10: 32}

// block describes one block of a multi-block xz stream.
type block struct {
	compOffset int64
	compSize   int64
	uncompOff  int64
	uncompSize int64
	dictCap    int
	check      []byte
}

// readerAtSizer is the random access the parallel path needs. The embedded
// distro reaches DecompressReader as a *strings.Reader, which provides it.
type readerAtSizer interface {
	io.ReaderAt
	Size() int64
}

// uvarint decodes an xz multibyte integer from b at *i.
func uvarint(b []byte, i *int) (int64, error) {
	var v int64
	for shift := 0; ; shift += 7 {
		if *i >= len(b) || shift > 63 {
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

// parseBlocks walks the block headers of a single-stream xz file. It returns
// errNotSplittable for anything this decoder will not split: a second stream,
// stream padding, a header without both sizes, more than one filter, or a
// filter other than LZMA2.
func parseBlocks(r readerAtSizer) ([]block, error) {
	size := r.Size()
	header := make([]byte, streamHeaderSize)
	if _, err := r.ReadAt(header, 0); err != nil {
		return nil, errNotSplittable
	}
	checkSize, ok := checkSizes[header[7]&0x0f]
	if !ok {
		return nil, errNotSplittable
	}

	var blocks []block
	var uncompOff int64
	// A block header never exceeds 1024 bytes, so one read covers it.
	buf := make([]byte, 1024)
	for off := int64(streamHeaderSize); ; {
		n, err := r.ReadAt(buf, off)
		if n == 0 && err != nil {
			return nil, errNotSplittable
		}
		b := buf[:n]
		if b[0] == indexIndicator {
			// The index ends the stream. Anything after the stream footer is
			// a second stream or stream padding, which this decoder skips.
			break
		}
		headerSize := (int64(b[0]) + 1) * 4
		flags := b[1]
		if flags&sizesPresent != sizesPresent || flags&filterCountMask != 0 {
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
		checkOff := (dataOff + compSize + 3) &^ 3
		if checkOff+int64(checkSize) > size {
			return nil, errNotSplittable
		}
		check := make([]byte, checkSize)
		if checkSize > 0 {
			if _, err := r.ReadAt(check, checkOff); err != nil {
				return nil, errNotSplittable
			}
		}
		blocks = append(blocks, block{
			compOffset: dataOff,
			compSize:   compSize,
			uncompOff:  uncompOff,
			uncompSize: uncompSize,
			dictCap:    (2 | (bits & 1)) << (bits/2 + 11),
			check:      check,
		})
		uncompOff += uncompSize
		off = checkOff + int64(checkSize)
	}
	// One block decodes no faster in parallel, and the sequential path already
	// streams it without holding the compressed bytes in memory.
	if len(blocks) < 2 {
		return nil, errNotSplittable
	}
	return blocks, nil
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

	w := newSparseWriterAt(dst, blk.uncompOff)
	var hash io.Writer = io.Discard
	digest := crc64.New(checkTable)
	if checkTable != nil && len(blk.check) == 8 {
		hash = digest
	}
	n, err := io.Copy(io.MultiWriter(w, hash), &ctxReader{ctx: ctx, r: lr})
	if err != nil {
		return fmt.Errorf("decompressing block at %d: %w", blk.uncompOff, err)
	}
	if n != blk.uncompSize {
		return fmt.Errorf("block at %d decoded %d bytes, want %d", blk.uncompOff, n, blk.uncompSize)
	}
	if err := w.finish(); err != nil {
		return err
	}
	if hash != io.Discard {
		var want uint64
		for i := 7; i >= 0; i-- {
			want = want<<8 | uint64(blk.check[i])
		}
		if digest.Sum64() != want {
			return fmt.Errorf("block at %d: CRC64 %#x, want %#x", blk.uncompOff, digest.Sum64(), want)
		}
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

	// Each worker holds its own LZMA2 dictionary, 64 MiB for this image, so
	// the worker count bounds memory as much as it bounds CPU.
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
