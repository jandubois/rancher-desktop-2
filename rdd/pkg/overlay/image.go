// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package overlay

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/ext4"
)

// imageDistro writes into the ext4 root partition of an OEM disk image. It
// fills free space the distro reserves at build time; it does not grow the
// filesystem, so an overlay larger than that reserve fails with ENOSPC.
//
// go-diskfs writes extent-tree blocks without their metadata_csum checksum,
// ignores O_TRUNC, writes through symlinks, and corrupts htree-indexed
// directories it adds entries to, so imageDistro refuses those writes. It finds
// an extent tree only after go-diskfs has written it, so that refusal leaves the
// image corrupt.
type imageDistro struct {
	disk           *disk.Disk
	fs             *ext4.FileSystem
	blockSize      int64
	blocksPerGroup int64
}

// OpenImage opens a raw disk image for in-place modification and locates its
// ext4 root partition.
func OpenImage(p string) (Distro, error) {
	d, err := diskfs.Open(p, diskfs.WithOpenMode(diskfs.ReadWrite))
	if err != nil {
		return nil, err
	}
	table, err := d.GetPartitionTable()
	if err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("reading partition table: %w", err)
	}
	// Keep the last read failure: an unreadable partition and an image without
	// one look the same from here, and only the error tells them apart.
	var unread error
	for i, part := range table.GetPartitions() {
		candidate, err := d.GetFilesystem(i + 1)
		if err != nil {
			unread = fmt.Errorf("reading partition %d: %w", i+1, err)
			continue
		}
		if candidate.Type() != filesystem.TypeExt4 {
			continue
		}
		if root, ok := candidate.(*ext4.FileSystem); ok {
			img, err := newImageDistro(d, root, part.GetStart())
			if err != nil {
				_ = d.Close()
				return nil, err
			}
			return img, nil
		}
	}
	_ = d.Close()
	if unread != nil {
		return nil, fmt.Errorf("no ext4 root partition in %s: %w", p, unread)
	}
	return nil, fmt.Errorf("no ext4 root partition in %s", p)
}

// Superblock and group descriptor fields the overlay reads itself, because
// go-diskfs keeps its own parse private.
const (
	sbLogBlockSize    = 0x18
	sbBlocksCountLo   = 0x04
	sbBlocksCountHi   = 0x150
	sbFirstDataBlock  = 0x14
	sbBlocksPerGroup  = 0x20
	sbMagic           = 0x38
	sbFeatureIncompat = 0x60
	sbDescSize        = 0xfe
	gdFlags           = 0x12

	featureIncompat64Bit   = 0x80
	groupBlockBitmapUninit = 0x0002
)

// newImageDistro wraps the ext4 filesystem at byte offset start of d, reading the
// geometry it needs from the superblock and checking every block group.
func newImageDistro(d *disk.Disk, root *ext4.FileSystem, start int64) (*imageDistro, error) {
	var sb [1024]byte
	if _, err := d.Backend.ReadAt(sb[:], start+1024); err != nil {
		return nil, fmt.Errorf("reading the ext4 superblock: %w", err)
	}
	if binary.LittleEndian.Uint16(sb[sbMagic:]) != 0xef53 {
		return nil, fmt.Errorf("no ext4 superblock at offset %d", start)
	}
	img := &imageDistro{
		disk:           d,
		fs:             root,
		blockSize:      1024 << binary.LittleEndian.Uint32(sb[sbLogBlockSize:]),
		blocksPerGroup: int64(binary.LittleEndian.Uint32(sb[sbBlocksPerGroup:])),
	}
	if img.blocksPerGroup <= 0 {
		return nil, fmt.Errorf("the ext4 superblock reports %d blocks per group", img.blocksPerGroup)
	}
	descSize, err := img.checkGroupDescriptors(&sb)
	if err != nil {
		return nil, err
	}
	if err := img.checkGroupsInitialized(&sb, start, descSize); err != nil {
		return nil, err
	}
	return img, nil
}

// checkGroupDescriptors refuses an image whose group descriptors are smaller
// than the 64 bytes go-diskfs slices out of every one it rewrites, so go-diskfs
// panics part-way through a write. It returns the size for the group scan.
func (i *imageDistro) checkGroupDescriptors(sb *[1024]byte) (int64, error) {
	incompat := binary.LittleEndian.Uint32(sb[sbFeatureIncompat:])
	if incompat&featureIncompat64Bit == 0 {
		return 0, errors.New("the image is not 64bit ext4, and go-diskfs crashes on its 32-byte group " +
			"descriptors")
	}
	descSize := int64(binary.LittleEndian.Uint16(sb[sbDescSize:]))
	if descSize < 64 {
		return 0, fmt.Errorf("the ext4 superblock reports %d-byte group descriptors", descSize)
	}
	return descSize, nil
}

