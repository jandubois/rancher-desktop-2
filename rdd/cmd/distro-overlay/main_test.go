// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"gotest.tools/v3/assert"
)

func TestParseMtime(t *testing.T) {
	want := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

	t.Run("epoch seconds", func(t *testing.T) {
		got, err := parseMtime("1577934245")
		assert.NilError(t, err)
		assert.Equal(t, got.Unix(), want.Unix())
	})

	t.Run("RFC3339", func(t *testing.T) {
		got, err := parseMtime("2020-01-02T03:04:05Z")
		assert.NilError(t, err)
		assert.Equal(t, got.Unix(), want.Unix())
	})

	t.Run("invalid", func(t *testing.T) {
		_, err := parseMtime("last tuesday")
		assert.ErrorContains(t, err, "want Unix epoch seconds or RFC3339")
	})
}

// TestCheckTarget checks that the tool takes one destination flag and refuses
// both or neither.
func TestCheckTarget(t *testing.T) {
	assert.NilError(t, checkTarget("overlaid.raw", false))
	assert.NilError(t, checkTarget("", true))
	assert.ErrorContains(t, checkTarget("overlaid.raw", true), "not both")
	assert.ErrorContains(t, checkTarget("", false), "pass --output")
}

// TestRunRawImage drives the raw path end to end, through the GPT scan and the
// format detection no other test reaches. An --output leaves the distro alone,
// and the tool writes through to whatever that output names.
func TestRunRawImage(t *testing.T) {
	pristine, source := fragmentedImage(t)
	orig, err := os.ReadFile(pristine)
	assert.NilError(t, err)

	for name, tc := range map[string]struct{ source, output, want string }{
		"in place":            {"small", "", ""},
		"to --output":         {"small", "overlaid.raw", ""},
		"through a symlink":   {"small", "link.raw", ""},
		"a file needing more": {"big", "overlaid.raw", "four extents an inode holds"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			img := filepath.Join(dir, "distro.raw")
			assert.NilError(t, os.WriteFile(img, orig, 0o644))
			output := ""
			if tc.output != "" {
				output = filepath.Join(dir, tc.output)
			}
			if tc.output == "link.raw" {
				assert.NilError(t, os.Symlink(filepath.Join(dir, "real.raw"), output))
			}

			err := run(manifestFor(t, dir, tc.source), source, "auto", output, "", img, testTime, "")
			after, readErr := os.ReadFile(img)
			assert.NilError(t, readErr)
			if tc.want != "" {
				assert.ErrorContains(t, err, tc.want)
			} else {
				assert.NilError(t, err)
			}
			if output == "" {
				assert.Assert(t, !bytes.Equal(after, orig), "the overlay did not reach the distro")
				return
			}
			assert.Assert(t, bytes.Equal(after, orig), "--output changed the distro")
			if tc.want != "" {
				info, err := os.Stat(output)
				assert.NilError(t, err)
				assert.Equal(t, info.Size(), int64(0), "the failed run left an image behind")
			}
			if tc.output == "link.raw" {
				info, err := os.Lstat(output)
				assert.NilError(t, err)
				assert.Assert(t, info.Mode()&os.ModeSymlink != 0, "the output symlink was replaced")
			}
		})
	}
}

// TestRunRefusesKernelParamsItCannotApply checks that a tarball and an image
// with no boot menu each report the flag they cannot apply, rather than
// ignoring it. The image case is also what proves the flag reaches the image.
func TestRunRefusesKernelParamsItCannotApply(t *testing.T) {
	t.Run("tarball", func(t *testing.T) {
		dir := t.TempDir()
		tarball := filepath.Join(dir, "distro.tar")
		writeTar(t, tarball, "etc/os-release", "NAME")
		source := filepath.Join(dir, "source")
		assert.NilError(t, os.MkdirAll(source, 0o755))
		assert.NilError(t, os.WriteFile(filepath.Join(source, "small"), []byte("S"), 0o644))

		err := run(manifestFor(t, dir, "small"), source, "auto", filepath.Join(dir, "out.tar"), "", tarball, testTime, "quiet")
		assert.ErrorContains(t, err, "needs a raw image")
	})

	t.Run("image with no boot menu", func(t *testing.T) {
		pristine, source := fragmentedImage(t)
		dir := t.TempDir()
		img := filepath.Join(dir, "distro.raw")
		orig, err := os.ReadFile(pristine)
		assert.NilError(t, err)
		assert.NilError(t, os.WriteFile(img, orig, 0o644))

		err = run(manifestFor(t, dir, "small"), source, "auto", "", "", img, testTime, "quiet")
		assert.ErrorContains(t, err, "/boot/grub2/grub.cfg")
	})
}

