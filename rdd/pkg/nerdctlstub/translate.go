// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package nerdctlstub

// This file rewrites the host paths in nerdctl arguments to the /mnt/<drive>
// locations where the WSL2 guest automounts them. cmd/rdd calls it only on
// Windows.

import (
	"encoding/csv"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestexec"
)

// namedVolume matches the volume sources that nerdctl takes for volume names
// instead of host paths; it copies isNamedVolume in nerdctl's pkg/mountutil.
var namedVolume = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]+$`)

// windowsDrive matches a leading Windows drive, as in `C:\data` or the
// drive-relative `d:app`. A one-letter source cannot be a volume name.
var windowsDrive = regexp.MustCompile(`^[a-zA-Z]:`)

// cutVolumeSource drops the leading colons of a volume spec and cuts the
// source at the next colon, as nerdctl's splitVolumeSpec does, except that a
// leading drive's colon stays in the source. found reports whether a colon
// ended the source, and rest is what follows it.
func cutVolumeSource(spec string) (source, rest string, found bool) {
	spec = strings.TrimLeft(spec, ":")
	drive := ""
	if windowsDrive.MatchString(spec) {
		drive, spec = spec[:2], spec[2:]
	}
	source, rest, found = strings.Cut(spec, ":")
	return drive + source, rest, found
}

// volumeArgHandler handles the argument of `nerdctl run --volume=...`.
func volumeArgHandler(arg string) (string, error) {
	// Valid arguments are:
	// <container path>, an anonymous volume
	// <host path or volume name>:<container path>
	// <host path or volume name>:<container path>:<mode>
	// The mode passes through verbatim for nerdctl to interpret.
	hostPath, rest, found := cutVolumeSource(arg)
	if !found {
		if windowsDrive.MatchString(hostPath) {
			// nerdctl splits at the drive's colon, so `C:/data` would create a
			// directory named `C` and mount it at /data.
			return "", fmt.Errorf("invalid volume mount %s: expected source:container-path", arg)
		}
		// An anonymous volume.
		return arg, nil
	}
	if namedVolume.MatchString(hostPath) {
		return arg, nil
	}
	guestPath, err := guestexec.TranslateHostPath(hostPath)
	if err != nil {
		return "", fmt.Errorf("could not get volume host path for %s: %w", arg, err)
	}
	return guestPath + ":" + rest, nil
}

// mountArgHandler handles the argument of `nerdctl run --mount=...`.
func mountArgHandler(arg string) (string, error) {
	// Like nerdctl's ProcessFlagMount, match keys in any case and values
	// exactly. The mount type defaults to volume, and the last `type=bind`
	// or `type=tmpfs` sets it; `type=volume` leaves it as it was. Volume and
	// bind mounts read a nonempty source that is not a volume name as a host
	// path, and tmpfs mounts ignore the source.
	parts := strings.Split(arg, ",")
	isTmpfs := false
	for _, part := range parts {
		key, value, _ := strings.Cut(part, "=")
		if !strings.EqualFold(key, "type") {
			continue
		}
		switch value {
		case "bind":
			isTmpfs = false
		case "tmpfs":
			isTmpfs = true
		}
	}
	if isTmpfs {
		return arg, nil
	}
	for i, part := range parts {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		if k := strings.ToLower(key); k != "source" && k != "src" {
			continue
		}
		// nerdctl joins the source to the target with a colon and splits the
		// result like a `--volume` spec.
		source, rest, found := cutVolumeSource(value)
		if namedVolume.MatchString(source) {
			continue
		}
		guestPath, err := filePathArgHandler(source)
		if err != nil {
			return "", err
		}
		if found {
			guestPath += ":" + rest
		}
		parts[i] = key + "=" + guestPath
	}
	return strings.Join(parts, ","), nil
}

// filePathArgHandler handles arguments that take a path for input.
func filePathArgHandler(arg string) (string, error) {
	if arg == "" {
		// An empty value does not name a path. nerdctl takes most empty
		// flags as unset and resolves other empty values in its own working
		// directory, the guest's view of the host one.
		return arg, nil
	}
	return guestexec.TranslateHostPath(arg)
}

// filePathListArgHandler handles a comma-separated list of input paths. It
// splits and joins the list as one CSV record, the way pflag's StringSlice
// reads and writes it.
func filePathListArgHandler(arg string) (string, error) {
	if arg == "" {
		return arg, nil
	}
	paths, err := csv.NewReader(strings.NewReader(arg)).Read()
	if err != nil {
		// nerdctl rejects the value too, and reports it.
		return arg, nil
	}
	for i, p := range paths {
		if paths[i], err = filePathArgHandler(p); err != nil {
			return "", err
		}
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	if err := w.Write(paths); err != nil {
		return "", err
	}
	w.Flush()
	result := strings.TrimSuffix(b.String(), "\n")
	if reread, err := csv.NewReader(strings.NewReader(result)).Read(); err != nil || !slices.Equal(reread, paths) {
		// csv.Writer writes a lone empty entry as an empty value, which pflag
		// reads as no entries. csv.Reader reads CR LF inside an entry back as
		// LF. Either way nerdctl would read a different list, so report the
		// value here.
		return "", fmt.Errorf("invalid path list %q: it is a single empty entry, or an entry contains CR LF", arg)
	}
	return result, nil
}

// outputPathArgHandler handles arguments that take a path to write to. On
// Windows the guest writes through the /mnt/<drive> mount directly, so this
// matches the input handling.
func outputPathArgHandler(arg string) (string, error) {
	return filePathArgHandler(arg)
}

// urlLikeRE matches a value that starts with a URI scheme, such as `https:`
// or `pkcs11:`. A scheme has at least two characters, so a drive path such as
// `C://dir` does not match.
var urlLikeRE = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]+:`)

// fileOrURLOrStdinArgHandler rewrites a file path but passes `-` and URIs
// through, for arguments such as `compose --file`, where `-` is stdin.
func fileOrURLOrStdinArgHandler(arg string) (string, error) {
	if arg == "-" || urlLikeRE.MatchString(arg) {
		return arg, nil
	}
	return filePathArgHandler(arg)
}
