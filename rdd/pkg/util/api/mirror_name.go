// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package api provides utility functions for working with the RDD API.
package api

import (
	"crypto/sha256"
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation"
)

// MirrorName returns the mirrored name for a given container resource.  The
// prefix is prepended to the mirrored name with a dash if the input is not a
// valid Kubernetes object name.
func MirrorName(prefix, input string) string {
	if len(validation.IsDNS1123Subdomain(input)) < 1 {
		return input
	}
	return fmt.Sprintf("%s-%x", prefix, sha256.Sum256([]byte(input)))
}
