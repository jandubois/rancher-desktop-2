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
	// fileOrURLOrStdinArg is a path to an input file; `-` and URIs pass
	// through.
	fileOrURLOrStdinArg
	// filePathListArg is a comma-separated list of input paths, which
	// nerdctl reads as a pflag StringSlice.
	filePathListArg
)

// pathFlags classifies the nerdctl flags whose host paths the stub translates.
// Keys are canonical command paths; aliases resolve before lookup.
var pathFlags = map[string]map[string]argHandlerKind{
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
