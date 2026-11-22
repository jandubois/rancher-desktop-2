// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/lima-vm/lima/v2/pkg/limatype"
	"github.com/lima-vm/lima/v2/pkg/limayaml"
	"gotest.tools/v3/assert"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/capi/v1alpha1"
)

// loadTemplate renders and checks a template the way rdd's LimaVM webhook does.
func loadTemplate(t *testing.T, spec v1alpha1.LimaMachineSpec, script []byte) *limatype.LimaYAML {
	t.Helper()
	// Keep the developer's Lima default.yaml and override.yaml out of Load.
	t.Setenv("LIMA_HOME", t.TempDir())
	data, err := LimaTemplate(spec, script)
	assert.NilError(t, err)
	y, err := limayaml.Load(t.Context(), data, "")
	assert.NilError(t, err)
	assert.NilError(t, limayaml.Validate(y, false), "%s", data)
	return y
}

func TestLimaTemplateScriptSurvivesTemplating(t *testing.T) {
	// Template syntax and YAML syntax that would break a hand-built template.
	script := []byte("#!/bin/sh\necho '{{.User}}' \"key: [value]\" # " + secret + "\n")
	y := loadTemplate(t, v1alpha1.LimaMachineSpec{}, script)
	data := y.Provision[0]
	assert.Equal(t, *data.Path, installScriptData)
	assert.Assert(t, *data.Content == base64.StdEncoding.EncodeToString(script), "Load changed the install script data")
	assert.Assert(t, !strings.Contains(*y.Provision[1].Script, "{{"), "guard script contains template syntax")
}

func TestLimaTemplateSizing(t *testing.T) {
	disk := resource.MustParse("100Gi")
	tests := []struct {
		name   string
		spec   v1alpha1.LimaMachineSpec
		cpus   int
		memory string
		disk   string
	}{
		// Lima's own default disk is 100GiB.
		{"defaults", v1alpha1.LimaMachineSpec{}, 2, "3221225472", "100GiB"},
		{"spec", v1alpha1.LimaMachineSpec{CPUs: 1, Memory: new(resource.MustParse("1Gi")), Disk: &disk}, 1, "1073741824", "107374182400"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			y := loadTemplate(t, tt.spec, []byte("true\n"))
			assert.Equal(t, *y.CPUs, tt.cpus)
			assert.Equal(t, *y.Memory, tt.memory)
			assert.Equal(t, *y.Disk, tt.disk)
		})
	}
}

func TestValidateMachineName(t *testing.T) {
	// instance.LimaHome() is ~/.rd<suffix>/lima.
	const limaHome = "/Users/someone/.rd2/lima"
	tests := []struct {
		name     string
		limaHome string
		machine  string
		wantErr  bool
	}{
		{"valid", limaHome, "pool1-abcde-12345", false},
		{"not an instance name", limaHome, "pool--a", true},
		{"dot in the hostname", limaHome, "pool.a-1", true},
		// A short LIMA_HOME, so the hostname limit is hit before the socket limit.
		{"hostname 63", "/l", strings.Repeat("a", 58), false},
		{"hostname 64", "/l", strings.Repeat("a", 59), true},
		// limaHome is 24 characters and filenames.LongestSock is 25, so the
		// socket path is 24 + 1 + len(name) + 1 + 25 and must stay below 104.
		{"socket 103", limaHome, strings.Repeat("a", 52), false},
		{"socket 104", limaHome, strings.Repeat("a", 53), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMachineName(tt.limaHome, tt.machine)
			assert.Equal(t, err != nil, tt.wantErr, "error: %v", err)
		})
	}
}
