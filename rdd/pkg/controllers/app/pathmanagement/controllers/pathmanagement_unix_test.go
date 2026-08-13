// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

//go:build !windows

package controllers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
)

const (
	testStart = "### MANAGED BY RANCHER DESKTOP 2 START (DO NOT EDIT)"
	testEnd   = "### MANAGED BY RANCHER DESKTOP 2 END (DO NOT EDIT)"
	testBin   = "/home/me/.rd2/bin"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	assert.NilError(t, err)
	return string(data)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestUnwindRemovesBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg-config-home"))
	r := &PathManagementReconciler{BinDir: filepath.Join(home, ".rd2", "bin"), Suffix: "2"}

	zshrc := filepath.Join(home, ".zshrc")
	block := r.startMarker() + "\nexport PATH=\"" + r.BinDir + ":$PATH\"\n" + r.endMarker() + "\n"
	assert.NilError(t, os.WriteFile(zshrc, []byte("keep\n"+block), 0o644))

	assert.NilError(t, r.Unwind(context.Background()))

	// The block is gone; the user's own content stays.
	assert.Equal(t, readFile(t, zshrc), "keep\n")
}

func TestApplyPosixFrontCreatesBashProfile(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "xdg-config-home")

	err := applyPosix(home, configHome, testBin, appv1alpha1.AddPathFront, testStart, testEnd)
	assert.NilError(t, err)

	// No login file existed, so .bash_profile is created; the others are not.
	assert.Assert(t, exists(filepath.Join(home, ".bash_profile")))
	assert.Assert(t, !exists(filepath.Join(home, ".bash_login")))
	assert.Assert(t, !exists(filepath.Join(home, ".profile")))

	got := readFile(t, filepath.Join(home, ".bash_profile"))
	assert.Equal(t, got, testStart+"\nexport PATH=\""+testBin+":$PATH\"\n"+testEnd+"\n")

	// .bashrc and .zshrc always get the line.
	assert.Equal(t, readFile(t, filepath.Join(home, ".bashrc")),
		testStart+"\nexport PATH=\""+testBin+":$PATH\"\n"+testEnd+"\n")
	assert.Equal(t, readFile(t, filepath.Join(home, ".zshrc")),
		testStart+"\nexport PATH=\""+testBin+":$PATH\"\n"+testEnd+"\n")

	// csh and fish.
	assert.Equal(t, readFile(t, filepath.Join(home, ".cshrc")),
		testStart+"\nsetenv PATH \""+testBin+"\":\"$PATH\"\n"+testEnd+"\n")
	assert.Equal(t, readFile(t, filepath.Join(configHome, "fish", "config.fish")),
		testStart+"\nset --export --prepend PATH \""+testBin+"\"\n"+testEnd+"\n")
}

func TestApplyPosixBackAppends(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "xdg-config-home")

	err := applyPosix(home, configHome, testBin, appv1alpha1.AddPathBack, testStart, testEnd)
	assert.NilError(t, err)

	assert.Equal(t, readFile(t, filepath.Join(home, ".bashrc")),
		testStart+"\nexport PATH=\"$PATH:"+testBin+"\"\n"+testEnd+"\n")
	assert.Equal(t, readFile(t, filepath.Join(home, ".cshrc")),
		testStart+"\nsetenv PATH \"$PATH\":\""+testBin+"\"\n"+testEnd+"\n")
	assert.Equal(t, readFile(t, filepath.Join(configHome, "fish", "config.fish")),
		testStart+"\nset --export --append PATH \""+testBin+"\"\n"+testEnd+"\n")
}

func TestApplyPosixFirstExistingLoginFileWins(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "xdg-config-home")
	// .profile exists but .bash_profile/.bash_login do not: only .profile gets it.
	assert.NilError(t, os.WriteFile(filepath.Join(home, ".profile"), []byte("# mine\n"), 0o644))

	err := applyPosix(home, configHome, testBin, appv1alpha1.AddPathFront, testStart, testEnd)
	assert.NilError(t, err)

	assert.Assert(t, !exists(filepath.Join(home, ".bash_profile")))
	assert.Assert(t, !exists(filepath.Join(home, ".bash_login")))
	got := readFile(t, filepath.Join(home, ".profile"))
	assert.Equal(t, got, "# mine\n\n"+testStart+"\nexport PATH=\""+testBin+":$PATH\"\n"+testEnd+"\n")
}

func TestApplyPosixOnlyFirstOfMultipleLoginFiles(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "xdg-config-home")
	assert.NilError(t, os.WriteFile(filepath.Join(home, ".bash_profile"), []byte("a\n"), 0o644))
	assert.NilError(t, os.WriteFile(filepath.Join(home, ".profile"), []byte("b\n"), 0o644))

	err := applyPosix(home, configHome, testBin, appv1alpha1.AddPathFront, testStart, testEnd)
	assert.NilError(t, err)

	// .bash_profile (first in precedence) carries the block; .profile is left clean.
	assert.Equal(t, readFile(t, filepath.Join(home, ".bash_profile")),
		"a\n\n"+testStart+"\nexport PATH=\""+testBin+":$PATH\"\n"+testEnd+"\n")
	assert.Equal(t, readFile(t, filepath.Join(home, ".profile")), "b\n")
}

