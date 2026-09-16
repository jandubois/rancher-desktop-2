// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package overlay

import (
	"archive/tar"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"slices"
	"strings"
	"time"
)

// tarDistro appends overlay entries to an output tar stream.
type tarDistro struct {
	tw      *tar.Writer
	base    map[string]byte // tar type flag of each path in the base tarball
	written map[string]byte // tar type flag of each path this backend has written
}

// ApplyTar copies the base tarball to out, dropping any path the manifest
// overrides, then appends the overlay entries stamped with mtime.
func ApplyTar(base io.Reader, out io.Writer, m *Manifest, sourceDir string, mtime time.Time) error {
	// overridePaths walks the source trees, so the manifest has to hold up before
	// it runs; Apply validates again for callers that reach it directly.
	if err := m.validate(); err != nil {
		return err
	}
	override, err := overridePaths(m, sourceDir)
	if err != nil {
		return err
	}
	tr := tar.NewReader(base)
	tw := tar.NewWriter(out)
	baseTypes := map[string]byte{}
	linked := map[string]bool{} // both ends of every base hard link
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading base tarball: %w", err)
		}
		name := tarClean(hdr.Name)
		baseTypes[name] = hdr.Typeflag
		if hdr.Typeflag == tar.TypeLink {
			linked[name] = true
			linked[tarClean(hdr.Linkname)] = true
		}
		if override[name] {
			continue // the overlay replaces this path
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return err
		}
	}
	// Dropping a link target leaves the links naming a missing entry, and the
	// image backend refuses hard-linked files, so refuse them here too. Report
	// the first in path order, because a map would name a different one per run.
	for _, p := range slices.Sorted(maps.Keys(override)) {
		if linked[p] {
			return fmt.Errorf("/%s is hard-linked in the base, and the overlay cannot replace it", p)
		}
	}
	d := &tarDistro{tw: tw, base: baseTypes, written: map[string]byte{}}
	return Apply(d, m, sourceDir, mtime)
}

// overridePaths returns every destination the manifest writes, so ApplyTar can
// drop the matching base entries. A dir entry that copies a source tree expands
// to the members it writes, which excludes those a nested tree owns: dropping a
// base path the overlay then skips would delete it from the distro.
func overridePaths(m *Manifest, sourceDir string) (map[string]bool, error) {
	set := make(map[string]bool, len(m.Entries))
	roots := treeRoots(m)
	for i := range m.Entries {
		e := &m.Entries[i]
		set[tarClean(e.Path)] = true
		if e.kind() == TypeDir && e.Source != "" {
			members, err := walkTree(sourceDir, e)
			if err != nil {
				return nil, err
			}
			for _, mem := range members {
				if ownedByTree(mem.dest, e.Path, roots) {
					set[tarClean(mem.dest)] = true
				}
			}
		}
	}
	return set, nil
}

// tarClean turns a path into the relative, slash-separated form tar uses.
func tarClean(p string) string {
	return strings.TrimPrefix(path.Clean("/"+p), "/")
}

func (t *tarDistro) EnsureDir(dir string, uid, gid int, mode os.FileMode, mtime time.Time, force bool) error {
	name := tarClean(dir)
	if name == "" {
		return nil
	}
	// Emit missing ancestors first, with default ownership, so the tar carries a
	// full directory chain rather than relying on the extractor's umask.
	if parent := path.Dir(name); parent != "." {
		if err := t.EnsureDir("/"+parent, 0, 0, 0o755, mtime, false); err != nil {
			return err
		}
	}
	// Like the image backend, refuse a directory where the base has a symlink
	// such as lib -> usr/lib, which an extractor would replace with the directory.
	typ, inBase := t.base[name]
	if inBase && typ != tar.TypeDir {
		return fmt.Errorf("%s exists in the base and is not a directory", dir)
	}
	if wrote, ok := t.written[name]; ok {
		if wrote != tar.TypeDir {
			return fmt.Errorf("%s is a %s the overlay writes", dir, tarKind(wrote))
		}
		return nil
	}
	if inBase && !force {
		return nil
	}
	t.written[name] = tar.TypeDir
	return t.tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     name + "/",
		Mode:     int64(mode.Perm()),
		Uid:      uid,
		Gid:      gid,
		ModTime:  mtime,
	})
}

func (t *tarDistro) WriteFile(file string, contents io.Reader, uid, gid int, mode os.FileMode, mtime time.Time) error {
	if err := t.checkType(file, tar.TypeReg); err != nil {
		return err
	}
	buf, err := io.ReadAll(contents)
	if err != nil {
		return err
	}
	t.written[tarClean(file)] = tar.TypeReg
	if err := t.tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     tarClean(file),
		Mode:     int64(mode.Perm()),
		Uid:      uid,
		Gid:      gid,
		Size:     int64(len(buf)),
		ModTime:  mtime,
	}); err != nil {
		return err
	}
	_, err = t.tw.Write(buf)
	return err
}

func (t *tarDistro) Symlink(link, target string, mtime time.Time) error {
	if err := t.checkType(link, tar.TypeSymlink); err != nil {
		return err
	}
	t.written[tarClean(link)] = tar.TypeSymlink
	// The header leaves the link root-owned, while the image backend gives it
	// the owner of its parent directory.
	return t.tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeSymlink,
		Name:     tarClean(link),
		Linkname: target,
		Mode:     0o777,
		ModTime:  mtime,
	})
}

func (t *tarDistro) Close() error {
	return t.tw.Close()
}

// checkType refuses to write p as typ where the overlay already wrote another
// type, or where the base has a directory. Extracting such an archive either
// fails, because tar refuses to replace a populated directory, or silently
// drops whichever entry came first. A base symlink or file the overlay replaces
// with another type still passes here, though the image backend refuses it.
func (t *tarDistro) checkType(p string, typ byte) error {
	name := tarClean(p)
	if t.base[name] == tar.TypeDir {
		return fmt.Errorf("%s is a directory in the base", p)
	}
	if wrote, ok := t.written[name]; ok && wrote != typ {
		return fmt.Errorf("%s is a %s the overlay writes", p, tarKind(wrote))
	}
	return nil
}

// tarKind names a tar type flag for an error message.
func tarKind(typ byte) string {
	switch typ {
	case tar.TypeDir:
		return "directory"
	case tar.TypeSymlink:
		return "symlink"
	default:
		return "file"
	}
}
