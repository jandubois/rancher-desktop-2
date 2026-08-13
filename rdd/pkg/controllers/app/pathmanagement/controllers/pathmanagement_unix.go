// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

//go:build !windows

package controllers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/util/managedlines"
)

// startMarker and endMarker fence the block we manage in a shell startup file.
// The suffix keeps our markers distinct from other instances and from Rancher
// Desktop 1 (which uses "RANCHER DESKTOP" with no number).
func (r *PathManagementReconciler) startMarker() string {
	return fmt.Sprintf("### MANAGED BY RANCHER DESKTOP %s START (DO NOT EDIT)", r.Suffix)
}

func (r *PathManagementReconciler) endMarker() string {
	return fmt.Sprintf("### MANAGED BY RANCHER DESKTOP %s END (DO NOT EDIT)", r.Suffix)
}

// applyPath enforces the strategy by editing the user's shell startup files. The
// forceRemove flag (used when unwinding for deletion) is ignored here: the Unix
// files are fenced with start/end markers, so removing the block already takes
// out only our own lines regardless of where they sit. It matters only on
// Windows, where the Path has no such fence.
//
// The caller (apply) holds a cross-process lock, so the read-compute-write in
// managedlines.Manage can't lose a concurrent instance's block from a shared
// startup file.
func (r *PathManagementReconciler) applyPath(_ context.Context, strategy appv1alpha1.AddPathStrategy, _ bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("locate home directory: %w", err)
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	return applyPosix(home, configHome, r.BinDir, strategy, r.startMarker(), r.endMarker())
}

// applyPosix mirrors Rancher Desktop 1's RcFilePathManager: it manages the bash
// login-shell files (only the first existing one carries the block), the bash
// and zsh rc files, the csh rc files, and the fish config. A failure on one file
// doesn't stop the rest; the errors are joined so the caller sees every offender
// and the remaining files still converge.
func applyPosix(home, configHome, binDir string, strategy appv1alpha1.AddPathStrategy, start, end string) error {
	present := strategy != appv1alpha1.AddPathManual
	posixLine := posixPathLine(binDir, strategy)
	var errs []error

	// Bash login files, in precedence order. Only the first existing file gets the
	// block (so PATH isn't extended twice); the rest have it removed.
	loginFiles := []string{".bash_profile", ".bash_login", ".profile"}
	if present {
		var blockInfo os.FileInfo
		handled := false
		loginExisted := false
		for _, name := range loginFiles {
			p := filepath.Join(home, name)
			info, err := os.Stat(p)
			if err != nil {
				if os.IsNotExist(err) {
					// Rancher Desktop 1 skips login files that aren't there
					// rather than creating all of them; do the same.
					continue
				}
				errs = append(errs, err)
				continue
			}
			loginExisted = true
			if handled {
				if blockInfo != nil && os.SameFile(blockInfo, info) {
					// A later name resolving to the file we already wrote (e.g.
					// .bash_profile symlinked to .profile); leave that block alone.
					continue
				}
				// A distinct, later login file: strip any block an earlier run
				// left, since only the first existing file carries ours.
				if err := managedlines.Manage(p, start, end, []string{posixLine}, false); err != nil {
					errs = append(errs, err)
				}
				continue
			}
			// First existing login file: it carries the block. Mark it handled
			// before the write so a failure here doesn't push the block onto a
			// later file that bash never sources (it reads only the first one).
			handled = true
			if err := managedlines.Manage(p, start, end, []string{posixLine}, true); err != nil {
				errs = append(errs, err)
				continue
			}
			// The atomic write replaces the file, so re-stat to capture the
			// identity that later names are compared against.
			if blockInfo, err = os.Stat(p); err != nil {
				errs = append(errs, err)
				// Without blockInfo we can't tell a later symlink to this file
				// from a distinct one, so stop rather than risk writing the
				// block twice and doubling the PATH entry.
				break
			}
		}
		if !loginExisted {
			p := filepath.Join(home, loginFiles[0])
			if err := managedlines.Manage(p, start, end, []string{posixLine}, true); err != nil {
				errs = append(errs, err)
			}
		}
	} else {
		for _, name := range loginFiles {
			p := filepath.Join(home, name)
			if err := managedlines.Manage(p, start, end, nil, false); err != nil {
				errs = append(errs, err)
			}
		}
	}

	// bash and zsh rc files always carry the block (or have it removed).
	for _, name := range []string{".bashrc", ".zshrc"} {
		p := filepath.Join(home, name)
		if err := managedlines.Manage(p, start, end, []string{posixLine}, present); err != nil {
			errs = append(errs, err)
		}
	}

	// csh reads .cshrc; tcsh reads .tcshrc if it exists, otherwise .cshrc. Manage
	// .cshrc always, but only touch .tcshrc when the user already has one, so we
	// don't create a .tcshrc that shadows their existing .cshrc.
	cshLine := cshPathLine(binDir, strategy)
	if err := managedlines.Manage(filepath.Join(home, ".cshrc"), start, end, []string{cshLine}, present); err != nil {
		errs = append(errs, err)
	}
	tcshrc := filepath.Join(home, ".tcshrc")
	if tcshExists, err := fileExists(tcshrc); err != nil {
		errs = append(errs, err)
	} else if tcshExists {
		if err := managedlines.Manage(tcshrc, start, end, []string{cshLine}, present); err != nil {
			errs = append(errs, err)
		}
	}

	// fish.
	fishDir := filepath.Join(configHome, "fish")
	fishPath := filepath.Join(fishDir, "config.fish")
	if present {
		if err := os.MkdirAll(fishDir, 0o700); err != nil {
			errs = append(errs, fmt.Errorf("create fish config directory: %w", err))
		}
	}
	if err := managedlines.Manage(fishPath, start, end, []string{fishPathLine(binDir, strategy)}, present); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// fileExists reports whether path resolves to an existing file (following
// symlinks). A missing file is not an error.
func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// posixPathLine returns the POSIX-shell line adding binDir to PATH.
func posixPathLine(binDir string, strategy appv1alpha1.AddPathStrategy) string {
	if strategy == appv1alpha1.AddPathBack {
		return fmt.Sprintf(`export PATH="$PATH:%s"`, binDir)
	}
	return fmt.Sprintf(`export PATH="%s:$PATH"`, binDir)
}

// cshPathLine returns the csh/tcsh line adding binDir to PATH.
func cshPathLine(binDir string, strategy appv1alpha1.AddPathStrategy) string {
	if strategy == appv1alpha1.AddPathBack {
		return fmt.Sprintf(`setenv PATH "$PATH":"%s"`, binDir)
	}
	return fmt.Sprintf(`setenv PATH "%s":"$PATH"`, binDir)
}

// fishPathLine returns the fish line adding binDir to PATH.
func fishPathLine(binDir string, strategy appv1alpha1.AddPathStrategy) string {
	op := "--prepend"
	if strategy == appv1alpha1.AddPathBack {
		op = "--append"
	}
	return fmt.Sprintf(`set --export %s PATH "%s"`, op, binDir)
}
