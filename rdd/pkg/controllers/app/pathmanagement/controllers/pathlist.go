// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"strings"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
)

// computeWindowsPath returns the new user Path after applying strategy, and
// whether it changed. It takes the raw semicolon-separated Path so we can test
// it off Windows.
//
// front and back drop every existing binDir entry (case-insensitive, to match
// Windows) and re-add it at the chosen end, so the result is idempotent and the
// entry can move between front and back.
//
// manual's behaviour depends on removeExisting, which the caller derives from
// the marker file (see pathmanagement_windows.go). Windows Path has no fenced
// block like the Unix startup files, so we can't tell an entry we added from one
// the user typed by value alone; the marker records that rdd added it. When
// removeExisting is true (the marker is present, or we're force-removing for
// deletion) every binDir entry is dropped regardless of position. When it is
// false (no marker) every entry is left alone, wherever the user put it. Empty
// segments are only tidied up when we're actually adding our entry.
func computeWindowsPath(current, binDir string, strategy appv1alpha1.AddPathStrategy, removeExisting bool) (string, bool) {
	segments := strings.Split(current, ";")
	wanted := normalizeWindowsPathEntry(binDir)
	adding := strategy == appv1alpha1.AddPathFront || strategy == appv1alpha1.AddPathBack
	// Drop our entries when adding (front/back re-add one below) or when the
	// caller says the marker owns them (manual with the marker, or force-remove).
	removeOurs := adding || removeExisting

	kept := make([]string, 0, len(segments)+1)
	for _, seg := range segments {
		trimmed := strings.TrimSpace(seg)
		isOurs := trimmed != "" && strings.EqualFold(normalizeWindowsPathEntry(trimmed), wanted)
		if isOurs && removeOurs {
			continue
		}
		if trimmed == "" && adding {
			// Empty segments just leave stray semicolons; drop them when adding.
			continue
		}
		kept = append(kept, seg)
	}

	switch strategy {
	case appv1alpha1.AddPathFront:
		kept = append([]string{binDir}, kept...)
	case appv1alpha1.AddPathBack:
		kept = append(kept, binDir)
	}

	updated := strings.Join(kept, ";")
	return updated, updated != current
}

// normalizeWindowsPathEntry rewrites a Path entry so slash direction and a
// trailing separator stop mattering: C:/foo/bin and C:\foo\bin\ both come out
// as C:\foo\bin. filepath.Clean would handle this on Windows, but off Windows
// it leaves backslash paths alone, and these tests run off Windows. Callers
// still compare case-insensitively.
func normalizeWindowsPathEntry(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	// Strip trailing separators, but leave a drive root like C:\ alone.
	for len(p) > 3 && p[len(p)-1] == '\\' {
		p = p[:len(p)-1]
	}
	return p
}
