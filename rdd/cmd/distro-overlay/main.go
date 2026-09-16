// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Command distro-overlay merges Rancher Desktop assets into a pristine openSUSE
// distro image or tarball, taking destination paths and ownership from a manifest.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/overlay"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/sparse"
)

func main() {
	manifest := flag.String("manifest", "", "overlay manifest (YAML)")
	source := flag.String("source", "", "directory holding file sources (default: manifest directory)")
	format := flag.String("format", "auto", "distro format: auto, raw, or tar")
	output := flag.String("output", "", "output path (default: overwrite input)")
	mtimeArg := flag.String("mtime", "", "modification time for every entry, as Unix epoch seconds or RFC3339 (default: now)")
	flag.Parse()

	if *manifest == "" || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: distro-overlay --manifest M [--source D] [--format auto|raw|tar] [--output O] [--mtime T] <distro>")
		os.Exit(2)
	}
	mtime, err := parseMtime(*mtimeArg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "distro-overlay:", err)
		os.Exit(2)
	}
	if err := run(*manifest, *source, *format, *output, flag.Arg(0), mtime); err != nil {
		fmt.Fprintln(os.Stderr, "distro-overlay:", err)
		os.Exit(1)
	}
}

// parseMtime reads the --mtime flag as Unix epoch seconds or an RFC3339
// timestamp, defaulting to the current time when the flag is empty. A build
// passes a deterministic value, such as a git commit time, for a reproducible
// result.
func parseMtime(arg string) (time.Time, error) {
	if arg == "" {
		return time.Now(), nil
	}
	if secs, err := strconv.ParseInt(arg, 10, 64); err == nil {
		return time.Unix(secs, 0).UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, arg); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid --mtime %q: want Unix epoch seconds or RFC3339", arg)
}

func run(manifestPath, sourceDir, format, output, distro string, mtime time.Time) error {
	m, err := overlay.LoadManifest(manifestPath)
	if err != nil {
		return err
	}
	if sourceDir == "" {
		sourceDir = filepath.Dir(manifestPath)
	}
	if format == "auto" {
		if format, err = detectFormat(distro); err != nil {
			return err
		}
	}
	switch format {
	case "raw":
		return applyRawFile(distro, output, m, sourceDir, mtime)
	case "tar":
		return applyTarFile(distro, output, m, sourceDir, mtime)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

// applyRawFile overlays the image, or a copy of it at output. It writes through,
// so an output that is a symlink stays one, and a run that fails leaves the image
// it wrote to be discarded: go-diskfs reveals an extent tree only once it has
// written one.
func applyRawFile(input, output string, m *overlay.Manifest, sourceDir string, mtime time.Time) error {
	target := input
	if output != "" {
		if err := refuseAliasedOutput(input, output); err != nil {
			return err
		}
		// Open both files before emptyOutput below takes charge, so a failure to
		// open either one leaves an existing output unchanged.
		in, err := os.Open(input)
		if err != nil {
			return err
		}
		out, err := os.Create(output)
		if err != nil {
			_ = in.Close()
			return err
		}
		err = copyTo(in, out)
		_ = in.Close()
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return emptyOutput(output, err)
		}
		target = output
	}
	d, err := overlay.OpenImage(target)
	if err == nil {
		err = overlay.Apply(d, m, sourceDir, mtime)
	}
	if err != nil && output != "" {
		return emptyOutput(output, err)
	}
	return err
}

// emptyOutput empties a half-written output and returns the failure that led
// there. The file itself stays, so a symlink or an inode the caller named
// survives, and nothing downstream can mistake the leftover for a distro.
func emptyOutput(output string, cause error) error {
	_ = os.Truncate(output, 0)
	return cause
}

// refuseAliasedOutput rejects an output naming the distro, which creating it
// would empty before the overlay reads it. Omitting --output is how to overlay
// the distro itself.
func refuseAliasedOutput(input, output string) error {
	inInfo, err := os.Stat(input)
	if err != nil {
		return err
	}
	outInfo, err := os.Stat(output)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if os.SameFile(inInfo, outInfo) {
		return fmt.Errorf("--output %s names the distro itself; omit --output to overlay it in place", output)
	}
	return nil
}

// copyTo copies in to out, writing it sparsely where the platform can punch
// holes, so the free space a distro image reserves stays unallocated on disk.
func copyTo(in io.Reader, out *os.File) error {
	w := sparse.NewWriter(out)
	if _, err := io.Copy(w, in); err != nil {
		return err
	}
	return w.Finish()
}

// detectFormat distinguishes an OEM disk image from a tarball by signature.
func detectFormat(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if sig := make([]byte, 8); readAt(f, sig, 512) && string(sig) == "EFI PART" {
		return "raw", nil // GPT header at LBA 1
	}
	if magic := make([]byte, 5); readAt(f, magic, 257) && string(magic) == "ustar" {
		return "tar", nil // POSIX tar magic
	}
	return "", fmt.Errorf("cannot detect distro format of %s; pass --format", p)
}

func readAt(f *os.File, b []byte, off int64) bool {
	_, err := f.ReadAt(b, off)
	return err == nil
}

// applyTarFile overlays a tarball, writing through to the output. Rewriting the
// distro itself, which it reads at the same time, goes to a new file beside it
// that takes its place once every entry is written.
func applyTarFile(input, output string, m *overlay.Manifest, sourceDir string, mtime time.Time) error {
	in, err := os.Open(input)
	if err != nil {
		return err
	}
	defer in.Close()

	if output != "" {
		if err := refuseAliasedOutput(input, output); err != nil {
			return err
		}
		out, err := os.Create(output)
		if err != nil {
			return err
		}
		// Empty a failed output rather than removing it: a tar writer closed
		// mid-overlay still ends the archive, and the result reads as a distro
		// with files missing. Removing it would unlink a symlink the caller
		// named and strand the file behind it.
		if err := overlay.ApplyTar(in, out, m, sourceDir, mtime); err != nil {
			_ = out.Truncate(0)
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return emptyOutput(output, err)
		}
		return nil
	}

	// Rewriting the distro reads and writes it at once, so the result goes to a
	// new file that takes its place. Follow a symlink first, so the link keeps
	// naming the distro afterwards, as it does on the raw path.
	distro, err := filepath.EvalSymlinks(input)
	if err != nil {
		return err
	}
	info, err := os.Stat(distro)
	if err != nil {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(distro), filepath.Base(distro)+".*")
	if err != nil {
		return err
	}
	tmp := out.Name()
	if err := overlay.ApplyTar(in, out, m, sourceDir, mtime); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = in.Close()
	// The replacement carries none of the distro's own metadata. Restore the
	// permissions, which decide who can read the distro next; its owner and
	// times belong to whoever built it, and this process cannot set them back.
	if err := os.Chmod(tmp, info.Mode().Perm()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, distro); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
