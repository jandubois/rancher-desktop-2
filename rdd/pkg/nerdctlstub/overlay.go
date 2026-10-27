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
// Keys are canonical command paths; aliases resolve before lookup.
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
