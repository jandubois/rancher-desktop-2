// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package api provides utility functions for working with the RDD API.
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// MirrorName returns the mirrored name for a given container resource.  The
// prefix is prepended to the mirrored name with a dash if the input is not a
// valid Kubernetes object name, or if it looks like the encoded output of this
// function.
func MirrorName(prefix, input string) string {
	hasher := sha256.New()
	needsEscape := len(validation.IsDNS1123Subdomain(input)) > 0
	if !needsEscape {
		after, found := strings.CutPrefix(input, prefix+"-")
		if found && len(after) == hex.EncodedLen(hasher.Size()) {
			// Check that the string only consists of hexadecimal characters.
			needsEscape = strings.TrimLeft(after, "0123456789abcdef") == ""
		}
	}
	if !needsEscape {
		return input
	}
	_, _ = hasher.Write([]byte(input))
	return fmt.Sprintf("%s-%x", prefix, hasher.Sum(nil))
}
