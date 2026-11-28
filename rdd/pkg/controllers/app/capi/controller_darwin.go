// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"context"
	"fmt"
	"net/http"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
	limav1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/lima/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/base"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/instance"
	servicecontrollers "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/service/controllers"
)

// Only macOS registers the controller, because the node VMs that the
// provider creates use Lima's vzNAT network with guest-to-guest traffic,
// which works only on macOS.
func init() {
	base.RegisterController(&controller{})
}

type controller struct{}

var (
	_ base.Controller            = &controller{}
	_ base.PassthroughController = &controller{}
)

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

// GetPassthroughEndpoints returns the UI extension's endpoint when rdd embeds
// the extension. Rancher fetches the extension's files from it over plain
// HTTP.
func (c *controller) GetPassthroughEndpoints() []string {
	if uiExtensionFiles == nil {
		return nil
	}
	return []string{uiExtensionEndpoint}
}

func (c *controller) GetPassthroughHandler(endpoint string) http.Handler {
	if endpoint != uiExtensionEndpoint || uiExtensionFiles == nil {
		return nil
	}
	return http.FileServer(http.FS(uiExtensionFiles))
}

func (c *controller) RegisterWithManager(_ context.Context, mgr ctrl.Manager) error {
	// guestexec reaches the node VMs through the Lima library, which reads
	// LIMA_HOME.
	if err := os.Setenv("LIMA_HOME", instance.LimaHome()); err != nil {
		return fmt.Errorf("failed to set LIMA_HOME: %w", err)
	}
	if err := appv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return err
	}
	if err := limav1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return err
	}
	// The discovery reads the rdd-controller-manager ConfigMap straight from
	// rdd's control plane. A lookup through mgr.GetClient() would make the
	// manager's cache start watching ConfigMaps.
	discovery, err := servicecontrollers.NewControllerManagerDiscovery(mgr.GetConfig())
	if err != nil {
		return err
	}
	r := &Reconciler{
		Client:        mgr.GetClient(),
		apiReader:     mgr.GetAPIReader(),
		K3sConfigPath: instance.K3sConfig(),
		LimaHome:      instance.LimaHome(),
		probeNode:     probeNodeOverSSH,
		lookupPassthroughEndpoint: func(ctx context.Context) (string, error) {
			return discovery.LookupPassthroughEndpoint(ctx, controllerName, uiExtensionEndpoint)
		},
	}
	return r.SetupWithManager(mgr)
}
