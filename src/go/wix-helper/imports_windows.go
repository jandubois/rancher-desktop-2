// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package main

import "golang.org/x/sys/windows"

type MSIHANDLE uint32

var (
	dllMsi              = windows.NewLazySystemDLL("msi.dll")
	msiCloseHandle      = dllMsi.NewProc("MsiCloseHandle")
	msiCreateRecord     = dllMsi.NewProc("MsiCreateRecord")
	msiRecordSetStringW = dllMsi.NewProc("MsiRecordSetStringW")
	msiProcessMessage   = dllMsi.NewProc("MsiProcessMessage")
	msiSetPropertyW     = dllMsi.NewProc("MsiSetPropertyW")
)
