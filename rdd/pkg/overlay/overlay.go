// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package overlay

import (
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"time"
)

// Tree members get these modes whatever the source's are, because a Windows
// build host reports every file as 0666 and every directory as 0777.
const (
	treeFileMode os.FileMode = 0o644
	treeDirMode  os.FileMode = 0o755
)

// Distro is a mutable distro root. The tarball and ext4 image backends both
// implement it, so Apply drives them identically.
type Distro interface {
	// EnsureDir creates dir and any missing parents. When force is true it applies
	// uid/gid/mode/mtime even to a directory that already exists; otherwise it
	// leaves an existing directory untouched.
	EnsureDir(dir string, uid, gid int, mode os.FileMode, mtime time.Time, force bool) error
	// WriteFile creates or replaces a regular file with the given contents.
	WriteFile(file string, contents io.Reader, uid, gid int, mode os.FileMode, mtime time.Time) error
	// Symlink creates a symbolic link pointing to target. A link takes no owner,
	// because go-diskfs cannot set a link's own; the manifest rejects one.
	Symlink(link, target string, mtime time.Time) error
	// Close flushes pending changes.
	Close() error
}

// Apply writes every manifest entry into the distro, stamping mtime on each, and
// closes it, also when it fails. It creates the directories first, with their
// owner and mode resolved up front, then writes the files and symlinks, so an
// explicit entry overrides a path a tree copy would also write, whatever the
// manifest order. Overriding a tree's directory takes a dir entry; a file or
// symlink there is a type conflict that both backends refuse.
func Apply(d Distro, m *Manifest, sourceDir string, mtime time.Time) (err error) {
	defer func() {
		if closeErr := d.Close(); err == nil {
			err = closeErr
		}
	}()
	// Both backends rely on the rules validate enforces, and either entry point
	// can be called without LoadManifest.
	if err = m.validate(); err != nil {
		return err
	}
	owned := entryPaths(m)
	roots := treeRoots(m)
	dirs, err := plannedDirs(m, sourceDir, roots)
	if err != nil {
		return err
	}
	// Create directories parents-first so each is written once with its final
	// owner and mode, before any file or symlink creates the same path implicitly.
	for _, dir := range sortedKeys(dirs) {
		meta := dirs[dir]
		if err := d.EnsureDir(dir, meta.uid, meta.gid, meta.mode, mtime, true); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}
	for i := range m.Entries {
		e := &m.Entries[i]
		switch {
		case e.kind() == TypeDir && e.Source == "":
			continue // created above
		case e.kind() == TypeDir:
			if err := writeTreeFiles(d, e, sourceDir, mtime, owned, roots); err != nil {
				return err
			}
		case e.kind() == TypeSymlink:
			if err := writeParent(d, e.Path, mtime); err != nil {
				return err
			}
			if err := d.Symlink(e.Path, e.Target, mtime); err != nil {
				return fmt.Errorf("creating symlink %s: %w", e.Path, err)
			}
		default: // TypeFile
			if err := writeParent(d, e.Path, mtime); err != nil {
				return err
			}
			src, err := sourcePath(sourceDir, e.Source)
			if err != nil {
				return err
			}
			if err := writeFile(d, e.Path, src, e.UID, e.GID, modeOr(e, 0o644), mtime); err != nil {
				return err
			}
		}
	}
	return nil
}

// dirMeta is the resolved owner and mode for a directory the overlay creates.
type dirMeta struct {
	uid, gid int
	mode     os.FileMode
}

// plannedDirs resolves the directories the manifest names, the members of its
// tree copies and its dir entries, to their final owner and mode. Tree
// subdirectories come first; every dir entry, tree roots included, then overrides
// any path it shares with a tree, so explicit ownership always wins. The
// ancestors that files and symlinks need are not here: writeParent creates those
// as root:root 0755 as it reaches them.
func plannedDirs(m *Manifest, sourceDir string, roots map[string]bool) (map[string]dirMeta, error) {
	dirs := map[string]dirMeta{}
	for i := range m.Entries {
		e := &m.Entries[i]
		if e.kind() != TypeDir || e.Source == "" {
			continue
		}
		members, err := walkTree(sourceDir, e)
		if err != nil {
			return nil, err
		}
		for _, mem := range members {
			if mem.isDir && ownedByTree(mem.dest, e.Path, roots) {
				dirs[mem.dest] = dirMeta{e.UID, e.GID, treeDirMode}
			}
		}
	}
	for i := range m.Entries {
		e := &m.Entries[i]
		if e.kind() == TypeDir {
			dirs[e.Path] = dirMeta{e.UID, e.GID, modeOr(e, 0o755)}
		}
	}
	return dirs, nil
}

// writeTreeFiles writes the files under a tree copy, skipping any path an
// explicit entry or a nested tree owns. The tree's directories are created
// earlier by plannedDirs.
func writeTreeFiles(d Distro, e *Entry, sourceDir string, mtime time.Time, owned, roots map[string]bool) error {
	members, err := walkTree(sourceDir, e)
	if err != nil {
		return err
	}
	for _, mem := range members {
		if mem.isDir || owned[mem.dest] || !ownedByTree(mem.dest, e.Path, roots) {
			continue
		}
		if err := writeFile(d, mem.dest, mem.srcPath, e.UID, e.GID, treeFileMode, mtime); err != nil {
			return err
		}
	}
	return nil
}

