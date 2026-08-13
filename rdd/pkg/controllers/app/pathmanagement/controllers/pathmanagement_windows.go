// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

//go:build windows

package controllers

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
)

const (
	// hwndBroadcast sends the message to all top-level windows.
	hwndBroadcast = windows.HWND(0xffff)
	// wmSettingChange notifies applications that a system-wide setting changed.
	wmSettingChange = 0x001A
	// smtoAbortIfHung skips windows that have stopped responding instead of
	// blocking on them.
	smtoAbortIfHung = 0x0002
)

// Loaded once and shared across broadcasts. LazyDLL/LazyProc resolve on first
// use, so declaring them here costs nothing if we never broadcast.
var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	sendMessageTimeout = user32.NewProc("SendMessageTimeoutA")
)

// envSubKey is the HKCU subkey that holds the user Path. It's a var so tests can
// point at a throwaway key instead of the real one.
var envSubKey = `Environment`

// applyPath edits the user's Path in HKCU\Environment and, if it changed,
// broadcasts WM_SETTINGCHANGE so new processes (and refresh-aware apps) pick it
// up. forceRemove drops our entry regardless of position (used when unwinding
// for deletion); see computeWindowsPath.
//
// The caller (apply) holds a cross-process lock, so the read-modify-write of the
// user Path can't lose a concurrent instance's entry.
func (r *PathManagementReconciler) applyPath(_ context.Context, strategy appv1alpha1.AddPathStrategy, forceRemove bool) error {
	changed, err := r.writeUserPath(strategy, forceRemove)
	if err != nil {
		return err
	}
	if changed {
		broadcastEnvironmentChange()
	}
	return nil
}

// writeUserPath applies strategy to the user Path in the registry and reports
// whether it changed. It's separate from apply, and skips the broadcast, so a
// test can run it against a throwaway key.
func (r *PathManagementReconciler) writeUserPath(strategy appv1alpha1.AddPathStrategy, forceRemove bool) (bool, error) {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, envSubKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, fmt.Errorf("open HKCU\\%s: %w", envSubKey, err)
	}
	defer key.Close()

	current, valType, err := key.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return false, fmt.Errorf("read user Path: %w", err)
	}

	updated, changed := computeWindowsPath(current, r.BinDir, strategy, forceRemove)
	if !changed {
		return false, nil
	}

	// Keep the existing value type. The user Path is usually REG_EXPAND_SZ (it can
	// contain things like %USERPROFILE%), so use that when it did not exist yet.
	if valType == registry.SZ {
		err = key.SetStringValue("Path", updated)
	} else {
		err = key.SetExpandStringValue("Path", updated)
	}
	if err != nil {
		return false, fmt.Errorf("write user Path: %w", err)
	}
	return true, nil
}

// broadcastEnvironmentChange tells running applications the environment changed.
// Errors are ignored: the Path is already written and new processes pick it up.
func broadcastEnvironmentChange() {
	env, err := windows.BytePtrFromString("Environment")
	if err != nil {
		return
	}
	var result uintptr
	_, _, _ = sendMessageTimeout.Call(
		uintptr(hwndBroadcast),
		wmSettingChange,
		0,
		uintptr(unsafe.Pointer(env)),
		smtoAbortIfHung,
		5000,
		uintptr(unsafe.Pointer(&result)),
	)
}
