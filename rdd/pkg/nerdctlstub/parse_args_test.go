// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package nerdctlstub

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestdeps"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestexec"
)

// TestTranslateCommandLine exercises the full parser against the generated
// nerdctl command table, with path translation as it happens on Windows.
func TestTranslateCommandLine(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "volume flag on aliased run command",
			args: []string{"run", "--rm", "-v", `C:\data:/data`, "alpine", "ls", "--color"},
			want: []string{"run", "--rm", "-v", "/mnt/c/data:/data", "--", "alpine", "ls", "--color"},
		},
		{
			name: "foreign flags after the image are not parsed",
			args: []string{"run", "alpine", "--env", "x=y"},
			want: []string{"run", "--", "alpine", "--env", "x=y"},
		},
		{
			name: "ps options after the container are not parsed",
			args: []string{"top", "ctr", "-ef"},
			want: []string{"top", "--", "ctr", "-ef"},
		},
		{
			name: "bunched short flags stay bunched",
			args: []string{"run", "-it", "alpine"},
			want: []string{"run", "-it", "--", "alpine"},
		},
		{
			name: "mount flag",
			args: []string{"container", "run", "--mount", `type=bind,source=C:\x,target=/y`, "img"},
			want: []string{"container", "run", "--mount", "type=bind,source=/mnt/c/x,target=/y", "--", "img"},
		},
		{
			name: "global flag with equals sign",
			args: []string{"-n=test", "run", "-v", `C:\x:/x`, "alpine"},
			want: []string{"-n=test", "run", "-v", "/mnt/c/x:/x", "--", "alpine"},
		},
		{
			name: "attached value that starts with a dash",
			args: []string{"--hosts-dir=--certs", "run", "-v", `C:\x:/x`, "alpine"},
			want: []string{"--hosts-dir=--certs", "run", "-v", "/mnt/c/x:/x", "--", "alpine"},
		},
		{
			name: "long option with an attached path value",
			args: []string{"run", `--volume=C:\x:/x`, "alpine"},
			want: []string{"run", "--volume", "/mnt/c/x:/x", "--", "alpine"},
		},
		{
			name: "long option with an attached value containing an equals sign",
			args: []string{"run", `--mount=type=bind,source=C:\x,target=/y`, "alpine"},
			want: []string{"run", "--mount", "type=bind,source=/mnt/c/x,target=/y", "--", "alpine"},
		},
		{
			name: "global flag before subcommand",
			args: []string{"--namespace", "test", "ps", "-a"},
			want: []string{"--namespace", "test", "ps", "-a"},
		},
		{
			name: "compose file flag",
			args: []string{"compose", "-f", `C:\app\compose.yaml`, "up", "-d"},
			want: []string{"compose", "-f", "/mnt/c/app/compose.yaml", "up", "-d"},
		},
		{
			name: "unknown subcommand passes through",
			args: []string{"internal", "oci-hook", "createRuntime"},
			want: []string{"internal", "oci-hook", "createRuntime"},
		},
		{
			name: "double dash stops parsing",
			args: []string{"image", "ls", "--", "-v"},
			want: []string{"image", "ls", "--", "-v"},
		},
		{
			name: "output path on image save",
			args: []string{"save", "-o", `C:\img.tar`, "alpine"},
			want: []string{"save", "-o", "/mnt/c/img.tar", "--", "alpine"},
		},
		{
			name: "double dash as an option value",
			args: []string{"image", "ls", "--format", "--", "--", "-q"},
			want: []string{"image", "ls", "--format", "--", "--", "-q"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := TranslateCommandLine(tc.args)
			assert.NilError(t, err)
			assert.DeepEqual(t, got, tc.want)
		})
	}
}

