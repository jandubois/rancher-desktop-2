// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package limavm

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/lima/v1alpha1"
)

// runTemplateLibrarySync writes the bundled templates into TemplateLibraryNamespace,
// retrying until a pass succeeds or ctx ends. The library webhook's failure policy
// is Fail, so the first passes can fail until the webhook is serving.
func runTemplateLibrarySync(ctx context.Context, writer client.Client, reader client.Reader, bundle fs.FS) {
	logger := log.FromContext(ctx)
	const maxDelay = 10 * time.Second
	delay := 200 * time.Millisecond
	for {
		err := syncTemplateLibrary(ctx, writer, reader, bundle)
		if err == nil {
			logger.Info("Synced Lima template library", "namespace", TemplateLibraryNamespace)
			return
		}
		logger.Error(err, "Failed to sync Lima template library, retrying", "delay", delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(2*delay, maxDelay)
	}
}

// syncTemplateLibrary creates TemplateLibraryNamespace if it is missing, then makes
// the library ConfigMaps in it match bundle.
//
// The first ConfigMap found at each bundled path becomes the managed entry for
// that path, and the sync rewrites it whenever it differs from bundle, so a user's
// edit is undone. The sync takes over a ConfigMap without the managed-by label in
// the same way, because the webhook would reject a second ConfigMap at that path.
//
// The sync deletes any further ConfigMap at a bundled path, and any managed
// ConfigMap whose path bundle no longer has. It creates the entries that are
// missing, and leaves unmanaged ConfigMaps at other paths alone.
func syncTemplateLibrary(ctx context.Context, writer client.Client, reader client.Reader, bundle fs.FS) error {
	if err := ensureNamespace(ctx, writer, reader, TemplateLibraryNamespace); err != nil {
		return err
	}

	want := map[string]string{}
	err := fs.WalkDir(bundle, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := fs.ReadFile(bundle, path)
		want[path] = string(content)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to read bundled templates: %w", err)
	}

	list := &corev1.ConfigMapList{}
	if err := reader.List(ctx, list, client.InNamespace(TemplateLibraryNamespace), client.MatchingLabels{TemplateLibraryLabel: "true"}); err != nil {
		return fmt.Errorf("failed to list library template ConfigMaps: %w", err)
	}

	var errs []error
	synced := map[string]bool{}
	for i := range list.Items {
		existing := &list.Items[i]
		path := existing.Annotations[TemplatePathAnnotation]
		content, ok := want[path]
		if !ok && existing.Labels[managedByLabel] != TemplateLibraryManagedByValue {
			continue
		}
		if !ok || synced[path] {
			if err := client.IgnoreNotFound(writer.Delete(ctx, existing)); err != nil {
				errs = append(errs, fmt.Errorf("failed to delete ConfigMap %s: %w", existing.Name, err))
			}
			continue
		}
		synced[path] = true
		desired := libraryConfigMap(path, content)
		if maps.Equal(existing.Data, desired.Data) && len(existing.BinaryData) == 0 &&
			hasAll(existing.Labels, desired.Labels) && hasAll(existing.Annotations, desired.Annotations) {
			continue
		}
		existing.Data = desired.Data
		existing.BinaryData = nil
		existing.Labels = withAll(existing.Labels, desired.Labels)
		existing.Annotations = withAll(existing.Annotations, desired.Annotations)
		if err := writer.Update(ctx, existing); err != nil {
			errs = append(errs, fmt.Errorf("failed to update ConfigMap %s: %w", existing.Name, err))
		}
	}
	for path, content := range want {
		if synced[path] {
			continue
		}
		if err := writer.Create(ctx, libraryConfigMap(path, content)); err != nil {
			errs = append(errs, fmt.Errorf("failed to create ConfigMap for %s: %w", path, err))
		}
	}
	return errors.Join(errs...)
}

// libraryConfigMap returns the managed library ConfigMap for a bundled template.
func libraryConfigMap(path, content string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		Name:      libraryConfigMapName(path),
		Namespace: TemplateLibraryNamespace,
		Labels: map[string]string{
			TemplateLibraryLabel: "true",
			managedByLabel:       TemplateLibraryManagedByValue,
		},
		Annotations: map[string]string{
			TemplatePathAnnotation: path,
		},
		Data: map[string]string{
			v1alpha1.TemplateConfigMapKey: content,
		},
	}
}

func ensureNamespace(ctx context.Context, writer client.Client, reader client.Reader, name string) error {
	err := reader.Get(ctx, types.NamespacedName{Name: name}, &corev1.Namespace{})
	if !apierrors.IsNotFound(err) {
		return err
	}
	err = writer.Create(ctx, &corev1.Namespace{Name: name})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create namespace %s: %w", name, err)
	}
	return nil
}

// hasAll reports whether m holds every key of want with the same value.
func hasAll(m, want map[string]string) bool {
	for k, v := range want {
		if got, ok := m[k]; !ok || got != v {
			return false
		}
	}
	return true
}

// withAll returns m with every entry of add set, allocating m when it is nil.
func withAll(m, add map[string]string) map[string]string {
	if m == nil {
		m = map[string]string{}
	}
	maps.Copy(m, add)
	return m
}
