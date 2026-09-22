// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package sparse writes a stream to a file without allocating its runs of
// zeros, so a disk image arrives with its reserved free space still free on
// disk. Only macOS and Linux can punch the holes; elsewhere the zeros are
// written and the file comes out fully allocated.
package sparse

import (
	"bytes"
	"os"
)

// blockSize matches the default 4 KiB allocation block of APFS, ext4, XFS, and
// btrfs, the smallest hole any of them can make.
const blockSize = 4 << 10

var zeroBlock [blockSize]byte

// Writer writes to f, skipping each block of zeros and punching a hole over
// each whole one where punchHole can. Neither an xz stream nor a byte copy
// records where the holes were, so writing every byte would allocate a disk
// image's free space along with its data.
type Writer struct {
	f       *os.File
	off     int64 // offset in f of the next byte to write
	dataEnd int64 // offset in f just past the last data written
	region  bool  // fills part of f, so Finish leaves its length alone
}

// NewWriter returns a Writer filling f, which must be empty. Call Finish before
// closing f, so the zeros at the end of the stream reach it.
func NewWriter(f *os.File) *Writer {
	return &Writer{f: f}
}

// NewWriterAt returns a Writer filling f from off. Finish punches the zeros at
// the end of what it wrote but leaves f's length alone, so several Writers can
// fill separate regions of one file at once and the caller sets the length.
// Each region must already read as zeros.
func NewWriterAt(f *os.File, off int64) *Writer {
	return &Writer{f: f, off: off, dataEnd: off, region: true}
}

func (w *Writer) Write(p []byte) (int, error) {
	for i := 0; i < len(p); {
		data := w.runEnd(p, i, false)
		if data > i {
			if err := w.writeData(p[i:data], w.off+int64(i)); err != nil {
				return i, err
			}
		}
		i = w.runEnd(p, data, true)
	}
	w.off += int64(len(p))
	return len(p), nil
}

// writeData writes data at off in f, then punches a hole over the whole
// blocks of zeros skipped since the previous data.
func (w *Writer) writeData(data []byte, off int64) error {
	if _, err := w.f.WriteAt(data, off); err != nil {
		return err
	}
	w.punchSkipped(off)
	w.dataEnd = off + int64(len(data))
	return nil
}

// punchSkipped punches a hole over the whole blocks of zeros between the
// previous data and end.
func (w *Writer) punchSkipped(end int64) {
	holeStart := (w.dataEnd + blockSize - 1) / blockSize * blockSize
	holeEnd := end / blockSize * blockSize
	if holeEnd > holeStart {
		punchHole(w.f, holeStart, holeEnd-holeStart)
	}
}

// runEnd returns the index in p where the run starting at i ends. The run
// holds blocks of zeros if zero is true, and blocks of data otherwise. Blocks
// end at multiples of blockSize in f, so a block of zeros split across two
// Write calls is still skipped and punched.
func (w *Writer) runEnd(p []byte, i int, zero bool) int {
	for i < len(p) {
		end := min(len(p), i+int(blockSize-(w.off+int64(i))%blockSize))
		if bytes.Equal(p[i:end], zeroBlock[:end-i]) != zero {
			break
		}
		i = end
	}
	return i
}

// Finish extends f over the zeros skipped at the end of the stream, if any,
// and punches a hole over their whole blocks, because APFS allocates a short
// extension. A Writer from NewWriterAt leaves f's length alone.
func (w *Writer) Finish() error {
	if !w.region {
		if err := w.f.Truncate(w.off); err != nil {
			return err
		}
	}
	w.punchSkipped(w.off)
	return nil
}
