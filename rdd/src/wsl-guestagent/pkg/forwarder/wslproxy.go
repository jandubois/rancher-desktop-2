// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package forwarder implements a forwarding mechanism to forward
// port mappings to Rancher Desktop WSL Proxy.
package forwarder

import (
	"context"
	"encoding/json"
	"net"
	"time"

	"github.com/rancher-sandbox/rancher-desktop/src/ports"
)

// WSLProxyForwarder forwards the PortMappings to Rancher Desktop WSLProxy process in
// the default namespace over the unix socket.
// For more information on Rancher Desktop WSL Proxy, refer to the source code at:
// https://github.com/rancher-sandbox/rancher-desktop/blob/main/src/go/networking/cmd/proxy/wsl_integration_linux.go
type WSLProxyForwarder struct {
	ctx         context.Context
	dialer      net.Dialer
	proxySocket string
}

func NewWSLProxyForwarder(ctx context.Context, proxySocket string) *WSLProxyForwarder {
	return &WSLProxyForwarder{
		ctx:         ctx,
		dialer:      net.Dialer{Timeout: 5 * time.Second},
		proxySocket: proxySocket,
	}
}

// Send forwards the port mappings to WSL Proxy.
func (v *WSLProxyForwarder) Send(portMapping ports.PortMapping) error {
	conn, err := v.dialer.DialContext(v.ctx, "unix", v.proxySocket)
	if err != nil {
		return err
	}
	defer conn.Close()

	err = json.NewEncoder(conn).Encode(portMapping)
	if err != nil {
		return err
	}

	return nil
}
