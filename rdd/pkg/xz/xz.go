// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package xz decompresses xz streams in-process with a pure-Go decoder, so
// the limavm controller can extract the distro image embedded in its binary
// on any host.
package xz

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	ulikunitz "github.com/ulikunitz/xz"
)

// bufSize is the input read buffer size. ulikunitz/xz issues many small reads;
// without a large buffer wrapping the source it spends most of its time in
// syscalls.
const bufSize = 4 << 20

// Decompress streams xz-compressed data from in to out, aborting with the
// context error if ctx is cancelled mid-decode.
func Decompress(ctx context.Context, in io.Reader, out io.Writer) error {
	r, err := ulikunitz.NewReader(bufio.NewReaderSize(in, bufSize))
	if err != nil {
		return fmt.Errorf("initializing xz reader: %w", err)
	}
	if _, err := io.Copy(out, &ctxReader{ctx: ctx, r: r}); err != nil {
		return fmt.Errorf("decompressing xz stream: %w", err)
	}
	return nil
}

// ctxReader makes the otherwise-uninterruptible decode cancellable: each Read
// returns the context error once ctx is cancelled, which unwinds io.Copy. The
// decode is single-threaded and can run for tens of seconds on a large image,
// so this is what lets a service shutdown propagate instead of blocking on it.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// DecompressReader decompresses the xz stream from in to dst through a
// sparseWriter. It decodes into a temporary file in dst's directory and
// renames it into place, so an interrupted decode never leaves a partial dst
// that downstream code would mistake for a complete image.
func DecompressReader(ctx context.Context, in io.Reader, dst string) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	w := &sparseWriter{f: tmp}
	if err = Decompress(ctx, in, w); err != nil {
		return err
	}
	if err = w.finish(); err != nil {
		return err
	}
	// os.CreateTemp creates the file 0o600. Match Lima's 0o644 for decompressed
	// images so the result stays readable beyond its owner.
	if err = tmp.Chmod(0o644); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}

// sparseBlockSize matches the default 4 KiB allocation block of APFS, ext4,
// XFS, and btrfs, the smallest hole any of them can make.
const sparseBlockSize = 4 << 10

var zeroBlock [sparseBlockSize]byte

// sparseWriter writes to f, skipping each block of zeros and punching a hole
// over each whole one where punchHole can. An xz stream records no holes, so
// writing every byte would allocate a disk image's free space along with its
// data.
type sparseWriter struct {
	f       *os.File
	off     int64 // offset in f of the next byte to write
	dataEnd int64 // offset in f just past the last data written
}

func (w *sparseWriter) Write(p []byte) (int, error) {
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
func (w *sparseWriter) writeData(data []byte, off int64) error {
	if _, err := w.f.WriteAt(data, off); err != nil {
		return err
	}
	w.punchSkipped(off)
	w.dataEnd = off + int64(len(data))
	return nil
}

// punchSkipped punches a hole over the whole blocks of zeros between the
// previous data and end.
func (w *sparseWriter) punchSkipped(end int64) {
	holeStart := (w.dataEnd + sparseBlockSize - 1) / sparseBlockSize * sparseBlockSize
	holeEnd := end / sparseBlockSize * sparseBlockSize
	if holeEnd > holeStart {
		punchHole(w.f, holeStart, holeEnd-holeStart)
	}
}

// runEnd returns the index in p where the run starting at i ends. The run
// holds blocks of zeros if zero is true, and blocks of data otherwise. Blocks
// end at multiples of sparseBlockSize in f, so a block of zeros split across
// two Write calls is still skipped and punched.
func (w *sparseWriter) runEnd(p []byte, i int, zero bool) int {
	for i < len(p) {
		end := min(len(p), i+int(sparseBlockSize-(w.off+int64(i))%sparseBlockSize))
		if bytes.Equal(p[i:end], zeroBlock[:end-i]) != zero {
			break
		}
		i = end
	}
	return i
}

// finish extends f over the zeros skipped at the end of the stream, if any,
// and punches a hole over their whole blocks, because APFS allocates a short
// extension.
func (w *sparseWriter) finish() error {
	if err := w.f.Truncate(w.off); err != nil {
		return err
	}
	w.punchSkipped(w.off)
	return nil
}
