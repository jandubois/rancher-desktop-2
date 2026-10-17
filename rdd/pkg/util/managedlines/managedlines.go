// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package managedlines adds or removes a fenced block of lines in a text file,
// idempotently. It's a Go port of Rancher Desktop 1's manageLinesInFile, and we
// use it to put the Rancher Desktop bin directory on PATH via the user's shell
// startup files.
//
// The block sits between caller-supplied start/end marker lines, so different
// tools (and different Rancher Desktop instances) can edit the same file without
// stepping on each other. Text outside the markers is left as-is, except that
// leading and trailing blank lines get trimmed.
package managedlines

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/util/atomicfile"
)

// fileMode is used for startup files we create.
const fileMode = 0o644

// ErrMalformedBlock marks a file the user hand-edited into a state we can't
// parse: an unbalanced marker (a lone or unclosed marker, or a stray one left
// beside a block), or the end marker before the start. It's permanent —
// only the user can fix the file, so retrying can't clear the block — which lets
// callers tell it apart from a transient I/O error and stop retrying forever.
var ErrMalformedBlock = errors.New("managed block is malformed")

// Manage adds (present=true) or removes (present=false) managedLines between
// startMarker and endMarker in the file at path. The write is atomic, and text
// outside the block isn't touched.
//
// If removing the block leaves the file empty, we write an empty file rather
// than deleting it. We can't tell a file we created from one the user already
// had — an empty ~/.zshrc or ~/.bashrc is ordinary, and zsh even writes one when
// the user declines its setup wizard — so deleting it could destroy the user's
// file or resurface that wizard. A file that doesn't exist yet is left alone,
// since there's nothing to remove.
func Manage(path, startMarker, endMarker string, managedLines []string, present bool) error {
	desired := desiredLines(startMarker, endMarker, managedLines, present)

	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	target, changed, err := computeTargetContents(string(current), startMarker, endMarker, desired)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if !changed {
		return nil
	}

	// An empty result means we just removed the last of our own content. Don't
	// create a file that wasn't there; otherwise write the empty file, leaving a
	// pre-existing (possibly empty) startup file in place instead of deleting it.
	if target == "" {
		if _, statErr := os.Lstat(path); os.IsNotExist(statErr) {
			return nil
		}
	}
	return atomicfile.Write(path, []byte(target), fileMode)
}

// desiredLines returns the full block (markers included) we want in the file, or
// nil when there shouldn't be one.
func desiredLines(startMarker, endMarker string, managedLines []string, present bool) []string {
	if !present || len(managedLines) == 0 {
		return nil
	}
	block := make([]string, 0, len(managedLines)+2)
	block = append(block, startMarker)
	block = append(block, managedLines...)
	block = append(block, endMarker)
	return block
}

// markerSplit is the result of splitting a file around the managed block.
type markerSplit struct {
	before, managed, after []string
	// hasBlock is true when a start/end pair was found.
	hasBlock bool
	// duplicates is true when more than one block was present and the extras were
	// collapsed away, so the caller must rewrite even if the first block matched.
	duplicates bool
}

// splitByMarkers splits lines into the parts before, inside, and after the
// block, and reports whether the markers were there at all. With no markers, the
// whole file counts as "before".
//
// It scans in a single pass and rejects any unbalanced marker with
// ErrMalformedBlock: a start with no matching end, an end with no open start, or
// a start nested inside another. This keeps removal and the next pass consistent
// — a file we can't cleanly parse is reported to the user rather than half-edited
// into a lone marker that a later pass would then reject.
//
// A file should only ever hold one block, but if it somehow ends up with more
// than one complete block (a bad merge, a hand-edit, a past bug), the extra pairs
// are collapsed: managed is the first block's content, after has every later
// complete pair stripped out, and duplicates says a pair was removed. Without
// this a duplicated block is a permanent fixed point — the first pair matches
// what we want, so we'd never rewrite, and the directory would stay on PATH twice
// forever.
func splitByMarkers(lines []string, startMarker, endMarker string) (markerSplit, error) {
	malformed := func(format string, args ...any) (markerSplit, error) {
		return markerSplit{}, fmt.Errorf("%w: %s", ErrMalformedBlock, fmt.Sprintf(format, args...))
	}

	var blocks [][2]int
	inBlock := false
	start := -1
	for i, line := range lines {
		switch line {
		case startMarker:
			if inBlock {
				return malformed("line %d opens a block before the one opened on line %d is closed", i+1, start+1)
			}
			inBlock = true
			start = i
		case endMarker:
			if !inBlock {
				return malformed("line %d closes a block that was never opened", i+1)
			}
			inBlock = false
			blocks = append(blocks, [2]int{start, i})
		}
	}
	if inBlock {
		return malformed("the block opened on line %d is never closed", start+1)
	}

	if len(blocks) == 0 {
		return markerSplit{before: lines}, nil
	}

	first := blocks[0]
	// Collapse any later complete blocks by skipping their line ranges, so the
	// content between them is kept but a second copy of our block is dropped.
	after := make([]string, 0, len(lines))
	next := 1
	for i := first[1] + 1; i < len(lines); i++ {
		if next < len(blocks) && i == blocks[next][0] {
			i = blocks[next][1] // jump to the end marker; the loop's i++ moves past it
			next++
			continue
		}
		after = append(after, lines[i])
	}
	return markerSplit{
		before:     lines[:first[0]],
		managed:    lines[first[0]+1 : first[1]],
		after:      after,
		hasBlock:   true,
		duplicates: len(blocks) > 1,
	}, nil
}

// computeTargetContents works out what the file should contain, given its
// current contents and the desired block (markers included). changed is false
// when nothing needs to be written. The result has no leading blank lines and
// ends in a single newline; an empty result is "".
func computeTargetContents(current, startMarker, endMarker string, desired []string) (target string, changed bool, err error) {
	split, err := splitByMarkers(strings.Split(current, "\n"), startMarker, endMarker)
	if err != nil {
		return "", false, err
	}

	var currentBlock []string
	if split.hasBlock {
		currentBlock = append(append([]string{startMarker}, split.managed...), endMarker)
	}
	// Skip the rewrite only when the single existing block already matches. If we
	// collapsed duplicate blocks, the first one may match while extra copies still
	// need stripping, so always write in that case.
	if !split.duplicates && slices.Equal(currentBlock, desired) {
		return "", false, nil
	}

	lines := slices.Concat(split.before, desired, split.after)

	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return "", true, nil
	}
	return strings.Join(lines, "\n") + "\n", true, nil
}
