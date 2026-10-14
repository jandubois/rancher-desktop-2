// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

//go:build !linux

package kube

import (
	"context"
	"fmt"
	"net"

	"github.com/rancher-sandbox/rancher-desktop/src/wsl-guestagent/pkg/tracker"
)

func WatchForServices(
	ctx context.Context,
	configPath string,
	k8sServiceListenerIP net.IP,
	portTracker tracker.Tracker,
) error {
	return fmt.Errorf("not implemented for non-linux")
}