// checkGroupsInitialized refuses an image holding a BLOCK_UNINIT group.
// go-diskfs allocates into such a group without clearing the flag, and e2fsck
// and the kernel then rebuild that group's bitmap from metadata alone, leaving
// the blocks it wrote marked free for the next writer to take.
//
// INODE_UNINIT carries the same defect and is not checked, because the distro
// images ship with such groups and nothing has reached them: go-diskfs allocates
// inodes from group 0 up, and the early groups still have free ones.
func (i *imageDistro) checkGroupsInitialized(sb *[1024]byte, start, descSize int64) error {
	blocks := int64(binary.LittleEndian.Uint32(sb[sbBlocksCountLo:])) |
		int64(binary.LittleEndian.Uint32(sb[sbBlocksCountHi:]))<<32
	first := int64(binary.LittleEndian.Uint32(sb[sbFirstDataBlock:]))
	groups := (blocks - first + i.blocksPerGroup - 1) / i.blocksPerGroup
	table := start + (first+1)*i.blockSize
	desc := make([]byte, descSize)
	for g := range groups {
		if _, err := i.disk.Backend.ReadAt(desc, table+g*descSize); err != nil {
			return fmt.Errorf("reading the descriptor of block group %d: %w", g, err)
		}
		if binary.LittleEndian.Uint16(desc[gdFlags:])&groupBlockBitmapUninit != 0 {
			return fmt.Errorf("block group %d is BLOCK_UNINIT, and go-diskfs allocates into it "+
				"without clearing the flag", g)
		}
	}
	return nil
}

// rel converts an absolute distro path to the unrooted form go-diskfs expects.
func rel(p string) string {
	return strings.TrimPrefix(path.Clean(p), "/")
}

func (i *imageDistro) EnsureDir(dir string, uid, gid int, mode os.FileMode, mtime time.Time, force bool) error {
	r := rel(dir)
	if r == "" || r == "." {
		return nil // the root directory always exists
	}
	if info, err := i.fs.Stat(r); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s exists and is not a directory", dir)
		}
		if !force {
			return nil
		}
		return i.setMeta(r, uid, gid, mode, mtime)
	}
	// Create each missing ancestor explicitly so it gets a deterministic mtime;
	// go-diskfs Mkdir would otherwise stamp intermediate dirs with time.Now().
	if parent := path.Dir(r); parent != "." {
		if err := i.EnsureDir("/"+parent, 0, 0, 0o755, mtime, false); err != nil {
			return err
		}
	}
	if err := i.checkParentIndex(r); err != nil {
		return err
	}
	if err := i.fs.Mkdir(r); err != nil {
		return err
	}
	return i.setMeta(r, uid, gid, mode, mtime)
}

// WriteFile hands go-diskfs the whole file in one Write, because go-diskfs
// allocates each Write as its own extent. One allocation gets the file a single
// extent wherever a free run is large enough, so it needs no extent tree.
func (i *imageDistro) WriteFile(file string, contents io.Reader, uid, gid int, mode os.FileMode, mtime time.Time) error {
	data, err := io.ReadAll(contents)
	if err != nil {
		return err
	}
	r := rel(file)
	if info, st, err := i.stat(r); err == nil {
		if err := i.checkOverride(info, st, int64(len(data))); err != nil {
			return err
		}
	} else if err := i.checkParentIndex(r); err != nil {
		return err
	}
	f, err := i.fs.OpenFile(r, os.O_CREATE|os.O_RDWR|os.O_TRUNC)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := i.setMeta(r, uid, gid, mode, mtime); err != nil {
		return err
	}
	info, st, err := i.stat(r)
	if err != nil {
		return err
	}
	if i.hasExtentTree(info, st) {
		return errors.New("the file needs more than the four extents an inode holds, so go-diskfs gave " +
			"it an extent tree without a checksum; see the tool's README for what drives that")
	}
	return nil
}

