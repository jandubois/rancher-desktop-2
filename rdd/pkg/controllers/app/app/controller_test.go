// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package app

import (
	"testing"

	"github.com/lima-vm/lima/v2/pkg/limatype"
	"github.com/lima-vm/lima/v2/pkg/limayaml"
	"gotest.tools/v3/assert"
)

// Test_limaTemplateData_omitsVMResources guards the duplicate-key invariant.
// The app controller appends cpus and memory, so the template must omit them.
func Test_limaTemplateData_omitsVMResources(t *testing.T) {
	t.Parallel()

	y := &limatype.LimaYAML{}
	assert.NilError(t, limayaml.Unmarshal([]byte(limaTemplateData()), y, ""))
	assert.Assert(t, y.CPUs == nil, "template must not set cpus")
	assert.Assert(t, y.Memory == nil, "template must not set memory")
}
