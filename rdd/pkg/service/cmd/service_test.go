// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors
package service

import (
	"context"
	"flag"
	"testing"

	"gotest.tools/v3/assert"

	"k8s.io/klog/v2"
)

// Setting -v on the serve command must only update the logging
// configuration, which logsapi.ValidateAndApply later applies to klog.
// Flag sets are merged in map order, so the test builds the command
// repeatedly to make both orders all but certain to occur.
func TestServeCommandVerbosityFlagIsTheLoggingConfiguration(t *testing.T) {
	klogFlags := flag.NewFlagSet("klog", flag.PanicOnError)
	klog.InitFlags(klogFlags)
	t.Cleanup(func() { assert.NilError(t, klogFlags.Set("v", "0")) })

	for range 50 {
		assert.NilError(t, klogFlags.Set("v", "0"))
		command := NewServeCommand(context.Background())
		assert.NilError(t, command.Flags().Set("v", "4"))
		assert.Assert(t, !klog.V(4).Enabled(), "-v is bound to klog, not to the logging configuration")
	}
}
