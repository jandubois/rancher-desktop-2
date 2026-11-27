// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package engine registers the engine controller. The engine controller
// mirrors container engine state into `ContainerNamespace`, `Container`,
// `Image`, and `Volume` resources in the containers.rancherdesktop.io API
// group, and forwards a user-initiated delete of a `Container`, `Image` or
// `Volume` back to the engine. A `ContainerNamespace` carries no finalizer
// and forwards nothing.
//
// containerd has no volume concept, so that backend never creates `Volume`
// resources.
//
// The engine controller's passthrough endpoints are `logs`, which streams a
// container's logs, and `exec`, which opens a shell in a container.
package engine

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"

	ctrl "sigs.k8s.io/controller-runtime"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
	containersv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/app/engine/controllers"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/base"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/instance"
)

func init() {
	base.RegisterController(newController())
}

// apiGroup is "containers" because the reconciler watches
// Container, Image, and Volume from that group and needs
// their CRDs at startup. Grouping engine with its dependencies
// keeps --controllers selections from splitting the two apart.
const apiGroup = "containers"

type controller struct {
	reconciler   *controllers.EngineReconciler
	passthroughs map[string]http.Handler
}

func newController() base.Controller {
	c := &controller{}
	c.passthroughs = map[string]http.Handler{
		"exec": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c.reconciler.HandleExec(w, r)
		}),
		"logs": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c.reconciler.HandleLogs(w, r)
		}),
	}
	return c
}

var (
	_ base.Controller            = &controller{}
	_ base.PassthroughController = &controller{}
)

func (c *controller) GetName() string {
	return appv1alpha1.EngineControllerName
}

func (c *controller) GetAPIGroup() string {
	return apiGroup
}

func (c *controller) GetCRDData() string {
	return ""
}

func (c *controller) GetPassthroughEndpoints() []string {
	return slices.Sorted(maps.Keys(c.passthroughs))
}

func (c *controller) GetPassthroughHandler(endpoint string) http.Handler {
	if c.reconciler == nil {
		return nil
	}
	return c.passthroughs[endpoint]
}

func (c *controller) RegisterWithManager(_ context.Context, mgr ctrl.Manager) error {
	// Container shells find the App's VM through Lima's store, which reads
	// LIMA_HOME. The LimaVM controller sets it too, but --controllers can run
	// this controller in a process without the LimaVM controller.
	if err := os.Setenv("LIMA_HOME", instance.LimaHome()); err != nil {
		return fmt.Errorf("failed to set LIMA_HOME: %w", err)
	}
	if err := appv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return err
	}
	if err := containersv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return err
	}

	c.reconciler = &controllers.EngineReconciler{
		Client: mgr.GetClient(),
	}
	return c.reconciler.SetupWithManager(mgr)
}
