// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package api provides utility functions for working with the RDD API.
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// MirrorName returns the mirrored name for a given container resource.  If the
// given name is not a valid Kubernetes object name, or if it looks like the
// hashed output of this function, the returned value is instead the prefix
// followed by the SHA-256 hash of the input, possibly followed by any extra
// strings provided, separated by nulls.
//
// Note that the extra values given are only used when the input needs to be
// encoded; two calls with the same input but different extras will return the
// same value.
func MirrorName(prefix, input string, extras ...string) string {
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
	_, _ = io.WriteString(hasher, input)
	for _, extra := range extras {
		_, _ = fmt.Fprintf(hasher, "\x00%s", extra)
	}
	return fmt.Sprintf("%s-%x", prefix, hasher.Sum(nil))
}
