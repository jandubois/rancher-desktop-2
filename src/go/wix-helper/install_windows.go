// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package main

import (
	"context"

	"github.com/sirupsen/logrus"

	wslutils "github.com/rancher-sandbox/rancher-desktop/src/go/wix-helper/pkg/wsl-utils"
)

func setupLogger(hInstall MSIHANDLE) *logrus.Entry {
	return logrus.NewEntry(&logrus.Logger{
		Out:       &msiWriter{hInstall: hInstall},
		Formatter: &logrus.TextFormatter{},
		Hooks:     make(logrus.LevelHooks),
		Level:     logrus.TraceLevel,
	})
}

// UpdateWSLImpl updates the previously installed WSL.
// This needs to be run as the user, and may request elevation.
func UpdateWSLImpl(hInstall MSIHANDLE) uint32 {
	ctx := context.Background()
	log := setupLogger(hInstall)

	log.Info("Updating WSL...")
	err := submitMessage(hInstall, INSTALLMESSAGE_ACTIONSTART, []string{
		"", "UpdateWSL", "Updating Windows Subsystem for Linux...", "<unused>",
	})
	if err != nil {
		log.WithError(err).Info("Failed to update progress")
	}
	if err := wslutils.UpdateWSL(ctx, log); err != nil {
		log.WithError(err).Error("Updating WSL failed")
		return 1
	}

	log.Info("WSL successfully updated.")
	return 0
}
