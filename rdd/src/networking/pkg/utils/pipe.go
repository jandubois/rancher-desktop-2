// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package utils provides shared networking helper utilities.
package utils

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/sirupsen/logrus"
)

// Pipe forwards data bidirectionally between conn and the upstream TCP address.
func Pipe(ctx context.Context, conn net.Conn, upstreamAddr string) {
	dialer := net.Dialer{
		Timeout: 5 * time.Second,
	}
	upstream, err := dialer.DialContext(ctx, "tcp", upstreamAddr)
	if err != nil {
		logrus.Errorf("Failed to dial upstream %s: %s", upstreamAddr, err)
		return
	}
	go func() {
		if _, err := io.Copy(upstream, conn); err != nil {
			logrus.Debugf("Error copying to upstream: %s", err)
		}
		if err = upstream.Close(); err != nil {
			logrus.Debugf("error closing connection while writing to upstream: %s", err)
		}
	}()

	if _, err := io.Copy(conn, upstream); err != nil {
		logrus.Debugf("Error copying from upstream: %s", err)
	}
	if err = upstream.Close(); err != nil {
		logrus.Debugf("error closing connection: %s", err)
	}
}