// TestRunRefusesOutputNamingTheDistro checks both formats refuse an --output
// that is the distro, which creating it would empty before it is read. The raw
// path is where that bites: its copy step would truncate the image first.
func TestRunRefusesOutputNamingTheDistro(t *testing.T) {
	t.Run("raw", func(t *testing.T) {
		// Built inside the subtest: fragmentedImage skips without e2fsprogs, and
		// the tarball case below needs none.
		image, imageSource := fragmentedImage(t)
		pristine, err := os.ReadFile(image)
		assert.NilError(t, err)

		dir := t.TempDir()
		distro := filepath.Join(dir, "distro.raw")
		assert.NilError(t, os.WriteFile(distro, pristine, 0o644))

		assert.ErrorContains(t, run(manifestFor(t, dir, "small"), imageSource, "auto", distro, "", distro, testTime, ""),
			"names the distro itself")

		after, err := os.ReadFile(distro)
		assert.NilError(t, err)
		assert.Assert(t, bytes.Equal(after, pristine), "the refused run changed the distro")
	})

	t.Run("tar", func(t *testing.T) {
		dir := t.TempDir()
		distro := filepath.Join(dir, "distro.tar")
		writeTar(t, distro, "etc/os-release", "NAME")
		source := filepath.Join(dir, "source")
		assert.NilError(t, os.MkdirAll(source, 0o755))
		assert.NilError(t, os.WriteFile(filepath.Join(source, "small"), []byte("S"), 0o644))

		err := run(manifestFor(t, dir, "small"), source, "auto", distro, "", distro, testTime, "")
		assert.ErrorContains(t, err, "names the distro itself")
		assert.Assert(t, tarNames(t, distro)["etc/os-release"], "the refused run emptied the distro")
	})
}

// TestRunTarKeepsSymlinksItIsGiven checks that a symlinked distro and a
// symlinked output both survive, so the links keep naming the files they did.
func TestRunTarKeepsSymlinksItIsGiven(t *testing.T) {
	for name, tc := range map[string]struct{ throughLink, refused bool }{
		"in place through a link":        {throughLink: true},
		"in place through a link, fails": {throughLink: true, refused: true},
		"output through a link":          {},
		"output through a link, fails":   {refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			distro := filepath.Join(dir, "distro.tar")
			writeTar(t, distro, "etc/os-release", "NAME")
			// A mode os.CreateTemp never produces on Unix, so the in-place
			// replacement has to copy the distro's own permissions.
			// Windows stats both as 0666, where the check below always passes.
			assert.NilError(t, os.Chmod(distro, 0o640))
			before, err := os.Stat(distro)
			assert.NilError(t, err)
			source := filepath.Join(dir, "source")
			assert.NilError(t, os.MkdirAll(source, 0o755))
			assert.NilError(t, os.WriteFile(filepath.Join(source, "small"), []byte("S"), 0o644))
			manifest := manifestFor(t, dir, "small")
			if tc.refused {
				// A manifest naming a source that is not there fails mid-run.
				manifest = manifestFor(t, dir, "absent")
			}

			link := filepath.Join(dir, "link.tar")
			input, output := distro, ""
			if tc.throughLink {
				assert.NilError(t, os.Symlink(distro, link))
				input = link
			} else {
				assert.NilError(t, os.Symlink(filepath.Join(dir, "real.tar"), link))
				output = link
			}

			err = run(manifest, source, "auto", output, "", input, testTime, "")
			switch {
			case tc.refused && tc.throughLink:
				assert.Assert(t, err != nil, "the run was expected to fail")
				// The distro is unchanged, and the half-built replacement is gone.
				assert.Assert(t, tarNames(t, distro)["etc/os-release"], "the failed run damaged the distro")
				assert.Assert(t, !tarNames(t, distro)["overlaid"], "the failed run overlaid the distro")
				leftovers, globErr := filepath.Glob(distro + ".*")
				assert.NilError(t, globErr)
				assert.Equal(t, len(leftovers), 0, "the failed run left %v behind", leftovers)
			case tc.refused:
				assert.Assert(t, err != nil, "the run was expected to fail")
				// The link survives and names an empty file, not a tarball
				// that reads as a distro with entries missing.
				through, err := os.Stat(link)
				assert.NilError(t, err)
				assert.Equal(t, through.Size(), int64(0), "the failed run left a tarball behind")
			case tc.throughLink:
				assert.NilError(t, err)
				// Without --output the overlay replaces the distro itself, and
				// the replacement keeps the permissions the distro had.
				assert.Assert(t, tarNames(t, distro)["overlaid"], "the overlay never reached the distro")
				after, err := os.Stat(distro)
				assert.NilError(t, err)
				assert.Equal(t, after.Mode().Perm(), before.Mode().Perm(), "the distro changed permissions")
			default:
				assert.NilError(t, err)
				assert.Assert(t, tarNames(t, link)["overlaid"], "the overlay never reached the output")
				assert.Assert(t, !tarNames(t, distro)["overlaid"], "the distro gained the overlay entry")
			}
			info, err := os.Lstat(link)
			assert.NilError(t, err)
			assert.Assert(t, info.Mode()&os.ModeSymlink != 0, "the symlink was replaced")
		})
	}
}