// entryPaths is the set of destinations the manifest writes explicitly, so a tree
// copy can yield a colliding member to the entry that names it.
func entryPaths(m *Manifest) map[string]bool {
	set := make(map[string]bool, len(m.Entries))
	for i := range m.Entries {
		set[m.Entries[i].Path] = true
	}
	return set
}

// treeRoots is the set of dir entries that copy a source tree.
func treeRoots(m *Manifest) map[string]bool {
	set := map[string]bool{}
	for i := range m.Entries {
		if e := &m.Entries[i]; e.kind() == TypeDir && e.Source != "" {
			set[e.Path] = true
		}
	}
	return set
}

// ownedByTree reports whether the tree rooted at root writes dest, rather than a
// tree nested between the two, so a nested tree keeps its own paths in any
// manifest order. It looks only at the directories above dest, so a nested tree's
// own root still answers true; plannedDirs gives that path the nested entry's
// owner and mode in its second pass.
func ownedByTree(dest, root string, roots map[string]bool) bool {
	// path.Dir("/") is "/", so the loop stops at the filesystem root too. A dest
	// outside the tree would otherwise never reach root.
	for p := path.Dir(dest); p != root && p != "/" && p != "."; p = path.Dir(p) {
		if roots[p] {
			return false
		}
	}
	return true
}

// sortedKeys returns the directory paths sorted so each ancestor precedes its
// descendants, which lexical order guarantees (a path sorts before its extensions).
func sortedKeys(dirs map[string]dirMeta) []string {
	return slices.Sorted(maps.Keys(dirs))
}

// writeParent creates the parent directory of dest with default ownership, leaving
// a directory another entry already established untouched.
func writeParent(d Distro, dest string, mtime time.Time) error {
	if err := d.EnsureDir(path.Dir(dest), 0, 0, 0o755, mtime, false); err != nil {
		return fmt.Errorf("creating parent of %s: %w", dest, err)
	}
	return nil
}

// treeMember is one file or directory found under a copied source tree.
type treeMember struct {
	dest    string // absolute destination path inside the distro
	srcPath string // absolute path of the source on the build host
	isDir   bool
}

// walkTree lists every member under a dir entry's source tree, excluding the
// tree root itself. Members are ordered parents before children. A tree holds
// only files and directories, because a Windows checkout without symlink
// support turns a symlink into a plain file.
func walkTree(sourceDir string, e *Entry) ([]treeMember, error) {
	root, err := sourcePath(sourceDir, e.Source)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s: the source of a dir entry must be a directory", root)
	}
	var members []treeMember
	err = filepath.WalkDir(root, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil // the root is created by the caller
		}
		if !de.IsDir() && !de.Type().IsRegular() {
			return fmt.Errorf("%s: a tree holds only files and directories; add symlinks as symlink entries", p)
		}
		members = append(members, treeMember{
			dest:    path.Join(e.Path, filepath.ToSlash(rel)),
			srcPath: p,
			isDir:   de.IsDir(),
		})
		return nil
	})
	return members, err
}

// modeOr returns the entry's mode, or def when it is unset. The mode string is
// validated when the manifest loads, so parsing cannot fail here.
func modeOr(e *Entry, def os.FileMode) os.FileMode {
	mode, _ := e.mode(def)
	return mode
}

func writeFile(d Distro, dest, srcPath string, uid, gid int, mode os.FileMode, mtime time.Time) error {
	// Opening the source would follow a symlink out of the source directory, and
	// a fifo would block the build with nothing written and no timeout.
	info, err := os.Lstat(srcPath)
	switch {
	case err != nil:
		return fmt.Errorf("opening source for %s: %w", dest, err)
	case info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("%s: a source may not be a symlink; add symlinks as symlink entries", srcPath)
	case !info.Mode().IsRegular():
		return fmt.Errorf("%s: a source must be a regular file, not a %s", srcPath, sourceKind(info.Mode()))
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("opening source for %s: %w", dest, err)
	}
	defer src.Close()
	if err := d.WriteFile(dest, src, uid, gid, mode, mtime); err != nil {
		return fmt.Errorf("writing file %s: %w", dest, err)
	}
	return nil
}

// sourceKind names a file type for an error message.
func sourceKind(mode os.FileMode) string {
	switch {
	case mode.IsDir():
		return "directory"
	case mode&os.ModeNamedPipe != 0:
		return "fifo"
	case mode&os.ModeSocket != 0:
		return "socket"
	case mode&os.ModeDevice != 0:
		return "device node"
	default:
		return "special file"
	}
}

// sourcePath resolves an entry's source to a path on the build host, refusing
// one that a symlink in a parent component takes outside sourceDir. The
// manifest's own check is lexical and Lstat sees only the final component, so a
// symlinked parent escapes both.
func sourcePath(sourceDir, source string) (string, error) {
	p := filepath.Join(sourceDir, filepath.FromSlash(source))
	root, err := filepath.EvalSymlinks(sourceDir)
	if err != nil {
		return "", fmt.Errorf("resolving source directory %s: %w", sourceDir, err)
	}
	// Resolve the parent only, so the caller's own Lstat still sees a symlink
	// that is the source itself and refuses it by name. A source naming the
	// source directory is already the root, and its parent is outside it.
	parent := filepath.Dir(p)
	if p == filepath.Clean(sourceDir) {
		parent = p
	}
	dir, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", fmt.Errorf("resolving source for %s: %w", source, err)
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || (rel != "." && !filepath.IsLocal(rel)) {
		return "", fmt.Errorf("%s: a source may not leave the source directory", source)
	}
	return p, nil
}
