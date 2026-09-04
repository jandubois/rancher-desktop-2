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
// parse: a lone marker, or the end marker before the start. It's permanent —
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
// A file should only ever hold one block, but if it somehow ends up with more
// than one (a bad merge, a hand-edit, a past bug), the extra pairs are collapsed:
// managed is the first block's content, after has every later complete pair
// stripped out, and duplicates says a pair was removed. Without this a duplicated
// block is a permanent fixed point — the first pair matches what we want, so we'd
// never rewrite, and the directory would stay on PATH twice forever. A lone,
// unbalanced marker is left as ordinary text (kept in before/after), same as
// before.
func splitByMarkers(lines []string, startMarker, endMarker string) (markerSplit, error) {
	startIdx := slices.Index(lines, startMarker)
	endIdx := slices.Index(lines, endMarker)

	switch {
	case startIdx < 0 && endIdx < 0:
		return markerSplit{before: lines}, nil
	case startIdx < 0 || endIdx < 0:
		return markerSplit{}, fmt.Errorf("%w: exactly one of the delimiter lines is present", ErrMalformedBlock)
	case startIdx >= endIdx:
		return markerSplit{}, fmt.Errorf("%w: the delimiter lines are in the wrong order", ErrMalformedBlock)
	}

	rest := lines[endIdx+1:]
	after := stripBlocks(rest, startMarker, endMarker)
	return markerSplit{
		before:     lines[:startIdx],
		managed:    lines[startIdx+1 : endIdx],
		after:      after,
		hasBlock:   true,
		duplicates: len(after) != len(rest),
	}, nil
}

// stripBlocks removes every complete start/end marker pair (and the lines
// between) from lines, leaving all other content in place. A lone, unbalanced
// marker is treated as ordinary text and kept, matching splitByMarkers's
// leniency. It's used to collapse a file that ended up with the managed block
// more than once down to a single block.
func stripBlocks(lines []string, startMarker, endMarker string) []string {
	result := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if lines[i] == startMarker {
			if rel := slices.Index(lines[i+1:], endMarker); rel >= 0 {
				i += rel + 1 // skip to the end marker; the loop's i++ moves past it
				continue
			}
		}
		result = append(result, lines[i])
	}
	return result
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
