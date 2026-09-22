// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package overlay layers Rancher Desktop assets onto a pristine openSUSE distro
// at build time, writing into both the WSL tarball and the Lima ext4 image from
// a single manifest. Ownership and permissions come from the manifest, so the
// build needs no root and the host's own uids never reach the distro.
package overlay

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"

	"sigs.k8s.io/yaml"
)

// maxID is the largest uid or gid ext4 stores; the tarball would carry a larger
// one that the image silently truncates. It is typed, because it overflows an
// int where that is 32 bits.
const maxID int64 = 1<<32 - 1

// Entry types.
const (
	TypeFile    = "file"
	TypeDir     = "dir"
	TypeSymlink = "symlink"
)

// Build profiles are a mirror of config.kiwi's
// profiles= attribute.
const (
	ProfileLima = "lima"
	ProfileWSL  = "wsl"
)

// ValidProfile reports whether profile is a build profile the overlay knows.
// distro-overlay checks its --profile flag with it, and the manifest checks
// each entry's profiles field, so a typo fails the build instead of silently
// dropping the entry.
func ValidProfile(profile string) bool {
	return profile == ProfileLima || profile == ProfileWSL
}

// Manifest describes the assets to merge into a distro.
type Manifest struct {
	Entries []Entry `json:"entries"`
}

// Entry is a single file, directory, or symlink to place in the distro.
type Entry struct {
	// Path is the absolute destination inside the distro root.
	Path string `json:"path"`
	// Type is file (the default), dir, or symlink.
	Type string `json:"type,omitempty"`
	// Source names the contents as a path relative to the source directory: a
	// file for a file entry, or a directory tree to copy for a dir entry.
	Source string `json:"source,omitempty"`
	// Target is the symlink target, stored verbatim.
	Target string `json:"target,omitempty"`
	// UID and GID own the entry; both default to 0 (root).
	UID int `json:"uid,omitempty"`
	GID int `json:"gid,omitempty"`
	// Mode is an octal string of permission bits such as "0755"; it defaults to
	// 0644 for files and 0755 for directories.
	Mode string `json:"mode,omitempty"`
	// Profiles limits the entry to some of the distro's build profiles, each
	// lima or wsl, mirroring config.kiwi. Empty applies the entry to every one.
	Profiles []string `json:"profiles,omitempty"`
}

// LoadManifest reads and validates a YAML manifest.
func LoadManifest(p string) (*Manifest, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := yaml.UnmarshalStrict(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", p, err)
	}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("invalid manifest %s: %w", p, err)
	}
	return &m, nil
}

func (m *Manifest) validate() error {
	seen := make(map[string]int, len(m.Entries))
	for i := range m.Entries {
		if err := m.Entries[i].validate(); err != nil {
			return fmt.Errorf("entry %d (%s): %w", i, m.Entries[i].Path, err)
		}
		// The backends cannot agree on a repeated path: the tarball carries both
		// entries, while the image applies the second over the first or refuses
		// it, depending on what the two entries are.
		if first, dup := seen[m.Entries[i].Path]; dup {
			return fmt.Errorf("entry %d (%s): path is already used by entry %d", i, m.Entries[i].Path, first)
		}
		seen[m.Entries[i].Path] = i
	}
	return nil
}

// kind returns the entry type, defaulting to TypeFile.
func (e *Entry) kind() string {
	if e.Type == "" {
		return TypeFile
	}
	return e.Type
}

func (e *Entry) validate() error {
	if !path.IsAbs(e.Path) || e.Path != path.Clean(e.Path) {
		return errors.New("path must be absolute and clean")
	}
	switch e.kind() {
	case TypeFile:
		if e.Source == "" {
			return errors.New("file entry needs a source")
		}
		if e.Target != "" {
			return errors.New("file entry cannot have a target")
		}
	case TypeDir:
		if e.Target != "" {
			return errors.New("dir entry cannot have a target")
		}
	case TypeSymlink:
		if e.Target == "" {
			return errors.New("symlink entry needs a target")
		}
		if e.Source != "" {
			return errors.New("symlink entry cannot have a source")
		}
		// The image backend cannot own a symlink: go-diskfs gives the link its
		// parent's owner, and Chown would follow it to its target.
		if e.UID != 0 || e.GID != 0 {
			return errors.New("symlink entry cannot set uid or gid")
		}
	default:
		return fmt.Errorf("unknown type %q (want file, dir, or symlink)", e.Type)
	}
	// IsLocal rejects what ValidPath allows on a Windows build host, where
	// filepath.Join reads a backslash in the source as a separator.
	if e.Source != "" && (!fs.ValidPath(e.Source) || !filepath.IsLocal(filepath.FromSlash(e.Source))) {
		return errors.New("source must be relative and within the source directory")
	}
	if e.UID < 0 || int64(e.UID) > maxID || e.GID < 0 || int64(e.GID) > maxID {
		return fmt.Errorf("uid and gid must be between 0 and %d", maxID)
	}
	if e.Mode != "" {
		if _, err := e.mode(0); err != nil {
			return fmt.Errorf("invalid mode %q: %w", e.Mode, err)
		}
	}
	for _, p := range e.Profiles {
		if !ValidProfile(p) {
			return fmt.Errorf("unknown profile %q (want %s or %s)", p, ProfileLima, ProfileWSL)
		}
	}
	return nil
}

// ForProfile returns a manifest holding only the entries that apply to profile,
// which is how distro-overlay builds one profile's image from a manifest both
// share. An entry naming no profile applies to every one. An empty profile
// returns m itself, so a caller that does not build per profile gets the whole
// manifest back.
func (m *Manifest) ForProfile(profile string) *Manifest {
	if profile == "" {
		return m
	}
	var entries []Entry
	for i := range m.Entries {
		if m.Entries[i].hasProfile(profile) {
			entries = append(entries, m.Entries[i])
		}
	}
	return &Manifest{Entries: entries}
}

func (e *Entry) hasProfile(profile string) bool {
	return len(e.Profiles) == 0 || slices.Contains(e.Profiles, profile)
}

// mode parses the octal Mode, returning def when it is unset.
func (e *Entry) mode(def os.FileMode) (os.FileMode, error) {
	if e.Mode == "" {
		return def, nil
	}
	v, err := strconv.ParseUint(e.Mode, 8, 32)
	if err != nil {
		return 0, err
	}
	if v > 0o777 {
		return 0, errors.New("only permission bits (0777) are supported")
	}
	return os.FileMode(v), nil
}
