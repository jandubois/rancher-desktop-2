// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package capi bundles the CRDs that rdd's Cluster API infrastructure provider
// installs into a guest cluster.
package capi

import _ "embed"

// CRDs holds the CustomResourceDefinitions of the LimaCluster, LimaMachine and
// LimaMachineTemplate kinds. The capi controller installs them into the guest
// cluster, not into rdd's own API server.
//
//go:embed crds.yaml
var CRDs string