// TestRunTarWritesThroughToOutput checks the tarball path writes the output the
// caller named, leaving the distro as it was.
func TestRunTarWritesThroughToOutput(t *testing.T) {
	dir := t.TempDir()
	tarball := filepath.Join(dir, "distro.tar")
	writeTar(t, tarball, "etc/os-release", "NAME")
	source := filepath.Join(dir, "source")
	assert.NilError(t, os.MkdirAll(source, 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(source, "small"), []byte("S"), 0o644))
	output := filepath.Join(dir, "overlaid.tar")

	assert.NilError(t, run(manifestFor(t, dir, "small"), source, "auto", output, "", tarball, testTime, ""))

	assert.Assert(t, tarNames(t, output)["overlaid"], "the overlay entry is missing")
	assert.Assert(t, !tarNames(t, tarball)["overlaid"], "the distro gained the overlay entry")
}

func TestRunProfileAppliesOnlyItsEntries(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	assert.NilError(t, os.MkdirAll(source, 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(source, "l"), []byte("L"), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(source, "w"), []byte("W"), 0o644))

	manifest := filepath.Join(dir, "manifest.yaml")
	assert.NilError(t, os.WriteFile(manifest, []byte(
		"entries:\n"+
			"  - path: /lima-only\n    source: l\n    profiles: [lima]\n"+
			"  - path: /wsl-only\n    source: w\n    profiles: [wsl]\n"), 0o644))

	tarball := filepath.Join(dir, "distro.tar")
	writeTar(t, tarball, "etc/os-release", "NAME")
	output := filepath.Join(dir, "overlaid.tar")

	assert.NilError(t, run(manifest, source, "auto", output, "lima", tarball, testTime, ""))

	names := tarNames(t, output)
	assert.Assert(t, names["lima-only"], "the lima entry is missing")
	assert.Assert(t, !names["wsl-only"], "the wsl entry leaked into the lima image")
}

func TestRunRejectsAnUnknownProfile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	assert.NilError(t, os.MkdirAll(source, 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(source, "small"), []byte("S"), 0o644))
	tarball := filepath.Join(dir, "distro.tar")
	writeTar(t, tarball, "etc/os-release", "NAME")

	err := run(manifestFor(t, dir, "small"), source, "auto", filepath.Join(dir, "out.tar"), "macos", tarball, testTime, "")
	assert.ErrorContains(t, err, `unknown profile "macos"`)
}

