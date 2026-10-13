// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package utils provides utility functions for the guestagent.
package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

var (
	ErrExecIptablesRule  = errors.New("failed updating iptables rules")
	ErrIPAddressNotFound = errors.New("IP address not found in line")
)

// NormalizeHostIP checks if the provided IP address is valid.
// The valid options are "127.0.0.1" and "0.0.0.0". If the input is "127.0.0.1",
// it returns "127.0.0.1". Any other address will be mapped to "0.0.0.0".
func NormalizeHostIP(ip string) string {
	if ip == "127.0.0.1" || ip == "localhost" {
		return ip
	}
	return "0.0.0.0"
}

func GenerateID(entry string) string {
	hasher := sha256.New()
	hasher.Write([]byte(entry))
	return hex.EncodeToString(hasher.Sum(nil))
}
