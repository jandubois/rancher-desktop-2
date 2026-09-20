// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestdeps"
)

func TestBuildSelectorPicksTheImageFormatOfTheBackend(t *testing.T) {
	manifest, err := guestdeps.LoadManifest(filepath.Join("..", "..", "dependencies.yaml"))
	assert.NilError(t, err)

	for goos, want := range map[string]string{
		"windows": "distro.tar.xz", // WSL2 imports a rootfs tarball
		"darwin":  "distro.raw.xz", // Lima's vz and qemu drivers boot a raw image
		"linux":   "distro.raw.xz",
	} {
		t.Run(goos, func(t *testing.T) {
			selector := buildSelector(goos, "amd64")
			assert.Equal(t, selector.Platform, "linux", "guest assets are Linux ones on every host")
			assert.Equal(t, selector.Arch, "amd64")

			dep, err := manifest.Select("distro", selector)
			assert.NilError(t, err)
			assert.Equal(t, dep.Asset.Filename, want)
		})
	}
}

// Every target the build supports must resolve every dependency against the
// manifest as checked in, so a rename or a dropped asset fails here rather than
// in a build. One selector has to suit them all, whether or not a dependency
// ships variants.
func TestEveryDependencyResolvesAgainstTheManifest(t *testing.T) {
	manifest, err := guestdeps.LoadManifest(filepath.Join("..", "..", "dependencies.yaml"))
	assert.NilError(t, err)
	assert.Assert(t, len(manifest) > 0, "the manifest as checked in has dependencies to resolve")

	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, goarch := range []string{"amd64", "arm64"} {
			t.Run(goos+"/"+goarch, func(t *testing.T) {
				for name := range manifest {
					dep, err := manifest.Select(name, buildSelector(goos, goarch))
					assert.NilError(t, err)
					assert.Assert(t, dep.Version != "")
				}
			})
		}
	}
}

// seedRun writes a manifest naming one arm64 asset per dependency and seeds the
// cache with the bytes of each, which keeps a run off the network. What these
// tests exercise is the wiring from manifest to staged file, so every asset
// holds the same bytes.
func seedRun(t *testing.T, body []byte) (manifestPath, cacheDir, destDir string) {
	t.Helper()
	sum := sha256.Sum256(body)
	dir := t.TempDir()

	manifestPath = filepath.Join(dir, "dependencies.yaml")
	manifest := fmt.Sprintf(`
distro:
  version: 0.2.7
  assets:
    - platform: linux
      arch: arm64
      variant: raw
      url: https://example.test/distro.v0.2.7.arm64.raw.xz
      checksum: sha256:%[1]s
      filename: distro.raw.xz
mkcert:
  version: 1.4.4
  assets:
    - platform: linux
      arch: arm64
      url: https://example.test/mkcert-v1.4.4-linux-arm64
      checksum: sha256:%[1]s
      filename: mkcert
nerdctl:
  version: 2.3.5
  assets:
    - platform: linux
      arch: arm64
      url: https://example.test/nerdctl-full-2.3.5-linux-arm64.tar.gz
      checksum: sha256:%[1]s
      filename: nerdctl-full.tar.gz
`, hex.EncodeToString(sum[:]))
	assert.NilError(t, os.WriteFile(manifestPath, []byte(manifest), 0o644))

	cacheDir = filepath.Join(dir, "cache")
	for _, cached := range []struct{ name, version, file string }{
		{"distro", "v0.2.7", "distro.v0.2.7.arm64.raw.xz"},
		{"mkcert", "v1.4.4", "mkcert-v1.4.4-linux-arm64"},
		{"nerdctl", "v2.3.5", "nerdctl-full-2.3.5-linux-arm64.tar.gz"},
	} {
		cachePath := filepath.Join(cacheDir, cached.name, cached.version, cached.file)
		assert.NilError(t, os.MkdirAll(filepath.Dir(cachePath), 0o755))
		assert.NilError(t, os.WriteFile(cachePath, body, 0o644))
	}

	return manifestPath, cacheDir, filepath.Join(dir, "staged")
}

// run is what a build invokes. It reads the manifest, picks each asset for the
// target, and leaves them staged under the names the build expects.
func TestRunStagesEveryAssetForTheTarget(t *testing.T) {
	body := []byte("guest dependency")
	manifestPath, cacheDir, destDir := seedRun(t, body)

	var log bytes.Buffer
	assert.NilError(t, run(t.Context(), &log, manifestPath, destDir, cacheDir, "linux", "arm64", nil))

	for _, filename := range []string{"distro.raw.xz", "mkcert", "nerdctl-full.tar.gz"} {
		contents, err := os.ReadFile(filepath.Join(destDir, filename))
		assert.NilError(t, err)
		assert.Equal(t, string(contents), string(body))
	}
	assert.Assert(t, strings.Contains(log.String(), "distro 0.2.7 is already downloaded to"),
		"the stager reports through run's log; it holds %q", log.String())
	assert.Assert(t, strings.Contains(log.String(), filepath.Join(destDir, "distro.raw.xz")),
		"the log names the staged file; it holds %q", log.String())
}

