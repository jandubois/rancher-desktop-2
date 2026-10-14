// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package iptables handles forwarding ports found in iptables DNAT
package iptables

import "github.com/lima-vm/lima/pkg/guestagent/iptables"

// Scanner is the interface that wraps the GetPorts method which
// is used to scan the iptables.
type Scanner interface {
	GetPorts() ([]iptables.Entry, error)
}

type IptablesScanner struct{}

func NewIptablesScanner() *IptablesScanner {
	return &IptablesScanner{}
}

func (i *IptablesScanner) GetPorts() ([]iptables.Entry, error) {
	return iptables.GetPorts()
}