// fragmentedImage builds a GPT disk whose 20 MiB ext4 root is nearly full, its
// free space in 1 MiB holes, and a source directory of two files.
func fragmentedImage(t *testing.T) (image, source string) {
	t.Helper()
	e2fsprogs(t, "mke2fs") // skips the test when e2fsprogs is missing
	dir := t.TempDir()
	image = filepath.Join(dir, "pristine.raw")
	d, err := diskfs.Create(image, 24<<20, diskfs.SectorSizeDefault)
	assert.NilError(t, err)
	assert.NilError(t, d.Partition(&gpt.Table{
		LogicalSectorSize:  512,
		PhysicalSectorSize: 512,
		ProtectiveMBR:      true,
		Partitions:         []*gpt.Partition{{Index: 1, Start: 2048, Size: 20 << 20, Type: gpt.LinuxFilesystem}},
	}))
	assert.NilError(t, d.Close())

	root := filepath.Join(dir, "root")
	for n := range 14 {
		p := filepath.Join(root, "fill", fmt.Sprintf("f%02d", n))
		assert.NilError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		assert.NilError(t, os.WriteFile(p, bytes.Repeat([]byte{'F'}, 1<<20), 0o644))
	}
	code, out := e2fsprogs(t, "mke2fs", "-q", "-F", "-t", "ext4", "-b", "4096", "-O", "metadata_csum",
		"-E", "offset=1048576", "-d", root, image, "5120")
	assert.Equal(t, code, 0, out)
	for n := 0; n < 14; n += 2 {
		code, out := e2fsprogs(t, "debugfs", "-w", "-R", fmt.Sprintf("rm /fill/f%02d", n), image+"?offset=1048576")
		assert.Equal(t, code, 0, out)
	}

	source = filepath.Join(dir, "source")
	assert.NilError(t, os.MkdirAll(source, 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(source, "big"), bytes.Repeat([]byte{'B'}, 6<<20), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(source, "small"), []byte("S"), 0o644))
	return image, source
}

// manifestFor writes a one-entry manifest naming the given source file.
func manifestFor(t *testing.T, dir, source string) string {
	t.Helper()
	p := filepath.Join(dir, "manifest.yaml")
	assert.NilError(t, os.WriteFile(p, []byte("entries:\n  - path: /overlaid\n    source: "+source+"\n"), 0o644))
	return p
}

// writeTar writes a tarball holding one file.
func writeTar(t *testing.T, path, name, body string) {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	assert.NilError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}))
	_, err := tw.Write([]byte(body))
	assert.NilError(t, err)
	assert.NilError(t, tw.Close())
	assert.NilError(t, os.WriteFile(path, buf.Bytes(), 0o644))
}

// tarNames is the set of entry names in a tarball.
func tarNames(t *testing.T, path string) map[string]bool {
	t.Helper()
	f, err := os.Open(path)
	assert.NilError(t, err)
	defer f.Close()
	names := map[string]bool{}
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err != nil {
			return names
		}
		names[hdr.Name] = true
	}
}

// testTime is the timestamp the CLI tests stamp every entry with.
var testTime = time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

// e2fsprogs runs an e2fsprogs command and returns its exit code and output. It
// skips the test where e2fsprogs is missing, unless RDD_REQUIRE_E2FSPROGS is
// true, which CI sets where it provides e2fsprogs.
//
// pkg/overlay keeps its own copy. Two of them beat a test-helper package that
// nothing else would use, so long as the RDD_REQUIRE_E2FSPROGS contract stays
// the same in both; a third caller is when to extract it.
func e2fsprogs(t *testing.T, name string, args ...string) (code int, output string) {
	t.Helper()
	cmd, err := exec.LookPath(name)
	if err != nil {
		assert.Assert(t, os.Getenv("RDD_REQUIRE_E2FSPROGS") != "true", "%s not found: %v", name, err)
		t.Skipf("%s not found: %v", name, err)
	}
	if len(args) == 0 {
		return 0, "" // the caller only wanted the skip
	}
	out, err := exec.CommandContext(t.Context(), cmd, args...).CombinedOutput()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), string(out)
	}
	assert.NilError(t, err)
	return 0, string(out)
}
