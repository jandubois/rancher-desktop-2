// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package overlay

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/ext4"
	"gotest.tools/v3/assert"
)

func TestLoadManifestParsesOctalAndDefaults(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "overlay.yaml")
	assert.NilError(t, os.WriteFile(manifest, []byte(`
entries:
  - path: /usr/local/bin/tool
    source: tool
    mode: "0755"
  - path: /etc/keep
    type: dir
`), 0o644))

	m, err := LoadManifest(manifest)
	assert.NilError(t, err)
	assert.Equal(t, len(m.Entries), 2)

	mode, err := m.Entries[0].mode(0o644)
	assert.NilError(t, err)
	assert.Equal(t, mode, os.FileMode(0o755))
	assert.Equal(t, m.Entries[1].kind(), TypeDir)
}

func TestValidateRejectsBadEntries(t *testing.T) {
	cases := map[string]struct {
		entry Entry
		want  string
	}{
		"relative path":   {Entry{Path: "usr/local/bin/x", Source: "x"}, "absolute and clean"},
		"file no source":  {Entry{Path: "/x"}, "needs a source"},
		"symlink target":  {Entry{Path: "/x", Type: TypeSymlink}, "needs a target"},
		"dir with target": {Entry{Path: "/x", Type: TypeDir, Target: "/y"}, "cannot have a target"},
		"escaping source": {Entry{Path: "/x", Source: "../x"}, "within the source directory"},
		"bad mode":        {Entry{Path: "/x", Source: "x", Mode: "9999"}, "invalid mode"},
		"setuid mode":     {Entry{Path: "/x", Source: "x", Mode: "4755"}, "permission bits"},
		"unknown type":    {Entry{Path: "/x", Type: "socket"}, "unknown type"},
		"symlink owner":   {Entry{Path: "/x", Type: TypeSymlink, Target: "/y", UID: 472}, "cannot set uid or gid"},
		"negative uid":    {Entry{Path: "/x", Source: "x", UID: -1}, "between 0 and"},
		"unknown profile": {Entry{Path: "/x", Source: "x", Profiles: []string{"macos"}}, "unknown profile"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.ErrorContains(t, tc.entry.validate(), tc.want)
		})
	}
}

// TestValidateRejectsSourceEscapingOnWindows checks the source check that only
// a Windows build host needs, where filepath.Join reads a backslash in the
// source as a separator and resolves it outside the source directory.
func TestValidateRejectsSourceEscapingOnWindows(t *testing.T) {
	e := Entry{Path: "/x", Source: `..\..\x`}
	err := e.validate()
	if runtime.GOOS != "windows" {
		// Elsewhere the backslashes are an ordinary filename.
		assert.NilError(t, err)
		return
	}
	assert.ErrorContains(t, err, "within the source directory")
}

// TestWriteFileRejectsSymlinkSource checks a file entry whose source is a
// symlink, which would otherwise be followed out of the source directory.
func TestWriteFileRejectsSymlinkSource(t *testing.T) {
	source := t.TempDir()
	writeSource(t, source, "real", "REAL")
	assert.NilError(t, os.Symlink("real", filepath.Join(source, "link")))

	m := &Manifest{Entries: []Entry{{Path: "/etc/app.conf", Source: "link"}}}
	err := ApplyTar(bytes.NewReader(buildTar(t, nil)), io.Discard, m, source, testMtime)
	assert.ErrorContains(t, err, "may not be a symlink")
}

// TestValidateRejectsIDPastUint32 checks the upper bound on ownership, which
// only a 64-bit int can express.
func TestValidateRejectsIDPastUint32(t *testing.T) {
	tooBig := maxID + 1
	if int64(int(tooBig)) != tooBig {
		t.Skip("an int this wide cannot hold a uid past uint32")
	}
	e := Entry{Path: "/x", Source: "x", GID: int(tooBig)}
	assert.ErrorContains(t, e.validate(), "between 0 and")
}

// TestValidateRejectsDuplicatePaths checks the manifest refuses one path twice,
// which the tarball would carry as two entries and the image as one file.
func TestValidateRejectsDuplicatePaths(t *testing.T) {
	m := &Manifest{Entries: []Entry{
		{Path: "/etc/app.conf", Source: "one"},
		{Path: "/etc/other.conf", Source: "other"},
		{Path: "/etc/app.conf", Source: "two"},
	}}
	assert.ErrorContains(t, m.validate(), "already used by entry 0")
}

func TestForProfileSelectsMatchingEntries(t *testing.T) {
	m := &Manifest{Entries: []Entry{
		{Path: "/both", Source: "both"},
		{Path: "/lima-only", Source: "lima", Profiles: []string{ProfileLima}},
		{Path: "/wsl-only", Source: "wsl", Profiles: []string{ProfileWSL}},
		{Path: "/either", Source: "either", Profiles: []string{ProfileLima, ProfileWSL}},
	}}

	paths := func(m *Manifest) []string {
		var got []string
		for i := range m.Entries {
			got = append(got, m.Entries[i].Path)
		}
		return got
	}

	assert.DeepEqual(t, paths(m.ForProfile(ProfileLima)), []string{"/both", "/lima-only", "/either"})
	assert.DeepEqual(t, paths(m.ForProfile(ProfileWSL)), []string{"/both", "/wsl-only", "/either"})
	assert.DeepEqual(t, paths(m.ForProfile("")), []string{"/both", "/lima-only", "/wsl-only", "/either"})
}

