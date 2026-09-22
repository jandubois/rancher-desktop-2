// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package overlay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// testGrubConfig has the tab-indented `linux` line per menu entry that
// grub2-mkconfig writes. The recovery entry ends in the trailing space the real
// config has, escaped so that stripping trailing whitespace cannot drop it.
const testGrubConfig = "set timeout=10\n" +
	"menuentry 'openSUSE Leap 16.0' {\n" +
	"\tlinux\t/boot/Image-6.12.0-default root=UUID=1234 console=hvc0\n" +
	"\tinitrd\t/boot/initrd-6.12.0-default\n" +
	"}\n" +
	"menuentry 'openSUSE Leap 16.0 (recovery mode)' {\n" +
	"\tlinux\t/boot/Image-6.12.0-default root=UUID=1234 single \n" +
	"\tinitrd\t/boot/initrd-6.12.0-default\n" +
	"}\n"

func TestAppendKernelParamsAddsToEveryEntry(t *testing.T) {
	img := seedGrubConfig(t, testGrubConfig)

	assert.NilError(t, AppendKernelParams(img, "tsc_early_khz=3192000", testMtime))

	got := grubConfigContents(t, img)
	assert.Check(t, strings.Contains(got, "root=UUID=1234 console=hvc0 tsc_early_khz=3192000\n"))
	assert.Check(t, strings.Contains(got, "root=UUID=1234 single tsc_early_khz=3192000\n"))
	// Every other line survives, the initrd lines among them.
	assert.Equal(t, strings.Count(got, "\tinitrd\t/boot/initrd-6.12.0-default\n"), 2)
	assert.Check(t, strings.HasPrefix(got, "set timeout=10\n"))

	info, st, err := img.stat("boot/grub2/grub.cfg")
	assert.NilError(t, err)
	assert.Equal(t, info.Mode().Perm(), os.FileMode(0o600))
	assert.Equal(t, int(st.UID), 0)
	assert.Equal(t, info.ModTime().Unix(), testMtime.Unix())
}

func TestAppendKernelParamsNeedsABootMenuItKnows(t *testing.T) {
	img := seedGrubConfig(t, "set timeout=10\n")
	assert.ErrorContains(t, AppendKernelParams(img, "quiet", testMtime), "no linux line")
}

func TestAppendKernelParamsRejectsWhatItCannotAppend(t *testing.T) {
	for name, tc := range map[string]struct {
		params string
		want   string
	}{
		"empty":       {params: "  ", want: "no kernel parameters"},
		"second line": {params: "quiet\nmenuentry 'evil' {", want: "cannot span lines"},
	} {
		t.Run(name, func(t *testing.T) {
			img := seedGrubConfig(t, testGrubConfig)
			assert.ErrorContains(t, AppendKernelParams(img, tc.params, testMtime), tc.want)
			assert.Equal(t, grubConfigContents(t, img), testGrubConfig)
		})
	}
}

func TestAppendKernelParamsRefusesATarball(t *testing.T) {
	d := &tarDistro{}
	assert.ErrorContains(t, AppendKernelParams(d, "quiet", testMtime), "no bootloader config")
}

// seedGrubConfig returns an image holding cfg at the path GRUB reads, owned by
// root and mode 0600 as the distro ships it.
func seedGrubConfig(t *testing.T, cfg string) *imageDistro {
	t.Helper()
	img := newImage(t, filepath.Join(t.TempDir(), "distro.raw"))
	assert.NilError(t, img.fs.Mkdir("boot"))
	assert.NilError(t, img.fs.Mkdir("boot/grub2"))
	writeImageFile(t, img.fs, "boot/grub2/grub.cfg", cfg)
	assert.NilError(t, img.fs.Chmod("boot/grub2/grub.cfg", 0o600))
	return img
}

func grubConfigContents(t *testing.T, img *imageDistro) string {
	t.Helper()
	data, err := img.readFile("boot/grub2/grub.cfg")
	assert.NilError(t, err)
	return string(data)
}
