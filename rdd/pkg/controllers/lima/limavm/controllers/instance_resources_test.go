// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"testing"

	"github.com/lima-vm/lima/v2/pkg/limatype"
	"gotest.tools/v3/assert"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/lima/v1alpha1"
)

func TestApplyConfigResources(t *testing.T) {
	vz := &limatype.LimaYAML{
		VMType: new(limatype.VZ),
		CPUs:   new(4),
		Memory: new("8GiB"),
		Disk:   new("100GiB"),
	}

	t.Run("VZ config sets all three fields", func(t *testing.T) {
		var status v1alpha1.LimaVMStatus
		assert.Assert(t, applyConfigResources(&status, vz))
		assert.Equal(t, status.CPUs, int32(4))
		assert.Equal(t, status.Memory.String(), "8Gi")
		assert.Equal(t, status.Disk.String(), "100Gi")
	})

	t.Run("WSL2 config clears the fields", func(t *testing.T) {
		wsl2 := *vz
		wsl2.VMType = new(limatype.WSL2)
		status := v1alpha1.LimaVMStatus{CPUs: 2, Memory: resource.NewQuantity(1<<30, resource.BinarySI)}
		assert.Assert(t, applyConfigResources(&status, &wsl2))
		assert.Equal(t, status.CPUs, int32(0))
		assert.Assert(t, status.Memory == nil)
		assert.Assert(t, status.Disk == nil)
	})

	t.Run("equal values in another format report no change", func(t *testing.T) {
		status := v1alpha1.LimaVMStatus{
			CPUs:   4,
			Memory: resource.NewQuantity(8<<30, resource.DecimalSI),
			Disk:   new(resource.MustParse("100Gi")),
		}
		assert.Assert(t, !applyConfigResources(&status, vz))
	})
}