func TestApplyTarOverridesNewFilesDirsAndLinks(t *testing.T) {
	base := buildTar(t, []tarItem{
		{name: "usr/local/bin/", dir: true},
		{name: "usr/local/bin/old", body: "OLD"},
		{name: "etc/keep.conf", body: "KEEP"},
	})

	source := t.TempDir()
	writeSource(t, source, "bin/old", "NEWOLD")
	writeSource(t, source, "bin/new", "NEW")

	m := &Manifest{Entries: []Entry{
		{Path: "/usr/local/bin/old", Source: "bin/old", Mode: "0755", UID: 1, GID: 2},
		{Path: "/usr/local/bin/new", Source: "bin/new", Mode: "0700"},
		{Path: "/etc/foo.d", Type: TypeDir, Mode: "0750", UID: 3, GID: 4},
		{Path: "/etc/link", Type: TypeSymlink, Target: "/usr/local/bin/new"},
	}}

	var out bytes.Buffer
	assert.NilError(t, ApplyTar(bytes.NewReader(base), &out, m, source, testMtime))
	got := readTar(t, out.Bytes())

	override := got["usr/local/bin/old"]
	assert.Equal(t, override.body, "NEWOLD")
	assert.Equal(t, override.hdr.Mode, int64(0o755))
	assert.Equal(t, override.hdr.Uid, 1)
	assert.Equal(t, override.hdr.Gid, 2)

	added := got["usr/local/bin/new"]
	assert.Equal(t, added.body, "NEW")
	assert.Equal(t, added.hdr.Mode, int64(0o700))

	assert.Equal(t, got["etc/keep.conf"].body, "KEEP")

	dir := got["etc/foo.d"]
	assert.Equal(t, dir.hdr.Typeflag, byte(tar.TypeDir))
	assert.Equal(t, dir.hdr.Mode, int64(0o750))
	assert.Equal(t, dir.hdr.Uid, 3)

	link := got["etc/link"]
	assert.Equal(t, link.hdr.Typeflag, byte(tar.TypeSymlink))
	assert.Equal(t, link.hdr.Linkname, "/usr/local/bin/new")

	assert.Equal(t, count(out.Bytes(), "usr/local/bin/old"), 1)
}

// testMtime is the timestamp every overlay entry is stamped with in the tests.
var testMtime = time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

func TestApplyTarStampsParameterMtime(t *testing.T) {
	source := t.TempDir()
	writeSource(t, source, "file", "F")
	// A source file's own mtime is ignored; every entry gets the parameter.
	old := time.Date(2009, 8, 7, 6, 5, 4, 0, time.UTC)
	assert.NilError(t, os.Chtimes(filepath.Join(source, "file"), old, old))

	m := &Manifest{Entries: []Entry{
		{Path: "/file", Source: "file"},
		{Path: "/dir", Type: TypeDir},
		{Path: "/link", Type: TypeSymlink, Target: "/file"},
	}}

	var out bytes.Buffer
	assert.NilError(t, ApplyTar(bytes.NewReader(buildTar(t, nil)), &out, m, source, testMtime))
	got := readTar(t, out.Bytes())

	for _, name := range []string{"file", "dir", "link"} {
		assert.Equal(t, got[name].hdr.ModTime.Unix(), testMtime.Unix(), "entry %s", name)
	}
}

func TestApplyTarCopiesTree(t *testing.T) {
	base := buildTar(t, []tarItem{
		{name: "lib/systemd/", dir: true},
		{name: "lib/systemd/stale.service", body: "STALE"}, // replaced by the tree
	})

	source := t.TempDir()
	writeSource(t, source, "units/fresh.service", "FRESH")
	writeSource(t, source, "units/stale.service", "NEW")
	writeSource(t, source, "units/multi-user.target.wants/run.sh", "#!/bin/sh\n")
	// Tree members get fixed modes, whatever the source has.
	assert.NilError(t, os.Chmod(filepath.Join(source, "units/multi-user.target.wants/run.sh"), 0o755))
	assert.NilError(t, os.Chmod(filepath.Join(source, "units/multi-user.target.wants"), 0o700))

	m := &Manifest{Entries: []Entry{
		{Path: "/lib/systemd", Type: TypeDir, Source: "units", UID: 0, GID: 0},
	}}

	var out bytes.Buffer
	assert.NilError(t, ApplyTar(bytes.NewReader(base), &out, m, source, testMtime))
	got := readTar(t, out.Bytes())

	assert.Equal(t, got["lib/systemd/fresh.service"].body, "FRESH")
	assert.Equal(t, got["lib/systemd/stale.service"].body, "NEW")       // overridden, not duplicated
	assert.Equal(t, count(out.Bytes(), "lib/systemd/stale.service"), 1) // base copy dropped

	run := got["lib/systemd/multi-user.target.wants/run.sh"]
	assert.Equal(t, run.hdr.Mode, int64(0o644))
	assert.Equal(t, run.hdr.ModTime.Unix(), testMtime.Unix())

	dir := got["lib/systemd/multi-user.target.wants"]
	assert.Equal(t, dir.hdr.Typeflag, byte(tar.TypeDir))
	assert.Equal(t, dir.hdr.Mode, int64(0o755))
}

