// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package main

import "C"

func main() {
	// DllMain is not used
}

// DetectWSL is a wrapper around DetectWSLImpl; this is the stub to be exported
// in the DLL.  This only exists to limit cgo to this file so that editing on a
// machine that requires cross compilation can avoid needing a cross cgo
// toolchain.
//
//export DetectWSL
func DetectWSL(hInstall C.ulong) C.ulong {
	return C.ulong(DetectWSLImpl(MSIHANDLE(hInstall)))
}

// UpdateWSL is a wrapper around UpdateWSLImpl; this is the stub to be exported
// in the DLL.  This only exists to limit cgo to this file so that editing on a
// machine that requires cross compilation can avoid needing a cross cgo
// toolchain.
//
//export UpdateWSL
func UpdateWSL(hInstall C.ulong) C.ulong {
	return C.ulong(UpdateWSLImpl(MSIHANDLE(hInstall)))
}
