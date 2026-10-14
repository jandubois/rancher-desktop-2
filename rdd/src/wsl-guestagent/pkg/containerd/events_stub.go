// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

//go:build !linux

package containerd

import (
	"context"
	"fmt"

	"github.com/rancher-sandbox/rancher-desktop/src/wsl-guestagent/pkg/tracker"
)

type EventMonitor struct{}

func NewEventMonitor(containerdSock string, portTracker tracker.Tracker) (*EventMonitor, error) {
	panic("not implement for non-Linux")
}

func (e *EventMonitor) MonitorPorts(ctx context.Context) {
	panic("not implement for non-Linux")
}

func (e *EventMonitor) IsServing(ctx context.Context) error {
	return fmt.Errorf("not implemented for non-Linux")
}

func (e *EventMonitor) Close() error {
	return fmt.Errorf("not implemented for non-Linux")
}