// TestTreeRejectsSourcesItCannotCopy checks that a tree copy fails, instead of
// copying nothing, when its source is not a directory, and fails on a symlink in
// the tree.
func TestTreeRejectsSourcesItCannotCopy(t *testing.T) {
	source := t.TempDir()
	writeSource(t, source, "plain", "P")
	writeSource(t, source, "tree/a.conf", "A")
	assert.NilError(t, os.Symlink("tree", filepath.Join(source, "tree-link")))
	writeSource(t, source, "linked/a.conf", "A")
	assert.NilError(t, os.Symlink("a.conf", filepath.Join(source, "linked/b.conf")))

	for name, tc := range map[string]struct{ source, want string }{
		"source is a file":     {"plain", "must be a directory"},
		"source is a symlink":  {"tree-link", "must be a directory"},
		"tree holds a symlink": {"linked", "only files and directories"},
	} {
		t.Run(name, func(t *testing.T) {
			m := &Manifest{Entries: []Entry{{Path: "/etc/app", Type: TypeDir, Source: tc.source}}}
			err := ApplyTar(bytes.NewReader(buildTar(t, nil)), io.Discard, m, source, testMtime)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// TestApplyTarRefusesTypeConflicts checks that the tarball backend, like the
// image backend, refuses a path under a base symlink and a file over a base
// directory.
func TestApplyTarRefusesTypeConflicts(t *testing.T) {
	base := buildTar(t, []tarItem{
		{name: "lib", link: "usr/lib"},
		{name: "etc/app/", dir: true},
	})
	source := t.TempDir()
	writeSource(t, source, "x.conf", "X")

	for name, tc := range map[string]struct{ path, want string }{
		"path under a base symlink":  {"/lib/app/x.conf", "not a directory"},
		"file over a base directory": {"/etc/app", "is a directory"},
	} {
		t.Run(name, func(t *testing.T) {
			m := &Manifest{Entries: []Entry{{Path: tc.path, Source: "x.conf"}}}
			err := ApplyTar(bytes.NewReader(base), io.Discard, m, source, testMtime)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// TestBackendsAgreeOnOverlayTypeConflicts checks that both backends refuse a
// path the overlay writes as two different types, in either order. Extracting
// such a tarball either fails or silently drops one of the two entries.
func TestBackendsAgreeOnOverlayTypeConflicts(t *testing.T) {
	source := t.TempDir()
	writeSource(t, source, "tree/sub/x.conf", "X")
	writeSource(t, source, "afile", "FILE")
	writeSource(t, source, "child", "CHILD")

	for name, tc := range map[string]struct {
		entries    []Entry
		image, tar string
	}{
		"file over a directory the overlay creates": {
			entries: []Entry{
				{Path: "/etc/app", Type: TypeDir, Source: "tree"},
				{Path: "/etc/app/sub", Source: "afile"},
			},
			image: "the image has a directory there",
			tar:   "is a directory the overlay writes",
		},
		"directory over a file the overlay writes": {
			entries: []Entry{
				{Path: "/etc/app/sub", Source: "afile"},
				{Path: "/etc/app/sub/child", Source: "child"},
			},
			image: "not a directory",
			tar:   "is a file the overlay writes",
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := &Manifest{Entries: tc.entries}
			image := newImage(t, filepath.Join(t.TempDir(), "distro.raw"))
			assert.ErrorContains(t, Apply(image, m, source, testMtime), tc.image)

			var out bytes.Buffer
			err := ApplyTar(bytes.NewReader(buildTar(t, nil)), &out, m, source, testMtime)
			assert.ErrorContains(t, err, tc.tar)
		})
	}
}

// TestTreeSourceMayNameTheSourceDirectory checks a dir entry copying the whole
// source directory, whose own parent is outside it.
func TestTreeSourceMayNameTheSourceDirectory(t *testing.T) {
	source := t.TempDir()
	writeSource(t, source, "a.conf", "A")
	writeSource(t, source, "sub/b.conf", "B")

	var out bytes.Buffer
	m := &Manifest{Entries: []Entry{{Path: "/etc/app", Type: TypeDir, Source: "."}}}
	assert.NilError(t, ApplyTar(bytes.NewReader(buildTar(t, nil)), &out, m, source, testMtime))

	got := readTar(t, out.Bytes())
	assert.Equal(t, got["etc/app/a.conf"].body, "A")
	assert.Equal(t, got["etc/app/sub/b.conf"].body, "B")
}

// TestTarRefusesFileAndSymlinkAtOnePath drives the backend directly, because a
// manifest naming one path twice never gets past validation. The image backend
// refuses both orders, so the tarball backend has to as well.
func TestTarRefusesFileAndSymlinkAtOnePath(t *testing.T) {
	newTar := func() *tarDistro {
		return &tarDistro{tw: tar.NewWriter(io.Discard), base: map[string]byte{}, written: map[string]byte{}}
	}

	t.Run("symlink over a file", func(t *testing.T) {
		d := newTar()
		assert.NilError(t, d.WriteFile("/etc/app/x", strings.NewReader("X"), 0, 0, 0o644, testMtime))
		assert.ErrorContains(t, d.Symlink("/etc/app/x", "/elsewhere", testMtime),
			"is a file the overlay writes")
	})

	t.Run("file over a symlink", func(t *testing.T) {
		d := newTar()
		assert.NilError(t, d.Symlink("/etc/app/x", "/elsewhere", testMtime))
		assert.ErrorContains(t, d.WriteFile("/etc/app/x", strings.NewReader("X"), 0, 0, 0o644, testMtime),
			"is a symlink the overlay writes")
	})
}

// TestApplyTarExplicitOverridesTree checks an explicit file or dir entry overrides
// a colliding tree member regardless of manifest order, with no duplicate entry.
func TestApplyTarExplicitOverridesTree(t *testing.T) {
	source := t.TempDir()
	writeSource(t, source, "tree/a.conf", "TREE-A")
	writeSource(t, source, "tree/sub/x.conf", "X")
	writeSource(t, source, "explicit-a.conf", "EXPLICIT-A")

	tree := Entry{Path: "/etc/app", Type: TypeDir, Source: "tree"}
	file := Entry{Path: "/etc/app/a.conf", Source: "explicit-a.conf", UID: 472, Mode: "0600"}
	dir := Entry{Path: "/etc/app/sub", Type: TypeDir, Mode: "0700", UID: 5}

	for name, entries := range map[string][]Entry{
		"explicit after tree":  {tree, file, dir},
		"explicit before tree": {file, dir, tree},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			assert.NilError(t, ApplyTar(bytes.NewReader(buildTar(t, nil)),
				&out, &Manifest{Entries: entries}, source, testMtime))
			got := readTar(t, out.Bytes())

			a := got["etc/app/a.conf"]
			assert.Equal(t, a.body, "EXPLICIT-A")
			assert.Equal(t, a.hdr.Mode, int64(0o600))
			assert.Equal(t, a.hdr.Uid, 472)
			assert.Equal(t, count(out.Bytes(), "etc/app/a.conf"), 1) // not duplicated

			sub := got["etc/app/sub"]
			assert.Equal(t, sub.hdr.Mode, int64(0o700))
			assert.Equal(t, sub.hdr.Uid, 5)
			assert.Equal(t, got["etc/app/sub/x.conf"].body, "X") // tree child still placed
		})
	}
}

// TestImageExplicitOverridesTree is the ext4 counterpart, where the two backends
// previously disagreed on which order won.
func TestImageExplicitOverridesTree(t *testing.T) {
	source := t.TempDir()
	writeSource(t, source, "tree/a.conf", "TREE-A")
	writeSource(t, source, "tree/sub/x.conf", "X")
	writeSource(t, source, "explicit-a.conf", "EXPLICIT-A")

	tree := Entry{Path: "/etc/app", Type: TypeDir, Source: "tree"}
	file := Entry{Path: "/etc/app/a.conf", Source: "explicit-a.conf", Mode: "0600"}
	dir := Entry{Path: "/etc/app/sub", Type: TypeDir, Mode: "0700"}

	for name, entries := range map[string][]Entry{
		"explicit after tree":  {tree, file, dir},
		"explicit before tree": {file, dir, tree},
	} {
		t.Run(name, func(t *testing.T) {
			img := filepath.Join(t.TempDir(), "distro.raw")
			assert.NilError(t, Apply(newImage(t, img), &Manifest{Entries: entries}, source, testMtime))
			fs := openImageFS(t, img)

			sub, err := fs.Stat("etc/app/sub")
			assert.NilError(t, err)
			assert.Equal(t, sub.Mode().Perm(), os.FileMode(0o700))

			a, err := fs.Stat("etc/app/a.conf")
			assert.NilError(t, err)
			assert.Equal(t, a.Mode().Perm(), os.FileMode(0o600))
		})
	}
}

// TestImageStampsParameterMtime checks the ext4 backend stamps the parameter
// mtime on a file, on every ancestor directory it creates, and on a symlink.
func TestImageStampsParameterMtime(t *testing.T) {
	source := t.TempDir()
	writeSource(t, source, "tool", "BIN")

	m := &Manifest{Entries: []Entry{
		{Path: "/usr/local/bin/tool", Source: "tool"}, // creates the usr/local/bin chain
		{Path: "/usr/local/bin/link", Type: TypeSymlink, Target: "tool"},
	}}

	img := filepath.Join(t.TempDir(), "distro.raw")
	assert.NilError(t, Apply(newImage(t, img), m, source, testMtime))

	fs := openImageFS(t, img)
	for _, p := range []string{"usr", "usr/local", "usr/local/bin", "usr/local/bin/tool", "usr/local/bin/link"} {
		info, err := fs.Stat(p)
		assert.NilError(t, err)
		assert.Equal(t, info.ModTime().Unix(), testMtime.Unix(), "mtime of %s", p)
	}
}

// TestImageRefusesUnsafeWrites checks that the image backend refuses the writes
// go-diskfs gets wrong.
func TestImageRefusesUnsafeWrites(t *testing.T) {
	source := t.TempDir()
	writeSource(t, source, "short", "SHORT")
	writeSource(t, source, "long", strings.Repeat("L", 64<<10))

	for name, tc := range map[string]struct {
		prepare      func(t *testing.T, fs *ext4.FileSystem)
		path, source string
		want         string
	}{
		"shorter than the image's file": {
			prepare: func(t *testing.T, fs *ext4.FileSystem) {
				writeImageFile(t, fs, "etc/app.conf", "LONGER", "CONTENT")
			},
			path: "/etc/app.conf", source: "short", want: "shrink",
		},
		"image has a symlink there": {
			prepare: func(t *testing.T, fs *ext4.FileSystem) {
				assert.NilError(t, fs.Symlink("hosts", "etc/app.conf"))
			},
			path: "/etc/app.conf", source: "short", want: "symlink",
		},
		"image's file has an extent tree": {
			// Each Write becomes its own extent, and five overflow the inode.
			prepare: func(t *testing.T, fs *ext4.FileSystem) {
				chunk := strings.Repeat("C", 4096)
				writeImageFile(t, fs, "etc/app.conf", chunk, chunk, chunk, chunk, chunk)
			},
			path: "/etc/app.conf", source: "long", want: "extent tree",
		},
		"path under an image symlink": {
			prepare: func(t *testing.T, fs *ext4.FileSystem) {
				assert.NilError(t, fs.Mkdir("usr/lib"))
				assert.NilError(t, fs.Symlink("usr/lib", "lib"))
			},
			path: "/lib/app.conf", source: "short", want: "not a directory",
		},
	} {
		t.Run(name, func(t *testing.T) {
			img := newImage(t, filepath.Join(t.TempDir(), "distro.raw"))
			assert.NilError(t, img.fs.Mkdir("etc"))
			tc.prepare(t, img.fs)
			m := &Manifest{Entries: []Entry{{Path: tc.path, Source: tc.source}}}
			assert.ErrorContains(t, Apply(img, m, source, testMtime), tc.want)
		})
	}
}

// TestImageRefusesSharedOrSparseFiles checks the image backend refuses to
// replace a file other names share, or one whose blocks fall short of its size;
// go-diskfs writes both outside the file the entry names.
func TestImageRefusesSharedOrSparseFiles(t *testing.T) {
	e2fsprogs(t, "mke2fs") // skip before os.Link, which some filesystems refuse
	root := t.TempDir()
	// mke2fs -d leaves all-zero source blocks unallocated, so this file has a hole.
	writeSource(t, root, "etc/sparse.conf", strings.Repeat("\x00", 64<<10)+"DATA")
	writeSource(t, root, "usr/bin/busybox", "BUSYBOX")
	assert.NilError(t, os.Link(filepath.Join(root, "usr/bin/busybox"), filepath.Join(root, "usr/bin/vi")))
	writeSource(t, root, "usr/bin/plain", "PLAIN")
	img := mkfsImage(t, root)

	source := t.TempDir()
	writeSource(t, source, "new", strings.Repeat("N", 64<<10+4))
	for name, tc := range map[string]struct{ path, want string }{
		"file with holes":      {"/etc/sparse.conf", "holes"},
		"file with hard links": {"/usr/bin/busybox", "hard links"},
	} {
		t.Run(name, func(t *testing.T) {
			m := &Manifest{Entries: []Entry{{Path: tc.path, Source: "new"}}}
			assert.ErrorContains(t, Apply(openImage(t, img), m, source, testMtime), tc.want)
		})
	}

	// A file that is neither still takes the override.
	m := &Manifest{Entries: []Entry{{Path: "/usr/bin/plain", Source: "new"}}}
	assert.NilError(t, Apply(openImage(t, img), m, source, testMtime))
	fsck(t, img)
}

// TestImageRefusesUninitializedGroups checks the image backend refuses an image
// holding a BLOCK_UNINIT group, where go-diskfs allocates without clearing the
// flag that tells the kernel to rebuild the bitmap.
func TestImageRefusesUninitializedGroups(t *testing.T) {
	img := filepath.Join(t.TempDir(), "fresh.ext4")
	// 768 MiB is the smallest filesystem mke2fs leaves a group uninitialized in.
	code, out := e2fsprogs(t, "mke2fs", "-q", "-F", "-t", "ext4", "-b", "4096", "-O", "metadata_csum", img, "196608")
	assert.Equal(t, code, 0, out)

	d, err := diskfs.Open(img, diskfs.WithOpenMode(diskfs.ReadWrite))
	assert.NilError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	fsi, err := d.GetFilesystem(0)
	assert.NilError(t, err)
	_, err = newImageDistro(d, fsi.(*ext4.FileSystem), 0)
	assert.ErrorContains(t, err, "BLOCK_UNINIT")
}

// TestImageRefusesNon64BitFilesystem checks the image backend refuses a
// filesystem whose 32-byte group descriptors crash go-diskfs mid-write.
func TestImageRefusesNon64BitFilesystem(t *testing.T) {
	img := filepath.Join(t.TempDir(), "small.ext4")
	code, out := e2fsprogs(t, "mke2fs", "-q", "-F", "-t", "ext4", "-b", "4096", "-O", "^64bit", img, "16384")
	assert.Equal(t, code, 0, out)

	d, err := diskfs.Open(img, diskfs.WithOpenMode(diskfs.ReadWrite))
	assert.NilError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	fsi, err := d.GetFilesystem(0)
	assert.NilError(t, err)
	_, err = newImageDistro(d, fsi.(*ext4.FileSystem), 0)
	assert.ErrorContains(t, err, "not 64bit ext4")
}

// TestImageRefusesFileNeedingAnExtentTree checks the refusal that fires only
// after go-diskfs has written the tree, on an image whose free space sits in
// 1 MiB holes.
func TestImageRefusesFileNeedingAnExtentTree(t *testing.T) {
	root := t.TempDir()
	for n := range 14 {
		writeSource(t, root, fmt.Sprintf("fill/f%02d", n), strings.Repeat("F", 1<<20))
	}
	img := filepath.Join(t.TempDir(), "frag.ext4")
	code, out := e2fsprogs(t, "mke2fs", "-q", "-F", "-t", "ext4", "-b", "4096", "-O", "metadata_csum",
		"-d", root, img, "5120")
	assert.Equal(t, code, 0, out)
	for n := 0; n < 14; n += 2 {
		code, out := e2fsprogs(t, "debugfs", "-w", "-R", fmt.Sprintf("rm /fill/f%02d", n), img)
		assert.Equal(t, code, 0, out)
	}

	source := t.TempDir()
	writeSource(t, source, "big", strings.Repeat("B", 6<<20))
	m := &Manifest{Entries: []Entry{{Path: "/big", Source: "big"}}}
	assert.ErrorContains(t, Apply(openImage(t, img), m, source, testMtime), "four extents an inode holds")
}

// TestImageRefusesSymlinkOverExistingPath checks the image backend refuses a
// symlink entry at a path the image already has, which go-diskfs cannot replace.
func TestImageRefusesSymlinkOverExistingPath(t *testing.T) {
	img := newImage(t, filepath.Join(t.TempDir(), "distro.raw"))
	assert.NilError(t, img.fs.Mkdir("etc"))
	writeImageFile(t, img.fs, "etc/os-release", "NAME=openSUSE\n")

	m := &Manifest{Entries: []Entry{{Path: "/etc/os-release", Type: TypeSymlink, Target: "/usr/lib/os-release"}}}
	assert.ErrorContains(t, Apply(img, m, t.TempDir(), testMtime), "cannot replace it with a symlink")
}

// TestApplyTarRefusesHardLinks checks the tarball backend refuses to replace
// either end of a base hard link, which would leave the other end naming an
// entry the overlay dropped.
func TestApplyTarRefusesHardLinks(t *testing.T) {
	base := buildTar(t, []tarItem{
		{name: "usr/bin/busybox", body: "BUSYBOX"},
		{name: "usr/bin/vi", link: "usr/bin/busybox", hard: true},
		{name: "usr/bin/plain", body: "PLAIN"},
	})
	source := t.TempDir()
	writeSource(t, source, "new", "NEW")

	for name, path := range map[string]string{
		"the link target": "/usr/bin/busybox",
		"the link itself": "/usr/bin/vi",
	} {
		t.Run(name, func(t *testing.T) {
			m := &Manifest{Entries: []Entry{{Path: path, Source: "new"}}}
			err := ApplyTar(bytes.NewReader(base), io.Discard, m, source, testMtime)
			assert.ErrorContains(t, err, "hard-linked in the base")
		})
	}

	// An unlinked file in the same directory still takes the override.
	m := &Manifest{Entries: []Entry{{Path: "/usr/bin/plain", Source: "new"}}}
	var out bytes.Buffer
	assert.NilError(t, ApplyTar(bytes.NewReader(base), &out, m, source, testMtime))
	assert.Equal(t, readTar(t, out.Bytes())["usr/bin/plain"].body, "NEW")
}

// TestNestedTreesKeepTheirOwnPaths checks that a tree nested inside another owns
// the paths under its root, in either manifest order, in both backends.
func TestNestedTreesKeepTheirOwnPaths(t *testing.T) {
	source := t.TempDir()
	writeSource(t, source, "app/data/a.conf", "FROM-APP") // the outer source mirrors the destination
	writeSource(t, source, "data/a.conf", "DATA")
	outer := Entry{Path: "/etc/app", Type: TypeDir, Source: "app"}
	inner := Entry{Path: "/etc/app/data", Type: TypeDir, Source: "data", Mode: "0700", UID: 472}

	for name, entries := range map[string][]Entry{
		"outer first": {outer, inner},
		"inner first": {inner, outer},
	} {
		t.Run(name, func(t *testing.T) {
			m := &Manifest{Entries: entries}
			var out bytes.Buffer
			assert.NilError(t, ApplyTar(bytes.NewReader(buildTar(t, nil)), &out, m, source, testMtime))
			got := readTar(t, out.Bytes())
			assert.Equal(t, got["etc/app/data"].hdr.Mode, int64(0o700))
			assert.Equal(t, got["etc/app/data"].hdr.Uid, 472)
			assert.Equal(t, got["etc/app/data/a.conf"].body, "DATA")
			assert.Equal(t, count(out.Bytes(), "etc/app/data/a.conf"), 1)

			img := filepath.Join(t.TempDir(), "distro.raw")
			assert.NilError(t, Apply(newImage(t, img), m, source, testMtime))
			b, err := openImageFS(t, img).ReadFile("etc/app/data/a.conf")
			assert.NilError(t, err)
			assert.Equal(t, string(b), "DATA")
		})
	}
}

// TestNestedTreeKeepsBasePathsItDoesNotWrite checks that the tarball drops only
// the base paths the overlay rewrites. The outer tree carries keep.conf, but the
// nested tree owns that subtree and supplies no such file, so the base's copy
// must survive, as it does in the image, which drops nothing.
func TestNestedTreeKeepsBasePathsItDoesNotWrite(t *testing.T) {
	base := buildTar(t, []tarItem{
		{name: "etc/app/data/", dir: true},
		{name: "etc/app/data/keep.conf", body: "BASE"},
	})
	source := t.TempDir()
	writeSource(t, source, "app/data/keep.conf", "FROM-APP") // skipped: the nested tree owns it
	writeSource(t, source, "data/a.conf", "DATA")

	m := &Manifest{Entries: []Entry{
		{Path: "/etc/app", Type: TypeDir, Source: "app"},
		{Path: "/etc/app/data", Type: TypeDir, Source: "data"},
	}}
	var out bytes.Buffer
	assert.NilError(t, ApplyTar(bytes.NewReader(base), &out, m, source, testMtime))

	got := readTar(t, out.Bytes())
	assert.Equal(t, got["etc/app/data/keep.conf"].body, "BASE")
	assert.Equal(t, got["etc/app/data/a.conf"].body, "DATA")
}

// TestApplyClosesTheDistro checks Apply closes the distro on both paths; a
// failure that leaves the image open blocks its removal on Windows.
func TestApplyClosesTheDistro(t *testing.T) {
	for name, m := range map[string]*Manifest{
		"Apply succeeds": {Entries: []Entry{{Path: "/etc/app", Type: TypeDir}}},
		"Apply fails":    {Entries: []Entry{{Path: "/etc/app", Type: TypeDir, Source: "missing"}}},
	} {
		t.Run(name, func(t *testing.T) {
			img := newImage(t, filepath.Join(t.TempDir(), "distro.raw"))
			_ = Apply(img, m, t.TempDir(), testMtime)
			assert.Assert(t, img.disk.Backend == nil, "the image is still open")
		})
	}
}

// TestImageWritesPassFsck checks the image backend's output with e2fsck, which
// verifies the metadata_csum checksums that go-diskfs's own reader skips.
func TestImageWritesPassFsck(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "usr/local/libexec/tool", "OLD")
	img := mkfsImage(t, root)

	// A file this size overflows the four extents an inode holds unless
	// go-diskfs allocates it in one piece.
	source := t.TempDir()
	writeSource(t, source, "big", strings.Repeat("B", 1<<20))

	m := &Manifest{Entries: []Entry{
		{Path: "/usr/local/bin/new", Source: "big", Mode: "0755"},
		{Path: "/usr/local/libexec/tool", Source: "big", Mode: "0755"},
		{Path: "/usr/local/bin/link", Type: TypeSymlink, Target: "new"},
	}}
	assert.NilError(t, Apply(openImage(t, img), m, source, testMtime))
	fsck(t, img)
}

// TestImageRefusesIndexedDirectory checks that the image backend refuses to add
// an entry to an htree-indexed directory, which go-diskfs would corrupt.
func TestImageRefusesIndexedDirectory(t *testing.T) {
	// Enough long names to fill several directory blocks, which e2fsck -D indexes.
	root := t.TempDir()
	for n := range 300 {
		writeSource(t, root, fmt.Sprintf("usr/bin/tool-with-a-long-name-%04d", n), "")
	}
	img := mkfsImage(t, root)
	code, out := e2fsprogs(t, "e2fsck", "-fyD", img)
	assert.Assert(t, code <= 1, out)

	source := t.TempDir()
	writeSource(t, source, "x", "X")
	m := &Manifest{Entries: []Entry{{Path: "/usr/bin/rd-tool", Source: "x"}}}
	assert.ErrorContains(t, Apply(openImage(t, img), m, source, testMtime), "htree-indexed")
	fsck(t, img)
}

// writeImageFile creates p in the image with one Write per chunk.
func writeImageFile(t *testing.T, fs *ext4.FileSystem, p string, chunks ...string) {
	t.Helper()
	f, err := fs.OpenFile(p, os.O_CREATE|os.O_RDWR)
	assert.NilError(t, err)
	for _, c := range chunks {
		_, err := f.Write([]byte(c))
		assert.NilError(t, err)
	}
	assert.NilError(t, f.Close())
}

// mkfsImage formats a 64 MiB ext4 image populated from root, with metadata_csum
// as in the distro.
func mkfsImage(t *testing.T, root string) string {
	t.Helper()
	img := filepath.Join(t.TempDir(), "root.ext4")
	code, out := e2fsprogs(t, "mke2fs", "-q", "-F", "-t", "ext4", "-b", "4096", "-O", "metadata_csum",
		"-d", root, img, "64M")
	assert.Equal(t, code, 0, out)
	return img
}

// fsck asserts that e2fsck finds no errors in the image.
func fsck(t *testing.T, img string) {
	t.Helper()
	code, out := e2fsprogs(t, "e2fsck", "-fn", img)
	assert.Equal(t, code, 0, out)
}

// e2fsprogs runs an e2fsprogs command and returns its exit code and output. It
// skips the test where e2fsprogs is missing, unless RDD_REQUIRE_E2FSPROGS is
// true, which CI sets where it provides e2fsprogs.
//
// cmd/distro-overlay keeps its own copy. Two of them beat a test-helper package
// that nothing else would use, so long as the RDD_REQUIRE_E2FSPROGS contract
// stays the same in both; a third caller is when to extract it.
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

// openImage opens an ext4 filesystem image for the image backend to write into.
func openImage(t *testing.T, path string) *imageDistro {
	t.Helper()
	d, err := diskfs.Open(path, diskfs.WithOpenMode(diskfs.ReadWrite))
	assert.NilError(t, err)
	t.Cleanup(func() {
		// Apply closes the disk, but a test that stops before calling it leaves
		// the disk open, and a second Close panics.
		if d.Backend != nil {
			_ = d.Close()
		}
	})
	fsi, err := d.GetFilesystem(0)
	assert.NilError(t, err)
	img, err := newImageDistro(d, fsi.(*ext4.FileSystem), 0)
	assert.NilError(t, err)
	return img
}

// newImage creates an empty ext4 image and returns an imageDistro writing into it.
func newImage(t *testing.T, path string) *imageDistro {
	t.Helper()
	d, err := diskfs.Create(path, 64*1024*1024, diskfs.SectorSizeDefault)
	assert.NilError(t, err)
	t.Cleanup(func() {
		// Apply closes the disk, but a test that stops before calling it leaves
		// the disk open, and a second Close panics.
		if d.Backend != nil {
			_ = d.Close()
		}
	})
	fsi, err := d.CreateFilesystem(disk.FilesystemSpec{Partition: 0, FSType: filesystem.TypeExt4})
	assert.NilError(t, err)
	img, err := newImageDistro(d, fsi.(*ext4.FileSystem), 0)
	assert.NilError(t, err)
	return img
}

// openImageFS reopens an ext4 image read-only for inspection. The disk handle is
// closed at test end so Windows can delete the image in TempDir cleanup.
func openImageFS(t *testing.T, path string) *ext4.FileSystem {
	t.Helper()
	d, err := diskfs.Open(path, diskfs.WithOpenMode(diskfs.ReadOnly))
	assert.NilError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	fsi, err := d.GetFilesystem(0)
	assert.NilError(t, err)
	return fsi.(*ext4.FileSystem)
}

type tarItem struct {
	name string
	body string
	dir  bool
	link string // symlink target, or hard-link target when hard is set
	hard bool
}

func buildTar(t *testing.T, items []tarItem) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, it := range items {
		hdr := &tar.Header{Name: it.name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(it.body))}
		if it.dir {
			hdr.Typeflag, hdr.Mode, hdr.Size = tar.TypeDir, 0o755, 0
		}
		if it.link != "" {
			hdr.Typeflag, hdr.Linkname, hdr.Mode = tar.TypeSymlink, it.link, 0o777
			if it.hard {
				hdr.Typeflag, hdr.Mode = tar.TypeLink, 0o755
			}
		}
		assert.NilError(t, tw.WriteHeader(hdr))
		_, err := tw.Write([]byte(it.body))
		assert.NilError(t, err)
	}
	assert.NilError(t, tw.Close())
	return buf.Bytes()
}

type tarEntry struct {
	hdr  tar.Header
	body string
}

func readTar(t *testing.T, data []byte) map[string]tarEntry {
	t.Helper()
	out := map[string]tarEntry{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		assert.NilError(t, err)
		body, err := io.ReadAll(tr)
		assert.NilError(t, err)
		name := hdr.Name
		if hdr.Typeflag == tar.TypeDir {
			name = name[:len(name)-1] // drop trailing slash for lookup
		}
		out[name] = tarEntry{hdr: *hdr, body: string(body)}
	}
	return out
}

func writeSource(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	assert.NilError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	assert.NilError(t, os.WriteFile(full, []byte(body), 0o644))
}

func count(data []byte, name string) int {
	n := 0
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		if hdr.Name == name {
			n++
		}
	}
	return n
}
