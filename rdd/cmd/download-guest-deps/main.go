// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Command download-guest-deps stages the guest dependencies the rdd build bakes
// into its binaries.
//
// The target defaults to the host. A build that cross-compiles has to pass
// --os and --arch; CI does cross-compile, packaging the macOS x86_64 build on
// an arm64 runner with GOARCH set for `make build-rdd`. They are flags rather
// than the GOOS and GOARCH environment variables, which would cross-compile
// this tool too and leave nothing that can run.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestdeps"
)

// stagedFiles collects --stage values: a dependency name mapped to the local
// file that replaces its download.
type stagedFiles map[string]string

// String renders the pairs for flag's usage and error output.
func (s stagedFiles) String() string {
	pairs := make([]string, 0, len(s))
	for _, name := range slices.Sorted(maps.Keys(s)) {
		pairs = append(pairs, name+"="+s[name])
	}
	return strings.Join(pairs, ",")
}

// Set records one NAME=PATH pair, refusing a repeated name so the second does
// not silently win.
func (s stagedFiles) Set(value string) error {
	name, path, found := strings.Cut(value, "=")
	if !found || name == "" || path == "" {
		return fmt.Errorf("want NAME=PATH, got %q", value)
	}
	if existing, taken := s[name]; taken {
		return fmt.Errorf("%s is already staged from %s", name, existing)
	}
	s[name] = path
	return nil
}

func main() {
	manifest := flag.String("manifest", "dependencies.yaml", "guest dependency manifest (YAML)")
	dest := flag.String("dest", filepath.Join("overlay", "build"), "directory to stage the assets into")
	cache := flag.String("cache", "", "directory the verified downloads are kept and pruned in (default: under the user cache directory)")
	goos := flag.String("os", runtime.GOOS, "the operating system the build targets")
	goarch := flag.String("arch", runtime.GOARCH, "the architecture the build targets")
	staged := stagedFiles{}
	flag.Var(staged, "stage", "stage NAME from a local file instead of downloading it, as NAME=PATH; repeatable")
	flag.Parse()

	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: download-guest-deps [--manifest M] [--dest D] [--cache C] [--os OS] [--arch ARCH] [--stage NAME=PATH]")
		os.Exit(2)
	}
	// A download runs for minutes; let an interrupt abort it and clean up.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Stderr, *manifest, *dest, *cache, *goos, *goarch, staged)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "download-guest-deps:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log io.Writer, manifestPath, destDir, cacheDir, goos, goarch string, staged map[string]string) error {
	manifest, err := guestdeps.LoadManifest(manifestPath)
	if err != nil {
		return err
	}
	// A misspelled --stage name would otherwise download the released asset and
	// leave a green build that never saw the file the caller meant to test.
	for _, name := range slices.Sorted(maps.Keys(staged)) {
		if _, listed := manifest[name]; !listed {
			return fmt.Errorf("%s: --stage names %q, which the manifest does not list", manifestPath, name)
		}
	}
	if cacheDir == "" {
		if cacheDir, err = guestdeps.DefaultCacheDir(); err != nil {
			return err
		}
	}
	selector := buildSelector(goos, goarch)
	stager := &guestdeps.Stager{CacheDir: cacheDir, Log: log}
	// Sorted, so the log reads the same from one build to the next.
	for _, name := range slices.Sorted(maps.Keys(manifest)) {
		dep, err := manifest.Select(name, selector)
		if err != nil {
			return fmt.Errorf("%s: %w", manifestPath, err)
		}
		destPath := filepath.Join(destDir, dep.Asset.Filename)
		if src, ok := staged[name]; ok {
			// Nothing verifies these bytes: the manifest's checksum belongs to
			// the released asset, not to whatever the build produced. Say so in
			// the log, so a run that staged a local file does not read like a
			// verified one.
			fmt.Fprintf(log, "Staging %s from %s, unverified\n", name, src)
			if err := guestdeps.StageFile(src, destPath); err != nil {
				return fmt.Errorf("staging %s from %s: %w", name, src, err)
			}
			continue
		}
		if err := stager.Stage(ctx, dep, destPath); err != nil {
			return err
		}
	}
	// Everything the build needs is staged by now, so a cache that cannot be
	// pruned is a nuisance rather than a failure.
	if err := stager.Prune(); err != nil {
		fmt.Fprintln(log, "warning: pruning the download cache:", err)
	}
	return nil
}

// buildSelector picks the asset a dependency ships for the build target. Guest
// dependencies run in the Linux VM, so every asset is a linux one whatever the
// host; only the image variant follows the host, because WSL2 imports the
// rootfs tarball while Lima's vz and qemu drivers boot the raw ext4 image. A
// dependency that ships one artifact per architecture records no variant and
// matches either way.
func buildSelector(goos, goarch string) guestdeps.Selector {
	variant := "raw"
	if goos == "windows" {
		variant = "tar"
	}
	return guestdeps.Selector{Platform: "linux", Arch: goarch, Variant: variant}
}
