// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"testing"
	"testing/fstest"

	"gotest.tools/v3/assert"
)

func TestNewUIPlugin(t *testing.T) {
	files := fstest.MapFS{
		"plugin/package.json": {Data: []byte(`{"name": "rancher-desktop", "version": "0.1.0", "main": "rancher-desktop-0.1.0.umd.min.js"}`)},
		"metadata.json":       {Data: []byte(`{"catalog.cattle.io/ui-extensions-version": ">= 3.0.0 < 4.0.0", "rancherdesktop.io/content-hash": "abc"}`)},
	}
	endpoint := "http://host.rancher-desktop.internal:9092/capi/uiextension"
	plugin, err := newUIPlugin(files, endpoint)
	assert.NilError(t, err)
	assert.Equal(t, plugin.GetAPIVersion(), "catalog.cattle.io/v1")
	assert.Equal(t, plugin.GetKind(), "UIPlugin")
	assert.Equal(t, plugin.GetNamespace(), "cattle-ui-plugin-system")
	assert.Equal(t, plugin.GetName(), "rancher-desktop")
	assert.DeepEqual(t, plugin.Object["spec"], map[string]any{
		"plugin": map[string]any{
			"name":     "rancher-desktop",
			"version":  "0.1.0",
			"endpoint": endpoint,
			"noCache":  false,
			"metadata": map[string]any{
				"catalog.cattle.io/ui-extensions-version": ">= 3.0.0 < 4.0.0",
				"rancherdesktop.io/content-hash":          "abc",
			},
		},
	})
}

func TestNewUIPluginRejectsPackageWithoutVersion(t *testing.T) {
	files := fstest.MapFS{
		"plugin/package.json": {Data: []byte(`{"name": "rancher-desktop"}`)},
		"metadata.json":       {Data: []byte(`{}`)},
	}
	_, err := newUIPlugin(files, "http://example")
	assert.ErrorContains(t, err, "lacks a name or version")
}

func TestUIExtensionURL(t *testing.T) {
	got, err := uiExtensionURL("http://localhost:9092/")
	assert.NilError(t, err)
	assert.Equal(t, got, "http://host.rancher-desktop.internal:9092/capi/uiextension")

	_, err = uiExtensionURL("")
	assert.ErrorIs(t, err, errUIPluginPending)
}
