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
// given name is a valid Kubernetes object name, and it is not in the same form
// as the hashed output, then the input is returned as-is.  Otherwise, the
// output consists of the prefix, followed by a dash, and a SHA-256 hash.  The
// hash is over the given input, followed by any extra strings provided,
// separated by nulls.
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