func TestApplyPosixDoesNotCreateTcshrc(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "xdg-config-home")
	assert.NilError(t, os.WriteFile(filepath.Join(home, ".cshrc"),
		[]byte("setenv EDITOR vim\n"), 0o644))

	err := applyPosix(home, configHome, testBin, appv1alpha1.AddPathFront, testStart, testEnd)
	assert.NilError(t, err)

	// .cshrc keeps the user's content and gets the block; .tcshrc is not created,
	// so it can't shadow .cshrc for tcsh.
	assert.Equal(t, readFile(t, filepath.Join(home, ".cshrc")),
		"setenv EDITOR vim\n\n"+testStart+"\nsetenv PATH \""+testBin+"\":\"$PATH\"\n"+testEnd+"\n")
	assert.Assert(t, !exists(filepath.Join(home, ".tcshrc")))
}

func TestApplyPosixUpdatesExistingTcshrc(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "xdg-config-home")
	assert.NilError(t, os.WriteFile(filepath.Join(home, ".tcshrc"),
		[]byte("alias ll ls -l\n"), 0o644))

	err := applyPosix(home, configHome, testBin, appv1alpha1.AddPathFront, testStart, testEnd)
	assert.NilError(t, err)

	// An existing .tcshrc gets the block, since tcsh reads it instead of .cshrc.
	assert.Equal(t, readFile(t, filepath.Join(home, ".tcshrc")),
		"alias ll ls -l\n\n"+testStart+"\nsetenv PATH \""+testBin+"\":\"$PATH\"\n"+testEnd+"\n")
}

func TestApplyPosixFirstLoginFileFailureDoesNotMoveBlock(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "xdg-config-home")
	// The first login file bash sources, .bash_profile, has a dangling start
	// marker, so Manage can't parse it. .bash_login exists and is clean.
	bad := filepath.Join(home, ".bash_profile")
	assert.NilError(t, os.WriteFile(bad, []byte("# mine\n"+testStart+"\nexport PATH=x\n"), 0o644))
	login := filepath.Join(home, ".bash_login")
	assert.NilError(t, os.WriteFile(login, []byte("# mine\n"), 0o644))

	err := applyPosix(home, configHome, testBin, appv1alpha1.AddPathFront, testStart, testEnd)
	assert.ErrorContains(t, err, bad)

	// The block must not migrate to .bash_login: bash reads only .bash_profile,
	// so the block there would never be sourced, and .bash_login is edited
	// against the user's intent.
	assert.Assert(t, !strings.Contains(readFile(t, login), testStart))
}

func TestApplyPosixSymlinkedLoginFile(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "xdg-config-home")
	// .bash_profile is a symlink to .profile, a common dotfiles layout. The block
	// must land in .profile and stay there, not get removed by the later name.
	assert.NilError(t, os.WriteFile(filepath.Join(home, ".profile"), []byte("# user profile\n"), 0o644))
	assert.NilError(t, os.Symlink(filepath.Join(home, ".profile"), filepath.Join(home, ".bash_profile")))

	err := applyPosix(home, configHome, testBin, appv1alpha1.AddPathFront, testStart, testEnd)
	assert.NilError(t, err)

	got := readFile(t, filepath.Join(home, ".profile"))
	assert.Equal(t, got, "# user profile\n\n"+testStart+"\nexport PATH=\""+testBin+":$PATH\"\n"+testEnd+"\n")
	// The symlink is preserved and still points at .profile.
	info, err := os.Lstat(filepath.Join(home, ".bash_profile"))
	assert.NilError(t, err)
	assert.Assert(t, info.Mode()&os.ModeSymlink != 0)
}

func TestApplyPosixConvergesPastMalformedFile(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "xdg-config-home")
	// The user hand-deleted the end marker from .bash_profile, leaving a dangling
	// start marker.
	bad := filepath.Join(home, ".bash_profile")
	assert.NilError(t, os.WriteFile(bad, []byte("# mine\n"+testStart+"\nexport PATH=x\n"), 0o644))

	err := applyPosix(home, configHome, testBin, appv1alpha1.AddPathFront, testStart, testEnd)
	// The error surfaces and names the offending file.
	assert.ErrorContains(t, err, bad)

	// The other shells still get managed despite the bad file.
	block := testStart + "\nexport PATH=\"" + testBin + ":$PATH\"\n" + testEnd + "\n"
	assert.Equal(t, readFile(t, filepath.Join(home, ".bashrc")), block)
	assert.Equal(t, readFile(t, filepath.Join(home, ".zshrc")), block)
	assert.Assert(t, exists(filepath.Join(home, ".cshrc")))
	assert.Assert(t, exists(filepath.Join(configHome, "fish", "config.fish")))
}

func TestApplyPosixManualEmptiesFilesWeCreated(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "xdg-config-home")

	assert.NilError(t, applyPosix(home, configHome, testBin, appv1alpha1.AddPathFront, testStart, testEnd))
	assert.NilError(t, applyPosix(home, configHome, testBin, appv1alpha1.AddPathManual, testStart, testEnd))

	// We remove our block but leave the files in place (empty), since we can't
	// tell one we created from an empty startup file the user already had.
	assert.Equal(t, readFile(t, filepath.Join(home, ".bash_profile")), "")
	assert.Equal(t, readFile(t, filepath.Join(home, ".bashrc")), "")
	assert.Equal(t, readFile(t, filepath.Join(configHome, "fish", "config.fish")), "")
}

func TestApplyPosixManualPreservesUserContent(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, "xdg-config-home")
	assert.NilError(t, os.WriteFile(filepath.Join(home, ".bashrc"), []byte("user line\n"), 0o644))

	assert.NilError(t, applyPosix(home, configHome, testBin, appv1alpha1.AddPathFront, testStart, testEnd))
	assert.NilError(t, applyPosix(home, configHome, testBin, appv1alpha1.AddPathManual, testStart, testEnd))

	assert.Equal(t, readFile(t, filepath.Join(home, ".bashrc")), "user line\n")
}
