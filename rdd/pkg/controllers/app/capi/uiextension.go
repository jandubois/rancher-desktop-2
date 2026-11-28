// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package capi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"

	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// uiExtensionEndpoint is the passthrough endpoint that serves the Rancher
	// UI extension, at /capi/uiextension/ on the passthrough server.
	uiExtensionEndpoint = "uiextension"

	// uiPluginCRDName is Rancher's UIPlugin CRD. Its presence in the guest
	// cluster tells us that Rancher is installed there.
	uiPluginCRDName = "uiplugins.catalog.cattle.io"

	// uiPluginNamespace is the only namespace in which Rancher looks for
	// UIPlugins. Rancher creates it.
	uiPluginNamespace = "cattle-ui-plugin-system"

	// uiExtensionHost is the name under which pods in the guest cluster reach
	// the host, where the passthrough server listens.
	uiExtensionHost = "host.rancher-desktop.internal"
)

var uiPluginGVK = schema.GroupVersionKind{Group: "catalog.cattle.io", Version: "v1", Kind: "UIPlugin"}

// uiExtensionFiles holds the staged Rancher UI extension, which consists of
// files.txt, metadata.json and plugin/. It is nil when rdd was built without
// the with_uiextension tag, and then the controller neither serves the
// extension nor creates a UIPlugin.
var uiExtensionFiles fs.FS

// errUIPluginPending means the UIPlugin cannot be written yet, and a later
// pass should try again.
var errUIPluginPending = errors.New("the UIPlugin cannot be written yet")

// newUIPlugin builds the UIPlugin that points Rancher at the extension in
// files, served at endpoint. Rancher indexes plugins by spec.plugin.name but
// updates their status by metadata.name, so both are the package name.
func newUIPlugin(files fs.FS, endpoint string) (*unstructured.Unstructured, error) {
	data, err := fs.ReadFile(files, "plugin/package.json")
	if err != nil {
		return nil, fmt.Errorf("read the UI extension's package.json: %w", err)
	}
	var pkg struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parse the UI extension's package.json: %w", err)
	}
	if pkg.Name == "" || pkg.Version == "" {
		return nil, errors.New("the UI extension's package.json lacks a name or version")
	}
	data, err = fs.ReadFile(files, "metadata.json")
	if err != nil {
		return nil, fmt.Errorf("read the UI extension's metadata.json: %w", err)
	}
	// The metadata holds a hash of the extension's content, so a rebuilt
	// extension changes the UIPlugin's spec and with it the generation.
	// Rancher fetches the files again only when the generation changes.
	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("parse the UI extension's metadata.json: %w", err)
	}

	plugin := &unstructured.Unstructured{}
	plugin.SetGroupVersionKind(uiPluginGVK)
	plugin.SetNamespace(uiPluginNamespace)
	plugin.SetName(pkg.Name)
	plugin.Object["spec"] = map[string]any{
		"plugin": map[string]any{
			"name":     pkg.Name,
			"version":  pkg.Version,
			"endpoint": endpoint,
			"noCache":  false,
			"metadata": metadata,
		},
	}
	return plugin, nil
}

// uiExtensionURL returns the URL at which pods in the guest cluster reach the
// extension. passthroughEndpoint is the passthrough server's URL on the host.
// uiExtensionURL keeps only its port, because pods reach the host under
// uiExtensionHost.
func uiExtensionURL(passthroughEndpoint string) (string, error) {
	if passthroughEndpoint == "" {
		return "", fmt.Errorf("%w: the passthrough endpoint %s/%s is not registered", errUIPluginPending, controllerName, uiExtensionEndpoint)
	}
	u, err := url.Parse(passthroughEndpoint)
	if err != nil {
		return "", fmt.Errorf("parse the passthrough endpoint %q: %w", passthroughEndpoint, err)
	}
	if u.Port() == "" {
		return "", fmt.Errorf("the passthrough endpoint %q has no port", passthroughEndpoint)
	}
	return fmt.Sprintf("http://%s:%s/%s/%s", uiExtensionHost, u.Port(), controllerName, uiExtensionEndpoint), nil
}

// reconcileUIPlugin creates or updates the UIPlugin for the embedded UI
// extension when the guest cluster runs Rancher. It writes the UIPlugin only
// when its spec.plugin differs from what we want, so its generation changes
// only when the extension or the passthrough port does.
func (r *Reconciler) reconcileUIPlugin(ctx context.Context) error {
	if uiExtensionFiles == nil || r.lookupPassthroughEndpoint == nil {
		return nil
	}
	if _, err := r.crdClient.Get(ctx, uiPluginCRDName, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			// Without Rancher there is nothing to register with.
			return nil
		}
		return fmt.Errorf("look up the UIPlugin CRD: %w", err)
	}
	var namespace corev1.Namespace
	if err := r.guestClient.Get(ctx, client.ObjectKey{Name: uiPluginNamespace}, &namespace); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("%w: namespace %s does not exist", errUIPluginPending, uiPluginNamespace)
		}
		return fmt.Errorf("get namespace %s: %w", uiPluginNamespace, err)
	}

	passthroughEndpoint, err := r.lookupPassthroughEndpoint(ctx)
	if err != nil {
		return fmt.Errorf("look up the passthrough endpoint: %w", err)
	}
	endpoint, err := uiExtensionURL(passthroughEndpoint)
	if err != nil {
		return err
	}
	desired, err := newUIPlugin(uiExtensionFiles, endpoint)
	if err != nil {
		return err
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(uiPluginGVK)
	if err := r.guestClient.Get(ctx, client.ObjectKeyFromObject(desired), existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get UIPlugin %s/%s: %w", desired.GetNamespace(), desired.GetName(), err)
		}
		if err := r.guestClient.Create(ctx, desired); err != nil {
			return fmt.Errorf("create UIPlugin %s/%s: %w", desired.GetNamespace(), desired.GetName(), err)
		}
		logf.FromContext(ctx).Info("Created UIPlugin in the guest cluster", "name", desired.GetName(), "endpoint", endpoint)
		return nil
	}

	// Compare only the fields that we set, so that fields that the API server
	// or Rancher fill in don't cause a write on every pass.
	desiredPlugin, _, _ := unstructured.NestedMap(desired.Object, "spec", "plugin")
	existingPlugin, _, _ := unstructured.NestedMap(existing.Object, "spec", "plugin")
	changed := false
	for key, value := range desiredPlugin {
		if !apiequality.Semantic.DeepEqual(existingPlugin[key], value) {
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}
	if existingPlugin == nil {
		existingPlugin = map[string]any{}
	}
	for key, value := range desiredPlugin {
		existingPlugin[key] = value
	}
	if err := unstructured.SetNestedMap(existing.Object, existingPlugin, "spec", "plugin"); err != nil {
		return fmt.Errorf("set UIPlugin spec: %w", err)
	}
	if err := r.guestClient.Update(ctx, existing); err != nil {
		return fmt.Errorf("update UIPlugin %s/%s: %w", existing.GetNamespace(), existing.GetName(), err)
	}
	logf.FromContext(ctx).Info("Updated UIPlugin in the guest cluster", "name", existing.GetName(), "endpoint", endpoint)
	return nil
}
