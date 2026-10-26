// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/app/v1alpha1"
	service "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/service/cmd"
)

// appLimaVMName is the LimaVM instance the App controller manages.
const appLimaVMName = "rd"

// containerdGuestSocket is the shared containerd socket inside the VM,
// matching the guestSocket forward in the App controller's lima template.
const containerdGuestSocket = "/run/k3s/containerd/containerd.sock"

func newNerdctlCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "nerdctl",
		Short: "Run nerdctl inside the Rancher Desktop VM",
		Long: `Run nerdctl inside the Rancher Desktop VM.

All arguments pass through to nerdctl. Rancher Desktop must already be
running with the containerd engine; "rdd run nerdctl" starts it first.`,
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		SilenceErrors:      true,
		RunE:               nerdctlAction,
	}
}

var errNerdctlNoApp = errors.New(`the App does not exist; create it with "rdd set running=true containerEngine.name=containerd"`)

func nerdctlAction(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	// Check first, because getAppKubeClient starts a stopped service and
	// nerdctl never starts anything. A missing control plane means a fresh
	// instance, which has no App either.
	if !service.Exists() {
		return errNerdctlNoApp
	}
	if !service.Running() {
		return errors.New(`the control plane is not running; run "rdd start" and try again`)
	}
	c, _, err := getAppKubeClient(ctx)
	if err != nil {
		return err
	}
	var app appv1alpha1.App
	if err := c.Get(ctx, client.ObjectKey{Name: "app"}, &app); err != nil {
		if apierrors.IsNotFound(err) {
			return errNerdctlNoApp
		}
		return fmt.Errorf("failed to get App: %w", err)
	}
	if err := nerdctlAppError(&app); err != nil {
		return err
	}
	// nerdctl must run as root in the guest. Non-root nerdctl insists on
	// rootless mode even when given an explicit --address.
	// TODO: let the distro's nerdctl wrapper own the address instead.
	guestCmd := append([]string{"sudo", "nerdctl", "--address", containerdGuestSocket}, args...)
	return limaVMGuestExec(ctx, appLimaVMName, "", "", guestCmd)
}

// nerdctlAppError returns why nerdctl cannot run against the App, or nil.
// nerdctl needs the containerd engine with ContainerEngineReady True at the
// App's current generation.
func nerdctlAppError(app *appv1alpha1.App) error {
	if engine := app.Spec.ContainerEngine.Name; engine != "containerd" {
		return fmt.Errorf(`nerdctl needs the containerd engine, but the App uses %q; switch with "rdd set containerEngine.name=containerd"`, engine)
	}
	cond := apimeta.FindStatusCondition(app.Status.Conditions, appv1alpha1.AppConditionContainerEngineReady)
	if cond == nil || cond.Status != metav1.ConditionTrue || cond.ObservedGeneration < app.Generation {
		return errors.New(`the containerd engine is not ready; run "rdd start" and try again`)
	}
	return nil
}
