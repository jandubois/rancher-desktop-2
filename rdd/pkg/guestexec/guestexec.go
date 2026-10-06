// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors
// SPDX-FileCopyrightText: The Lima Authors

// Package guestexec runs commands inside a Lima VM over ssh.
//
// The caller must have set LIMA_HOME: the CLI sets it itself, and the lima
// controller sets it in-process at startup.
package guestexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"

	"github.com/coreos/go-semver/semver"
	"github.com/lima-vm/lima/v2/pkg/limatype"
	"github.com/lima-vm/lima/v2/pkg/sshutil"
	"github.com/lima-vm/lima/v2/pkg/store"
)

// Inspect loads the named Lima instance from the store and checks that it
// is running and has a usable configuration.
func Inspect(ctx context.Context, instanceName string) (*limatype.Instance, error) {
	inst, err := store.Inspect(ctx, instanceName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("instance %q does not exist on disk", instanceName)
		}
		return nil, err
	}
	if len(inst.Errors) > 0 {
		return nil, fmt.Errorf("instance %q has configuration errors: %w", instanceName, errors.Join(inst.Errors...))
	}
	if inst.Config == nil {
		return nil, fmt.Errorf("instance %q has no configuration", instanceName)
	}
	if inst.Status != limatype.StatusRunning {
		return nil, fmt.Errorf("instance %q is not running (status: %s), use 'rdd lima start %s' first", instanceName, inst.Status, instanceName)
	}
	return inst, nil
}

// Options adjust the ssh session for an interactive caller.
type Options struct {
	TTY     bool     // request a pseudo-terminal (ssh -t)
	SendEnv []string // environment variables to forward (ssh -o SendEnv=NAME)
}

// Command returns an unstarted ssh command that runs script in inst as the
// instance's user. The caller wires up stdio and runs it.
func Command(ctx context.Context, inst *limatype.Instance, script string, opts Options) (*exec.Cmd, error) {
	sshExe, err := sshutil.NewSSHExe()
	if err != nil {
		return nil, err
	}

	sshOpts, err := sshutil.SSHOpts(
		ctx,
		sshExe,
		inst.Dir,
		*inst.Config.User.Name,
		*inst.Config.SSH.LoadDotSSHPubKeys,
		*inst.Config.SSH.ForwardAgent,
		*inst.Config.SSH.ForwardX11,
		*inst.Config.SSH.ForwardX11Trusted)
	if err != nil {
		return nil, err
	}

	if runtime.GOOS == "windows" {
		sshOpts = sshutil.SSHOptsRemovingControlPath(sshOpts)
	}

	sshArgs := append([]string{}, sshExe.Args...)
	sshArgs = append(sshArgs, sshutil.SSHArgsFromOpts(sshOpts)...)

	if opts.TTY {
		sshArgs = append(sshArgs, "-t")
	}

	for _, name := range opts.SendEnv {
		sshArgs = append(sshArgs, "-o", "SendEnv="+name)
	}

	logLevel := "ERROR"
	olderSSH := sshutil.DetectOpenSSHVersion(ctx, sshExe).LessThan(*semver.New("8.9.0"))
	if olderSSH {
		logLevel = "QUIET"
	}

	// ConnectTimeout caps the TCP handshake at 30s. ServerAliveInterval=30
	// with ServerAliveCountMax=3 closes a wedged session after ~90s of
	// unanswered keep-alives. Interactive shells and long-running commands
	// ack the keep-alives and stay connected.
	sshArgs = append(sshArgs, []string{
		"-o", fmt.Sprintf("LogLevel=%s", logLevel),
		"-o", "ConnectTimeout=30",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
		"-p", strconv.Itoa(inst.SSHLocalPort),
		inst.SSHAddress,
		"--",
		script,
	}...)

	return exec.CommandContext(ctx, sshExe.Exe, sshArgs...), nil
}
