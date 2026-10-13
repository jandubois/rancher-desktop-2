// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package vsock

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"github.com/linuxkit/virtsock/pkg/hvsock"
)

func Listen(vmGUID hvsock.GUID, vsockPort uint32) (net.Listener, error) {
	svcPort, err := hvsock.GUIDFromString(winio.VsockServiceID(vsockPort).String())
	if err != nil {
		return nil, fmt.Errorf("Listen, could not parse Hyper-v service GUID: %v", err)
	}

	addr := hvsock.Addr{
		VMID:      vmGUID,
		ServiceID: svcPort,
	}

	return hvsock.Listen(addr)
}