// TestTranslateCommandLineHelp checks that cobra's help and version flags
// pass through to the guest nerdctl.
func TestTranslateCommandLineHelp(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"--version"},
		{"ps", "--help"},
		{"ps", "-h"},
		{"compose", "up", "--help"},
		{"cp", "--help"},
	} {
		got, err := TranslateCommandLine(args)
		assert.NilError(t, err, "%q", args)
		assert.DeepEqual(t, got, args)
	}

	// run defines -h as --hostname, which takes a value.
	got, err := TranslateCommandLine([]string{"run", "-h", "myhost", "alpine"})
	assert.NilError(t, err)
	assert.DeepEqual(t, got, []string{"run", "-h", "myhost", "--", "alpine"})
}

func TestTranslateCommandLineLeavesErrorsToNerdctl(t *testing.T) {
	// Whether an unknown option takes a value is unknown, so nothing after
	// it is rewritten.
	for _, tc := range []struct {
		args, want []string
	}{
		{
			[]string{"run", "--no-such-flag", `C:\x`, "-v", `C:\data:/data`, "alpine"},
			[]string{"run", "--no-such-flag", `C:\x`, "-v", `C:\data:/data`, "alpine"},
		},
		{
			[]string{"run", "-v", `C:\data:/data`, "--no-such-flag", "alpine"},
			[]string{"run", "-v", "/mnt/c/data:/data", "--no-such-flag", "alpine"},
		},
		{
			[]string{"volume", "rm", "vol1", "--no-such-flag"},
			[]string{"volume", "rm", "vol1", "--no-such-flag"},
		},
		{
			// pflag never reads a single-dash word as a long option, so
			// nerdctl rejects `-cidfile` at its first letter.
			[]string{"run", "-cidfile", `C:\cid`, "-v", `C:\data:/data`, "alpine"},
			[]string{"run", "-cidfile", `C:\cid`, "-v", `C:\data:/data`, "alpine"},
		},
		{
			// No level has `-d`, so nerdctl rejects the bunch; the root's
			// `-a` must not take the rest as its value.
			[]string{"images", "-ad"},
			[]string{"images", "-ad"},
		},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			got, err := TranslateCommandLine(tc.args)
			assert.NilError(t, err)
			assert.DeepEqual(t, got, tc.want)
		})
	}

	// nerdctl reports a missing option value itself.
	for _, tc := range []struct {
		args, want []string
	}{
		{[]string{"run", "-v"}, []string{"run", "-v"}},
		{[]string{"ps", "--format"}, []string{"ps", "--format"}},
		{[]string{"compose", "up", "-f"}, []string{"compose", "up", "-f"}},
		{[]string{"compose", "up", "--file"}, []string{"compose", "up", "--file"}},
	} {
		got, err := TranslateCommandLine(tc.args)
		assert.NilError(t, err, "%q", tc.args)
		assert.DeepEqual(t, got, tc.want)
	}
}

func TestTranslateCommandLineParentHandlerError(t *testing.T) {
	// compose up falls back to the compose -f handler, whose error must
	// fail the command instead of being taken for an unknown option.
	options := commands["compose"].options
	saved := options["-f"]
	options["-f"] = func(string) (string, error) {
		return "", errors.New("handler failed")
	}
	t.Cleanup(func() { options["-f"] = saved })
	_, err := TranslateCommandLine([]string{"compose", "up", "-f", "x.yaml"})
	assert.ErrorContains(t, err, "handler failed")
}

// TestTableSanity pins properties of the generated table that the parser
// relies on; a regeneration that loses them should fail here.
func TestTableSanity(t *testing.T) {
	for _, path := range []string{"container run", "container create", "container exec", "top"} {
		assert.Assert(t, commandSpecs[path].foreignFlags, "command %q must have foreign flags", path)
	}
	for _, alias := range []string{"run", "build", "cp"} {
		_, ok := commandAliases[alias]
		assert.Assert(t, ok, "alias %q must exist", alias)
	}
	// Path handlers must be wired, not left as ignoredArgHandler: translate
	// a volume argument through the live table.
	got, err := commands["container run"].options["-v"](`C:\x:/y`)
	assert.NilError(t, err)
	assert.Equal(t, got, "/mnt/c/x:/y")
}

