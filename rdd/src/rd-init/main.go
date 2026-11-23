// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Command rd-init is a minimal implementation of cloud-init to start lima.
// Note that this only implements `cloud-init-local`.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/coreos/go-systemd/daemon"
	"github.com/coreos/go-systemd/v22/dbus"
)

func runCommand(ctx context.Context, name string, arg ...string) error {
	slog.DebugContext(ctx, "Running command", "name", name, "arg", arg)
	cmd := exec.CommandContext(ctx, name, arg...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func run(ctx context.Context) error {
	var units []string
	fns := []func(context.Context) ([]string, error){
		LoadMetadata,
		LoadUserData,
		LoadNetworkConfig,
	}
	for _, fn := range fns {
		if newUnits, err := fn(ctx); err != nil {
			return err
		} else {
			units = append(units, newUnits...)
		}
	}

	slog.InfoContext(ctx, "reloading systemd", "units", units)

	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to systemd: %w", err)
	}
	defer conn.Close()

	if err := conn.ReloadContext(ctx); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	// Reload udev so it reads the new .link files and names any network
	// interface that appears from now on.
	slog.InfoContext(ctx, "reloading udev")
	if err := runCommand(ctx, "/usr/bin/udevadm", "control", "--reload"); err != nil {
		return fmt.Errorf("failed to reload udev: %w", err)
	}

	// Queue the unit restarts before notifying ready. A unit that is ordered
	// after rd-init starts once rd-init is ready. If it is also ordered after
	// one of these units, as lima-init is after systemd-networkd, it waits
	// only for a restart that is already queued by then.
	//
	// But wait for the results only after notifying ready. systemd-networkd
	// starts after network-pre.target, which waits for rd-init to be ready,
	// so waiting first would block boot.
	//
	// The channels are buffered because go-systemd sends each job result
	// while holding a lock, and it needs the same lock to queue another job
	// and to deliver any other result. So an unread result would block both.
	type restartJob struct {
		unit string
		ch   chan string
	}
	var jobs []restartJob
	seenUnits := make(map[string]bool)
	for _, unit := range units {
		if seenUnits[unit] {
			continue
		}
		seenUnits[unit] = true
		ch := make(chan string, 1)
		_, err = conn.RestartUnitContext(ctx, unit, "replace", ch)
		if err != nil {
			return fmt.Errorf("failed to start unit %s: %w", unit, err)
		}
		jobs = append(jobs, restartJob{unit: unit, ch: ch})
	}

	if _, err := daemon.SdNotify(true, daemon.SdNotifyReady); err != nil {
		return fmt.Errorf("failed to notify systemd: %w", err)
	}

	for _, job := range jobs {
		select {
		case result := <-job.ch:
			if result == "done" {
				slog.InfoContext(ctx, "restarted systemd unit", "unit", job.unit, "result", result)
			} else {
				slog.ErrorContext(ctx, "failed to restart systemd unit", "unit", job.unit, "result", result)
			}
		case <-ctx.Done():
			return fmt.Errorf("context closed while waiting for systemd unit %s: %w", job.unit, ctx.Err())
		}
	}

	return nil
}

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		slog.ErrorContext(ctx, "rd-init failed", "error", err)
		os.Exit(1)
	}
}
