// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package nerdctlstub

// argHandlerKind classifies how a flag value containing host paths is
// rewritten before the arguments reach nerdctl in the guest.
type argHandlerKind int

const (
	// filePathArg is a plain path to an input file or directory.
	filePathArg argHandlerKind = iota
	// outputPathArg is a plain path nerdctl writes to (e.g. --cidfile).
	outputPathArg
	// volumeArg is host-path:container-path[:mode], or a bare container path
	// for an anonymous volume.
	volumeArg
	// mountArg is CSV such as type=bind,source=host-path,target=....
	mountArg
	// builderCacheArg is CSV with src= or source= (input) and dest= (output)
	// paths.
	builderCacheArg
	// buildContextArg is name=value, where value is a path, an
	// oci-layout:// path, a URL, or an image or target reference.
	buildContextArg
	// fileOrURLOrStdinArg is a path to an input file; `-` and URIs pass
	// through.
	fileOrURLOrStdinArg
	// filePathListArg is a comma-separated list of input paths, which
	// nerdctl reads as a pflag StringSlice.
	filePathListArg
	// builderOutputArg is builderCacheArg CSV, which nerdctl prefixes with
	// `type=local,dest=` when it has no `type=`.
	builderOutputArg
	// sshArg is id=path,path,..., where each path is an SSH agent socket or
	// a key file.
	sshArg
)

// pathFlags classifies the nerdctl flags whose host paths the stub translates.
// Keys are canonical command paths; aliases resolve before lookup. The
// ./generate tool validates each entry against the pinned nerdctl version
// and fails when a value-consuming flag smells like a path but appears
// neither here nor in notPathFlags, so generation after a nerdctl bump
// names each new flag that looks like a path.
var pathFlags = map[string]map[string]argHandlerKind{
	"builder build": {
		"--build-context": buildContextArg,
		"--cache-from":    builderCacheArg,
		"--cache-to":      builderCacheArg,
		"--file":          fileOrURLOrStdinArg,
		"-f":              fileOrURLOrStdinArg,
		"--iidfile":       outputPathArg,
		"--output":        builderOutputArg,
		"-o":              builderOutputArg,
		// --secret is CSV with src= or source=.
		"--secret": builderCacheArg,
		"--ssh":    sshArg,
	},
	"builder debug": {
		"--file":   filePathArg,
		"-f":       filePathArg,
		"--secret": builderCacheArg,
		"--ssh":    sshArg,
	},
	"compose": {
		"--env-file":          filePathArg,
		"--file":              fileOrURLOrStdinArg,
		"--f":                 fileOrURLOrStdinArg, // long-form spelling of the -f alias
		"-f":                  fileOrURLOrStdinArg,
		"--project-directory": filePathArg,
	},
	"compose run": {
		"--volume": volumeArg,
		"-v":       volumeArg,
	},
	"container create": {
		"--cidfile":    outputPathArg,
		"--cosign-key": fileOrURLOrStdinArg,
		"--env-file":   filePathListArg,
		"--label-file": filePathListArg,
		"--mount":      mountArg,
		"--pidfile":    outputPathArg,
		"--volume":     volumeArg,
		"-v":           volumeArg,
	},
	"container exec": {
		"--env-file": filePathListArg,
	},
	"container run": {
		"--cidfile":    outputPathArg,
		"--cosign-key": fileOrURLOrStdinArg,
		"--env-file":   filePathListArg,
		"--label-file": filePathListArg,
		"--mount":      mountArg,
		"--pidfile":    outputPathArg,
		"--volume":     volumeArg,
		"-v":           volumeArg,
	},
	"image convert": {
		"--estargz-record-in":     filePathArg,
		"--zstdchunked-record-in": filePathArg,
	},
	"image decrypt": {
		"--key": filePathListArg,
	},
	"image encrypt": {
		"--key": filePathListArg,
	},
	"image load": {
		"--input": filePathArg,
		"-i":      filePathArg,
	},
	"image pull": {
		"--cosign-key": fileOrURLOrStdinArg,
	},
	"image push": {
		"--cosign-key": fileOrURLOrStdinArg,
	},
	"image save": {
		"--output": outputPathArg,
		"-o":       outputPathArg,
	},
}

// notPathFlags records flags that the stub leaves alone on purpose. Some,
// such as `--recipient` with a key file, can hold a host path that reaches
// the guest unchanged.
var notPathFlags = map[string][]string{
	"": {
		"--cdi-spec-dirs",   // guest-side
		"--cni-netconfpath", // guest-side
		"--cni-path",        // guest-side
		"--data-root",       // guest-side
		"--hosts-dir",       // guest-side
	},
	"builder debug": {
		"--image", // image name
	},
	"compose": {
		"--ipfs-address", // multiaddr
		"--profile",      // profile name
	},
	"compose exec": {
		"--workdir", // path inside the container
		"-w",
	},
	"compose run": {
		"--workdir", // path inside the container
		"-w",
	},
	"compose up": {
		"--scale", // SERVICE=NUM
	},
	"container attach": {
		"--detach-keys",
	},
	"container commit": {
		"--change", // Dockerfile instruction
		"-c",
	},
	"container create": {
		"--blkio-weight-device", // guest device path
		"--detach-keys",
		"--device", // guest device path
		"--device-read-bps",
		"--device-read-iops",
		"--device-write-bps",
		"--device-write-iops",
		"--ipfs-address", // multiaddr
		"--log-driver",
		"--net",     // network name
		"--network", // network name
		"--tmpfs",   // path inside the container
		"--volumes-from",
		"--workdir", // path inside the container
		"-w",
	},
	"container exec": {
		"--workdir", // path inside the container
		"-w",
	},
	"container run": {
		"--blkio-weight-device", // guest device path
		"--detach-keys",
		"--device", // guest device path
		"--device-read-bps",
		"--device-read-iops",
		"--device-write-bps",
		"--device-write-iops",
		"--ipfs-address", // multiaddr
		"--log-driver",
		"--net",     // network name
		"--network", // network name
		"--tmpfs",   // path inside the container
		"--volumes-from",
		"--workdir", // path inside the container
		"-w",
	},
	"container start": {
		"--detach-keys",
	},
	"image convert": {
		"--nydus-builder-path", // guest-side
		"--nydus-prefetch-patterns",
		"--nydus-work-dir", // guest-side
		"--overlaybd-fs-type",
	},
	"image decrypt": {
		"--gpg-homedir", // guest-side
	},
	"image encrypt": {
		"--gpg-homedir", // guest-side
		// --recipient takes prefix:value where value may be a key file
		// (jwe:/path/to/key.pem); such paths stay untranslated, so use a
		// guest path (/mnt/c/...) explicitly if needed.
		"--recipient",
	},
	"image pull": {
		"--ipfs-address", // multiaddr
	},
	"image push": {
		"--ipfs-address",      // multiaddr
		"--notation-key-name", // key name, not a path
	},
	"ipfs registry serve": {
		"--ipfs-address", // multiaddr
	},
}
