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
// manual is more careful. Windows has no fenced block like the Unix startup
// files, so we can't tell an entry we added from one the user typed by value
// alone. We therefore only remove a binDir entry that sits at the very front or
// back of the Path — the two positions front/back would have written. An entry
// the user placed in the middle is a deliberate choice, so manual leaves it
// alone. "Middle" means a real, unrelated entry sits on both sides of ours:
// anchoring on the other entries (not the raw front/back of the Path) makes one
// pass a fixed point, so a duplicated entry that would otherwise slide to an edge
// on the next apply is removed now. The consequence, which the docs call out, is
// that a user who wants binDir at the front or back must use front/back: under
// manual an entry parked at either end is treated as ours and removed. Empty
// segments are only tidied up when we're actually adding our entry.
//
// forceRemove overrides the manual middle-entry rule and drops every binDir
// entry regardless of position. Service deletion uses it to fully unwind before
// removing the bin directory, so a middle entry isn't left pointing at a
// directory that no longer exists.
func computeWindowsPath(current, binDir string, strategy appv1alpha1.AddPathStrategy, forceRemove bool) (string, bool) {
	segments := strings.Split(current, ";")
	wanted := normalizeWindowsPathEntry(binDir)
	adding := strategy == appv1alpha1.AddPathFront || strategy == appv1alpha1.AddPathBack

	// First and last non-empty segments that aren't ours anchor the "middle": our
	// entry is a deliberate placement (kept under manual) only when one of these
	// sits before it and another after it. Computing this against the other
	// entries — rather than the raw front/back — means removing one copy can't
	// promote a second copy to an edge on a later pass.
	firstOtherIdx, lastOtherIdx := -1, -1
	for i, seg := range segments {
		trimmed := strings.TrimSpace(seg)
		if trimmed == "" || strings.EqualFold(normalizeWindowsPathEntry(trimmed), wanted) {
			continue
		}
		if firstOtherIdx < 0 {
			firstOtherIdx = i
		}
		lastOtherIdx = i
	}

	kept := make([]string, 0, len(segments)+1)
	for i, seg := range segments {
		trimmed := strings.TrimSpace(seg)
		if trimmed != "" && strings.EqualFold(normalizeWindowsPathEntry(trimmed), wanted) {
			// Our entry. front/back and forceRemove drop every copy; manual keeps
			// it only when it's genuinely in the middle — a non-ours entry on each
			// side of it.
			middle := firstOtherIdx >= 0 && i > firstOtherIdx && i < lastOtherIdx
			if adding || forceRemove || !middle {
				continue
			}
			// A middle entry under manual: the user's chosen location, kept.
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