func (i *imageDistro) Symlink(link, target string, mtime time.Time) error {
	r := rel(link)
	if _, _, err := i.stat(r); err == nil {
		return errors.New("the image already has that path, and go-diskfs cannot replace it with a symlink")
	}
	if err := i.checkParentIndex(r); err != nil {
		return err
	}
	if err := i.fs.Symlink(target, r); err != nil {
		return err
	}
	// Stamp the link's own mtime for a reproducible image. Chtimes writes the
	// link inode without following it; ownership is left at the go-diskfs default
	// because Chown would follow the link to its target.
	return i.fs.Chtimes(r, mtime, mtime, mtime)
}

func (i *imageDistro) Close() error {
	return i.disk.Close()
}

// setMeta applies ownership, permissions, and the modification time.
func (i *imageDistro) setMeta(r string, uid, gid int, mode os.FileMode, mtime time.Time) error {
	if err := i.fs.Chmod(r, mode); err != nil {
		return err
	}
	if err := i.fs.Chown(r, uid, gid); err != nil {
		return err
	}
	return i.fs.Chtimes(r, mtime, mtime, mtime)
}

// stat returns the file info for r, without following a symlink, and the ext4
// metadata behind it.
func (i *imageDistro) stat(r string) (os.FileInfo, *ext4.StatT, error) {
	info, err := i.fs.Stat(r)
	if err != nil {
		return nil, nil, err
	}
	st, ok := info.Sys().(*ext4.StatT)
	if !ok {
		return nil, nil, fmt.Errorf("%s: go-diskfs returned no ext4 metadata", r)
	}
	return info, st, nil
}

// checkParentIndex refuses to add r to an htree-indexed directory, which
// go-diskfs would rewrite as a plain list while keeping the index flag.
func (i *imageDistro) checkParentIndex(r string) error {
	parent := path.Dir(r)
	if parent == "." {
		parent = "/"
	}
	_, st, err := i.stat(parent)
	if err != nil {
		return err
	}
	if st.Flags.HashedIndexes {
		return fmt.Errorf("go-diskfs cannot add entries to the htree-indexed directory /%s", strings.TrimPrefix(parent, "/"))
	}
	return nil
}

// checkOverride refuses to replace an image file in the ways go-diskfs gets
// wrong. go-diskfs ignores O_TRUNC, so a shorter file would keep the old tail.
// The file is rewritten in place, so its other hard links would change too.
func (i *imageDistro) checkOverride(info os.FileInfo, st *ext4.StatT, size int64) error {
	switch {
	case info.IsDir():
		return errors.New("the image has a directory there")
	case info.Mode()&os.ModeSymlink != 0:
		return errors.New("the image has a symlink there, which go-diskfs would write through")
	case st.Nlink > 1:
		return fmt.Errorf("the image's file has %d hard links, which would all get the new contents", st.Nlink)
	case size < info.Size():
		return fmt.Errorf("go-diskfs cannot shrink the image's %d-byte file to %d bytes", info.Size(), size)
	case i.hasExtentTree(info, st):
		return errors.New("the image's file has an extent tree or an xattr block, which the tool does not rewrite")
	case i.hasHoles(info, st):
		return errors.New("the image's file has holes, and go-diskfs would write the new contents " +
			"outside the file")
	}
	return nil
}

// hasExtentTree reports whether a file has blocks beyond its data. Those are
// extent-tree blocks, which a file needs once its extents outgrow the four that
// fit in its inode. An external xattr block counts too, so a file with one looks
// like it has a tree.
func (i *imageDistro) hasExtentTree(info os.FileInfo, st *ext4.StatT) bool {
	allocated, data := i.blockBytes(info, st)
	return allocated > data
}

// hasHoles reports whether a file has fewer blocks than its size needs.
// go-diskfs numbers a grown file's new extent from the blocks it already owns
// rather than from the end of its last one, and writes each extent as if the
// file began there, so a hole sends the new contents outside the file.
//
// This and hasExtentTree read the same block count from opposite sides, so a
// file carrying both a hole and one metadata block passes each; i_blocks alone
// cannot tell that apart, and the images the tool writes into have no such file.
func (i *imageDistro) hasHoles(info os.FileInfo, st *ext4.StatT) bool {
	allocated, data := i.blockBytes(info, st)
	return allocated < data
}

// blockBytes returns the bytes a file's blocks take up and the bytes its size
// needs, rounded up to whole blocks.
func (i *imageDistro) blockBytes(info os.FileInfo, st *ext4.StatT) (allocated, data int64) {
	unit := int64(512)
	if st.Flags.HugeFile {
		unit = i.blockSize
	}
	return int64(st.Blocks) * unit, (info.Size() + i.blockSize - 1) / i.blockSize * i.blockSize
}
