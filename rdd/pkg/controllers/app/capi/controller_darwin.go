// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/base"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/instance"
)

const controllerName = "capi"

// Only macOS registers the controller, because the node VMs that the
// provider creates use Lima's vzNAT network with guest-to-guest traffic,
// which works only on macOS.
func init() {
	base.RegisterController(&controller{})
}

type controller struct{}

var _ base.Controller = &controller{}

func (c *controller) GetName() string {
	return controllerName
}

func (c *controller) GetAPIGroup() string {
	// The empty group enables this controller only through "*" or its
	// name, like the kubernetes controller. Group "lima" would also enable
	// it in the lima-only control plane. That control plane doesn't have
	// the App CRD, so watching Apps there would fail the cache sync and
	// stop the manager.
	return ""
}

func (c *controller) GetCRDData() string {
	// Our CRDs go into the guest cluster, not rdd's control plane.
	return ""
}

func (c *controller) RegisterWithManager(_ context.Context, mgr ctrl.Manager) error {
	if err := appv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return err
	}
	r := &Reconciler{
		Client:        mgr.GetClient(),
		K3sConfigPath: instance.K3sConfig(),
	}
	return r.SetupWithManager(mgr)
}
