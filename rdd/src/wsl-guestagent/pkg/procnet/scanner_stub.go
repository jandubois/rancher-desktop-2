// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

//go:build !linux

package procnet

import (
	"context"
	"fmt"
	"time"

	"github.com/rancher-sandbox/rancher-desktop/src/wsl-guestagent/pkg/tracker"
)

type ProcNetScanner struct{}

func NewProcNetScanner(context.Context, tracker.Tracker, time.Duration) (*ProcNetScanner, error) {
	panic("only implemented for Linux")
}

func (p *ProcNetScanner) ForwardPorts() error {
	return fmt.Errorf("only implemented for Linux")
}
