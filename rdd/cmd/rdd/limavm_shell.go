// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors
// SPDX-FileCopyrightText: The Lima Authors

package main

import (
	"fmt"
	"os"
	"strings"

	"al.essio.dev/pkg/shellescape"
	"github.com/mattn/go-isatty"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/guestexec"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/instance"
)

func newLimaVMShellCommand() *cobra.Command {
	shellCmd := &cobra.Command{
		Use:           "shell INSTANCE [COMMAND...]",
		Short:         "Execute shell in Lima VM",
		Long:          "Open an interactive shell or execute a command in a Lima VM instance.",
		Args:          cobra.MinimumNArgs(1),
		RunE:          limaVMShellAction,
		SilenceErrors: true,
	}

	shellCmd.Flags().SetInterspersed(false)
	shellCmd.Flags().String("shell", "", "Shell interpreter, e.g. /bin/bash")
	shellCmd.Flags().String("workdir", "", "Working directory")

	return shellCmd
}

func limaVMShellAction(cmd *cobra.Command, args []string) error {
	logrus.SetLevel(logrus.InfoLevel)
	ctx := cmd.Context()

	// Validate the VM exists in the API server
	c, err := getKubeClient(ctx)
	if err != nil {
		return err
	}
	_, err = findLimaVM(ctx, c, args[0])
	if err != nil {
		return err
	}

	// Set LIMA_HOME for the Lima library
	if err := os.Setenv("LIMA_HOME", instance.LimaHome()); err != nil {
		return fmt.Errorf("failed to set LIMA_HOME: %w", err)
	}

	// Get the Lima instance from the store
	inst, err := guestexec.Inspect(ctx, args[0])
	if err != nil {
		return err
	}

	// Build working directory change command
	var changeDirCmd string
	workDir, err := cmd.Flags().GetString("workdir")
	if err != nil {
		return err
	}
	if workDir != "" {
		changeDirCmd = fmt.Sprintf("cd %s || exit 1", shellescape.Quote(workDir))
	} else if len(inst.Config.Mounts) > 0 {
		hostCurrentDir, err := os.Getwd()
		if err == nil {
			changeDirCmd = fmt.Sprintf("cd %s", shellescape.Quote(hostCurrentDir))
		} else {
			changeDirCmd = "false"
			logrus.WithError(err).Warn("failed to get the current directory")
		}
		hostHomeDir, err := os.UserHomeDir()
		if err == nil {
			changeDirCmd = fmt.Sprintf("%s || cd %s", changeDirCmd, shellescape.Quote(hostHomeDir))
		} else {
			logrus.WithError(err).Warn("failed to get the home directory")
		}
	} else {
		logrus.Debug("the host home does not seem mounted, so the guest shell will have a different cwd")
	}

	if changeDirCmd == "" {
		changeDirCmd = "false"
	}
	logrus.Debugf("changeDirCmd=%q", changeDirCmd)

	// Determine shell
	shell, err := cmd.Flags().GetString("shell")
	if err != nil {
		return err
	}
	if shell == "" {
		shell = `"$SHELL"`
	} else {
		shell = shellescape.Quote(shell)
	}

	// Build script
	script := fmt.Sprintf("%s ; exec %s --login", changeDirCmd, shell)
	if len(args) > 1 {
		quotedArgs := make([]string, len(args[1:]))
		for i, arg := range args[1:] {
			quotedArgs[i] = shellescape.Quote(arg)
		}
		script += fmt.Sprintf(" -c %s", shellescape.Quote(strings.Join(quotedArgs, " ")))
	}

	// Build SSH command
	var opts guestexec.Options
	opts.TTY = isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
	if _, present := os.LookupEnv("COLORTERM"); present {
		opts.SendEnv = []string{"COLORTERM"}
	}

	sshCmd, err := guestexec.Command(ctx, inst, script, opts)
	if err != nil {
		return err
	}
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr

	logrus.Debugf("executing ssh: %+v", sshCmd.Args)

	return sshCmd.Run()
}
