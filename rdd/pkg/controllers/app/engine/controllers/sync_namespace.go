// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package controllers

import (
	"context"
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	containersv1alpha1 "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1"
	containersv1alpha1apply "github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/containers/v1alpha1/applyconfiguration/containers/v1alpha1"
)

// syncContainerNamespace creates the "moby" ContainerNamespace mirror and
// prunes every other one. This resource has no mirror finalizer: Docker has
// no corresponding engine object for the reverse delete, and
// cleanupMirrorResources sweeps it unconditionally on VM stop, so a
// finalizer with no handler would only trap user deletes in Terminating.
func (w *dockerWatcher) syncContainerNamespace(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("docker-watcher")

	applyConfig := containersv1alpha1apply.ContainerNamespace(containerNamespace, w.apiNamespace)
	if err := w.k8s.Apply(ctx, applyConfig,
		client.ForceOwnership, client.FieldOwner(controllerName)); err != nil {
		return err
	}

	// Docker has exactly one namespace, so every other ContainerNamespace
	// mirror is stale whatever created it; a containerd watcher that ran
	// before the backend changed is what produces them in practice. A failed
	// delete joins the returned error, as it does in every sibling prune:
	// that fails fullSync, and the watcher restart is what retries it, since
	// nothing else re-runs this.
	var nsMirrors containersv1alpha1.ContainerNamespaceList
	if err := w.k8s.List(ctx, &nsMirrors, client.InNamespace(w.apiNamespace)); err != nil {
		return fmt.Errorf("failed to list ContainerNamespaces: %w", err)
	}
	var errs []error
	for i := range nsMirrors.Items {
		ns := &nsMirrors.Items[i]
		if ns.Name == containerNamespace {
			continue
		}
		log.V(1).Info("Removing stale ContainerNamespace", "namespace", ns.Name)
		if err := w.removeMirrorResource(ctx, ns, ns.Name); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
