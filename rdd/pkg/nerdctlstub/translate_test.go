// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package nerdctlstub

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestexec"
)

func TestVolumeArgHandler(t *testing.T) {
	cases := []struct {
		arg  string
		want string
	}{
		{`C:\data:/data`, "/mnt/c/data:/data"},
		{`C:\data:/data:ro`, "/mnt/c/data:/data:ro"},
		{`C:\data:/data:rw`, "/mnt/c/data:/data:rw"},
		{`myvol:/data`, "myvol:/data"},
		{`my-vol_1.0:/data:ro`, "my-vol_1.0:/data:ro"},
		// Anonymous volumes reach nerdctl unchanged.
		{`no-separator`, "no-separator"},
		{`:foo`, ":foo"},
	}
	for _, tc := range cases {
		t.Run(tc.arg, func(t *testing.T) {
			got, err := volumeArgHandler(tc.arg)
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}

	for _, arg := range []string{`C:\data`, `C:/data`, `d:app`} {
		_, err := volumeArgHandler(arg)
		assert.ErrorContains(t, err, "expected source:container-path", "%s", arg)
	}
}

func TestVolumeArgHandlerModes(t *testing.T) {
	cases := []struct {
		arg  string
		want string
	}{
		{`/data`, "/data"},
		{`:/data`, ":/data"},
		{`myvol:/data:z`, "myvol:/data:z"},
		{`myvol:/data:rro`, "myvol:/data:rro"},
		{`myvol:/data:ro,rshared`, "myvol:/data:ro,rshared"},
		{`C:\data:/data:z`, "/mnt/c/data:/data:z"},
		{`C:\src:/data:z:ro`, "/mnt/c/src:/data:z:ro"},
	}
	for _, tc := range cases {
		t.Run(tc.arg, func(t *testing.T) {
			got, err := volumeArgHandler(tc.arg)
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}

	// The drive-relative source `d:app` must reach TranslateHostPath whole.
	want, err := guestexec.TranslateHostPath("d:app")
	assert.NilError(t, err)
	got, err := volumeArgHandler("d:app:/data")
	assert.NilError(t, err)
	assert.Equal(t, got, want+":/data")
}

func TestMountArgHandler(t *testing.T) {
	cases := []struct {
		name string
		arg  string
		want string
	}{
		{
			name: "bind mount",
			arg:  `type=bind,source=C:\x,target=/y`,
			want: "type=bind,source=/mnt/c/x,target=/y",
		},
		{
			name: "bind mount with src key and bare option",
			arg:  `type=bind,src=C:\x,target=/y,readonly`,
			want: "type=bind,src=/mnt/c/x,target=/y,readonly",
		},
		{
			name: "volume name stays unchanged",
			arg:  `type=volume,source=vol,target=/y`,
			want: "type=volume,source=vol,target=/y",
		},
		{
			name: "bind mount of a volume name stays unchanged",
			arg:  `type=bind,source=vol,target=/y`,
			want: "type=bind,source=vol,target=/y",
		},
		{
			name: "keys in any case, as nerdctl lowercases them",
			arg:  `Type=bind,Source=C:\x,target=/y`,
			want: "Type=bind,Source=/mnt/c/x,target=/y",
		},
		{
			name: "default volume type with a host path",
			arg:  `source=C:\x,target=/y`,
			want: "source=/mnt/c/x,target=/y",
		},
		{
			name: "volume type with a host path",
			arg:  `type=volume,source=C:\x,target=/y`,
			want: "type=volume,source=/mnt/c/x,target=/y",
		},
		{
			name: "volume type does not override bind",
			arg:  `type=bind,type=volume,source=C:\x,target=/y`,
			want: "type=bind,type=volume,source=/mnt/c/x,target=/y",
		},
		{
			name: "tmpfs ignores the source",
			arg:  `type=tmpfs,source=C:\x,target=/y`,
			want: `type=tmpfs,source=C:\x,target=/y`,
		},
		{
			name: "last of bind and tmpfs wins",
			arg:  `type=tmpfs,type=bind,source=C:\x,target=/y`,
			want: "type=tmpfs,type=bind,source=/mnt/c/x,target=/y",
		},
		{
			name: "empty source stays unchanged",
			arg:  `type=bind,source=,target=/y`,
			want: "type=bind,source=,target=/y",
		},
		{
			name: "volume name after leading colons",
			arg:  `source=:vol,target=/y`,
			want: "source=:vol,target=/y",
		},
		{
			name: "volume name before a colon",
			arg:  `source=vol:/z`,
			want: "source=vol:/z",
		},
		{
			name: "host path before a colon",
			arg:  `source=C:\x:/z`,
			want: "source=/mnt/c/x:/z",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mountArgHandler(tc.arg)
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestStdinArgPassesThrough(t *testing.T) {
	got, err := TranslateCommandLine([]string{"compose", "-f", "-", "up"})
	assert.NilError(t, err)
	assert.DeepEqual(t, got, []string{"compose", "-f", "-", "up"})

	// Elsewhere `-` names a file or directory, such as `--cidfile -`.
	want, err := guestexec.TranslateHostPath("-")
	assert.NilError(t, err)
	got, err = TranslateCommandLine([]string{"run", "--cidfile", "-", "alpine"})
	assert.NilError(t, err)
	assert.DeepEqual(t, got, []string{"run", "--cidfile", want, "--", "alpine"})
}

func TestTranslateCommandLinePathForms(t *testing.T) {
	for _, tc := range []struct {
		args, want []string
	}{
		{
			[]string{"pull", "--cosign-key", "awskms:///alias/key", "alpine"},
			[]string{"pull", "--cosign-key", "awskms:///alias/key", "--", "alpine"},
		},
		{
			[]string{"run", "--cosign-key", "k8s://ns/secret", "alpine"},
			[]string{"run", "--cosign-key", "k8s://ns/secret", "--", "alpine"},
		},
		{
			[]string{"push", "--cosign-key", `C:\cosign.key`, "example.com/img"},
			[]string{"push", "--cosign-key", "/mnt/c/cosign.key", "--", "example.com/img"},
		},
		{
			[]string{"pull", "--cosign-key", "pkcs11:token=tok;object=key", "alpine"},
			[]string{"pull", "--cosign-key", "pkcs11:token=tok;object=key", "--", "alpine"},
		},
		{
			[]string{"run", "--env-file", `C:\a.env,D:\b.env`, "alpine"},
			[]string{"run", "--env-file", "/mnt/c/a.env,/mnt/d/b.env", "--", "alpine"},
		},
		{
			[]string{"run", "--env-file", `C:\work\base.env,C:\work\env\..\override.env`, "alpine"},
			[]string{"run", "--env-file", "/mnt/c/work/base.env,/mnt/c/work/override.env", "--", "alpine"},
		},
		{
			[]string{"run", "--env-file", `"C:\a,b.env",C:\c.env`, "alpine"},
			[]string{"run", "--env-file", `"/mnt/c/a,b.env",/mnt/c/c.env`, "--", "alpine"},
		},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			got, err := TranslateCommandLine(tc.args)
			assert.NilError(t, err)
			assert.DeepEqual(t, got, tc.want)
		})
	}
}

func TestFilePathListArgHandlerRejects(t *testing.T) {
	// csv.Writer cannot write these lists so that pflag reads them back
	// unchanged.
	for _, arg := range []string{
		`""`,
		"\"C:\\a\r\r\nb.env\"",
		"C:\\work\\real.env,\"C:\\work\\x\r\r\ny.env\"",
	} {
		_, err := filePathListArgHandler(arg)
		assert.ErrorContains(t, err, "invalid path list", "%q", arg)
	}
}

func TestEmptyPathValuePassesThrough(t *testing.T) {
	// nerdctl takes an empty --cidfile as unset. A rewritten empty value would
	// name the working directory.
	for _, tc := range []struct {
		args, want []string
	}{
		{
			[]string{"run", "--cidfile", "", "alpine"},
			[]string{"run", "--cidfile", "", "--", "alpine"},
		},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			got, err := TranslateCommandLine(tc.args)
			assert.NilError(t, err)
			assert.DeepEqual(t, got, tc.want)
		})
	}
}
