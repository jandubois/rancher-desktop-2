// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package log provides logging setup utilities for networking processes.
package log

import (
	"os"

	"github.com/sirupsen/logrus"
)

const fileMode = 0o666

// SetOutputFile points the logger at filePath. When appendMode is true the file
// is opened for appending, so its contents survive a process restart; otherwise
// the file is truncated on open.
func SetOutputFile(filePath string, appendMode bool, logger *logrus.Logger) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if appendMode {
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	logFile, err := os.OpenFile(filePath, flags, fileMode)
	if err != nil {
		return err
	}
	logger.SetOutput(logFile)

	return nil
}
