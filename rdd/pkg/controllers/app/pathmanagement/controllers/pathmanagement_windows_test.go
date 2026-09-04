// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

//go:build windows

package controllers

import (
	"fmt"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
	"gotest.tools/v3/assert"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
)

// withTestEnvKey points envSubKey at a throwaway HKCU key so tests stay away
// from the real user Path, seeding it with the given value and type. It returns
// the key path and registers cleanup.
func withTestEnvKey(t *testing.T, seed string, seedType uint32) string {
	t.Helper()
	keyPath := fmt.Sprintf(`Software\rancher-desktop-daemon-test\%d`, time.Now().UnixNano())

	key, _, err := registry.CreateKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	assert.NilError(t, err)
	if seed != "" {
		if seedType == registry.SZ {
			assert.NilError(t, key.SetStringValue("Path", seed))
		} else {
			assert.NilError(t, key.SetExpandStringValue("Path", seed))
		}
	}
	assert.NilError(t, key.Close())

	old := envSubKey
	envSubKey = keyPath
	t.Cleanup(func() {
		envSubKey = old
		_ = registry.DeleteKey(registry.CURRENT_USER, keyPath)
	})
	return keyPath
}

func readTestPath(t *testing.T, keyPath string) (value string, valType uint32) {
	t.Helper()
	key, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE)
	assert.NilError(t, err)
	defer key.Close()
	val, valType, err := key.GetStringValue("Path")
	assert.NilError(t, err)
	return val, valType
}

func TestWriteUserPathFrontPreservesExpandSZ(t *testing.T) {
	keyPath := withTestEnvKey(t, `%SystemRoot%\system32;C:\Windows`, registry.EXPAND_SZ)
	r := &PathManagementReconciler{BinDir: `C:\Users\me\.rd2\bin`}

	changed, err := r.writeUserPath(appv1alpha1.AddPathFront, false)
	assert.NilError(t, err)
	assert.Equal(t, changed, true)

	val, valType := readTestPath(t, keyPath)
	assert.Equal(t, val, `C:\Users\me\.rd2\bin;%SystemRoot%\system32;C:\Windows`)
	// REG_EXPAND_SZ has to survive the rewrite, or entries like %SystemRoot%
	// stop expanding.
	assert.Equal(t, valType, uint32(registry.EXPAND_SZ))
}

func TestWriteUserPathKeepsPlainSZ(t *testing.T) {
	keyPath := withTestEnvKey(t, `C:\Windows`, registry.SZ)
	r := &PathManagementReconciler{BinDir: `C:\bin`}

	changed, err := r.writeUserPath(appv1alpha1.AddPathBack, false)
	assert.NilError(t, err)
	assert.Equal(t, changed, true)

	val, valType := readTestPath(t, keyPath)
	assert.Equal(t, val, `C:\Windows;C:\bin`)
	assert.Equal(t, valType, uint32(registry.SZ))
}

func TestWriteUserPathManualRemoves(t *testing.T) {
	keyPath := withTestEnvKey(t, `C:\bin;C:\Windows`, registry.EXPAND_SZ)
	r := &PathManagementReconciler{BinDir: `C:\bin`}

	changed, err := r.writeUserPath(appv1alpha1.AddPathManual, false)
	assert.NilError(t, err)
	assert.Equal(t, changed, true)

	val, _ := readTestPath(t, keyPath)
	assert.Equal(t, val, `C:\Windows`)
}

func TestWriteUserPathForceRemovesMiddleEntry(t *testing.T) {
	keyPath := withTestEnvKey(t, `C:\Windows;C:\bin;C:\Windows\System32`, registry.EXPAND_SZ)
	r := &PathManagementReconciler{BinDir: `C:\bin`}

	// manual leaves a middle entry alone, but the unwind path forces it out.
	changed, err := r.writeUserPath(appv1alpha1.AddPathManual, false)
	assert.NilError(t, err)
	assert.Equal(t, changed, false)

	changed, err = r.writeUserPath(appv1alpha1.AddPathManual, true)
	assert.NilError(t, err)
	assert.Equal(t, changed, true)

	val, _ := readTestPath(t, keyPath)
	assert.Equal(t, val, `C:\Windows;C:\Windows\System32`)
}

func TestWriteUserPathIdempotent(t *testing.T) {
	withTestEnvKey(t, `C:\bin;C:\Windows`, registry.EXPAND_SZ)
	r := &PathManagementReconciler{BinDir: `C:\bin`}

	changed, err := r.writeUserPath(appv1alpha1.AddPathFront, false)
	assert.NilError(t, err)
	assert.Equal(t, changed, false)
}

func TestWriteUserPathCreatesWhenMissing(t *testing.T) {
	keyPath := withTestEnvKey(t, "", registry.EXPAND_SZ)
	r := &PathManagementReconciler{BinDir: `C:\bin`}

	changed, err := r.writeUserPath(appv1alpha1.AddPathFront, false)
	assert.NilError(t, err)
	assert.Equal(t, changed, true)

	val, valType := readTestPath(t, keyPath)
	assert.Equal(t, val, `C:\bin`)
	// A brand new value comes out as REG_EXPAND_SZ.
	assert.Equal(t, valType, uint32(registry.EXPAND_SZ))
}