// run sweeps the cache once everything is staged, so a sweep that cannot read
// it warns instead of failing the build.
func TestRunWarnsWhenTheCacheCannotBePruned(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not restrict listing on Windows")
	}
	body := []byte("distro image")
	manifestPath, cacheDir, destDir := seedRun(t, body)

	// Write and search stay open, so staging still reaches the entry by name;
	// only the listing Prune needs is refused.
	assert.NilError(t, os.Chmod(cacheDir, 0o300))
	t.Cleanup(func() { _ = os.Chmod(cacheDir, 0o755) })
	if _, err := os.ReadDir(cacheDir); err == nil {
		t.Skip("this user can list a directory with no read permission")
	}

	var log bytes.Buffer
	assert.NilError(t, run(t.Context(), &log, manifestPath, destDir, cacheDir, "linux", "arm64", nil))

	staged, err := os.ReadFile(filepath.Join(destDir, "distro.raw.xz"))
	assert.NilError(t, err)
	assert.Equal(t, string(staged), string(body))
	assert.Assert(t, strings.Contains(log.String(), "warning: pruning the download cache"),
		"the log records the failed sweep; it holds %q", log.String())
}

// A manifest naming an asset the target has no entry for fails with the
// manifest's own path, so the build says which file to fix.
func TestRunReportsAManifestMissingTheTarget(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "dependencies.yaml")
	assert.NilError(t, os.WriteFile(manifestPath, []byte(`
distro:
  version: 0.2.7
  assets:
    - platform: linux
      arch: amd64
      variant: raw
      url: https://example.test/distro.v0.2.7.amd64.raw.xz
      checksum: sha256:ac6c23589bc4a92a4c7d823d59b029576fa9bb18bc2c081e95f59fb184547795
      filename: distro.raw.xz
`), 0o644))

	var log bytes.Buffer
	err := run(t.Context(), &log, manifestPath, filepath.Join(dir, "embedded"), filepath.Join(dir, "cache"), "linux", "arm64", nil)
	assert.ErrorContains(t, err, manifestPath)
	assert.ErrorContains(t, err, "found 0")
}

// A build that produced its own artifact stages it in place of the download.
// The manifest's checksum belongs to the released asset, so it cannot apply to
// these bytes, and the dependencies nobody overrode still come from the cache.
func TestRunStagesALocalFileInPlaceOfTheDownload(t *testing.T) {
	body := []byte("guest dependency")
	manifestPath, cacheDir, destDir := seedRun(t, body)

	built := filepath.Join(t.TempDir(), "distro.raw.xz")
	local := []byte("an image this build made, matching no checksum")
	assert.NilError(t, os.WriteFile(built, local, 0o644))

	var log bytes.Buffer
	assert.NilError(t, run(t.Context(), &log, manifestPath, destDir, cacheDir, "linux", "arm64",
		map[string]string{"distro": built}))

	staged, err := os.ReadFile(filepath.Join(destDir, "distro.raw.xz"))
	assert.NilError(t, err)
	assert.Equal(t, string(staged), string(local))

	for _, filename := range []string{"mkcert", "nerdctl-full.tar.gz"} {
		contents, err := os.ReadFile(filepath.Join(destDir, filename))
		assert.NilError(t, err)
		assert.Equal(t, string(contents), string(body), "%s still comes from the cache", filename)
	}
	assert.Assert(t, strings.Contains(log.String(), "Staging distro from "+built+", unverified"),
		"the log says the bytes went unchecked; it holds %q", log.String())
}

// A --stage name the manifest does not list fails the run. Ignoring it would
// download the released asset instead and leave a green build that never saw
// the file the caller meant to test.
func TestRunRejectsAStageNameTheManifestDoesNotList(t *testing.T) {
	manifestPath, cacheDir, destDir := seedRun(t, []byte("guest dependency"))

	var log bytes.Buffer
	err := run(t.Context(), &log, manifestPath, destDir, cacheDir, "linux", "arm64",
		map[string]string{"distros": filepath.Join(t.TempDir(), "typo")})
	assert.ErrorContains(t, err, "distros")
	assert.ErrorContains(t, err, manifestPath)
}

// The flag takes NAME=PATH and refuses a repeat, so a second value for one
// dependency cannot silently win.
func TestStagedFilesRejectsMalformedAndRepeatedValues(t *testing.T) {
	staged := stagedFiles{}
	assert.NilError(t, staged.Set("distro=/tmp/distro.raw.xz"))
	assert.Equal(t, staged.String(), "distro=/tmp/distro.raw.xz")

	for _, value := range []string{"distro", "=/tmp/image", "distro=", ""} {
		assert.ErrorContains(t, staged.Set(value), "want NAME=PATH")
	}
	assert.ErrorContains(t, staged.Set("distro=/tmp/other"), "already staged")
}