// TestTableMatchesGuestNerdctl fails when dependencies.yaml changes the
// guest's nerdctl but the parse table still describes the old version. To
// fix it, move the nerdctl requirement in generate/go.mod to the same
// version, move the tigron pin with it, and run
// `make -C rdd generate-nerdctl`.
func TestTableMatchesGuestNerdctl(t *testing.T) {
	manifest, err := guestdeps.LoadManifest(filepath.Join("..", "..", "dependencies.yaml"))
	assert.NilError(t, err)
	assert.Equal(t, nerdctlVersion, "v"+manifest["nerdctl"].Version)
}

func TestTranslateCommandLineAttachedShortValues(t *testing.T) {
	// pflag reads `-o=` as the value `=`, a relative path.
	equalsPath, err := guestexec.TranslateHostPath("=")
	assert.NilError(t, err)
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "separate value",
			args: []string{"run", "-p", "8080:80", "alpine"},
			want: []string{"run", "-p", "8080:80", "--", "alpine"},
		},
		{
			name: "attached value",
			args: []string{"run", "-p8080:80", "alpine"},
			want: []string{"run", "-p8080:80", "--", "alpine"},
		},
		{
			name: "attached value containing an equals sign",
			args: []string{"run", "-eFOO=bar", "alpine"},
			want: []string{"run", "-eFOO=bar", "--", "alpine"},
		},
		{
			name: "attached path value containing an equals sign",
			args: []string{"run", `-vC:\a=b:/y`, "alpine"},
			want: []string{"run", "-v", "/mnt/c/a=b:/y", "--", "alpine"},
		},
		{
			name: "attached path value after an equals sign",
			args: []string{"run", `-v=C:\x:/y`, "alpine"},
			want: []string{"run", "-v", "/mnt/c/x:/y", "--", "alpine"},
		},
		{
			name: "attached value after bunched booleans",
			args: []string{"run", "-itp8080:80", "alpine"},
			want: []string{"run", "-itp8080:80", "--", "alpine"},
		},
		{
			name: "attached path value",
			args: []string{"run", `-vC:\x:/y`, "alpine"},
			want: []string{"run", "-v", "/mnt/c/x:/y", "--", "alpine"},
		},
		{
			name: "attached path value after bunched booleans",
			args: []string{"run", `-itvC:\x:/y`, "alpine"},
			want: []string{"run", "-itv", "/mnt/c/x:/y", "--", "alpine"},
		},
		{
			name: "attached equals sign as the whole value",
			args: []string{"run", "-e=", "alpine"},
			want: []string{"run", "-e=", "--", "alpine"},
		},
		{
			name: "attached equals sign as the whole path value",
			args: []string{"image", "save", "-o=", "alpine"},
			want: []string{"image", "save", "-o", equalsPath, "--", "alpine"},
		},
		{
			name: "bunched booleans with an attached value",
			args: []string{"run", "-dt=false", "alpine"},
			want: []string{"run", "-dt=false", "--", "alpine"},
		},
		{
			name: "bunch with a letter of the enclosing command",
			args: []string{"compose", "run", "-dp", "proj", "-v", `C:\x:/x`, "svc"},
			want: []string{"compose", "run", "-dp", "proj", "-v", "/mnt/c/x:/x", "--", "svc"},
		},
		{
			name: "bunch with an attached value of the enclosing command",
			args: []string{"compose", "run", "-dpproj", "-v", `C:\x:/x`, "svc"},
			want: []string{"compose", "run", "-dpproj", "-v", "/mnt/c/x:/x", "--", "svc"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := TranslateCommandLine(tc.args)
			assert.NilError(t, err)
			assert.DeepEqual(t, got, tc.want)
		})
	}
}
