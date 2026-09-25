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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MirrorNameType is an interface that must be implemented by types that can
// have mirrored names.
type MirrorNameType interface {
	client.Object
	// MirrorPrefix returns the prefix used for mirrored names of this type.
	// Implementations must take a pointer receiver, and can be called with it
	// being nil.
	MirrorPrefix() string
}

// MirrorName returns the mirrored name for a given container resource name.  If
// the given name is a valid Kubernetes object name, and it is not in the same
// form as the hashed output, then the input is returned as-is.  Otherwise, the
// output consists of a type-specific prefix, followed by a dash, and a SHA-256
// hash.  The hash is over the given name, followed by any extra strings
// provided, separated by nulls.
func MirrorName[T MirrorNameType](name string, extras ...string) string {
	var t T // Nil T, but MirrorPrefix must support this.
	prefix := t.MirrorPrefix()
	hasher := sha256.New()
	needsEscape := len(validation.IsDNS1123Subdomain(name)) > 0
	if !needsEscape {
		after, found := strings.CutPrefix(name, prefix+"-")
		if found && len(after) == hex.EncodedLen(hasher.Size()) {
			// Check that the string only consists of hexadecimal characters.
			needsEscape = strings.TrimLeft(after, "0123456789abcdef") == ""
		}
	}
	if !needsEscape {
		return name
	}
	_, _ = io.WriteString(hasher, name)
	for _, extra := range extras {
		_, _ = fmt.Fprintf(hasher, "\x00%s", extra)
	}
	return fmt.Sprintf("%s-%x", prefix, hasher.Sum(nil))
}
