// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package limavm

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"unicode"

	"github.com/lima-vm/lima/v2/pkg/limatmpl"
	"github.com/lima-vm/lima/v2/pkg/limatype"
	"github.com/lima-vm/lima/v2/pkg/limayaml"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlwebhookadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/apis/lima/v1alpha1"
	"github.com/rancher-sandbox/rancher-desktop-daemon/pkg/controllers/lima/limavm/controllers"
)

// LibraryConfigMapValidator validates the ConfigMaps that have TemplateLibraryLabel.
type LibraryConfigMapValidator struct {
	// Reader must read from the API server, not the informer cache, so that the
	// path uniqueness check sees a ConfigMap created moments earlier.
	Reader client.Reader
}

var _ ctrlwebhookadmission.Validator[*corev1.ConfigMap] = &LibraryConfigMapValidator{}

// ValidateCreate validates a new library ConfigMap.
func (v *LibraryConfigMapValidator) ValidateCreate(ctx context.Context, configMap *corev1.ConfigMap) (ctrlwebhookadmission.Warnings, error) {
	return nil, v.validate(ctx, configMap)
}

// ValidateUpdate validates an updated library ConfigMap.
func (v *LibraryConfigMapValidator) ValidateUpdate(ctx context.Context, _, configMap *corev1.ConfigMap) (ctrlwebhookadmission.Warnings, error) {
	return nil, v.validate(ctx, configMap)
}

// ValidateDelete allows every delete.
func (v *LibraryConfigMapValidator) ValidateDelete(context.Context, *corev1.ConfigMap) (ctrlwebhookadmission.Warnings, error) {
	return nil, nil
}

func (v *LibraryConfigMapValidator) validate(ctx context.Context, configMap *corev1.ConfigMap) error {
	p, ok := configMap.Annotations[TemplatePathAnnotation]
	if !ok {
		return fmt.Errorf("template library ConfigMap must have a %s annotation", TemplatePathAnnotation)
	}
	if !fs.ValidPath(p) || p == "." {
		return fmt.Errorf("%s %q must be a relative slash-separated path without '.' or '..' elements", TemplatePathAnnotation, p)
	}
	// Lima derives a name from the basename of every file that a `base:` or
	// `file:` reference names, and fails the reference when that name is not a
	// valid identifier.
	if _, err := limatmpl.InstNameFromYAMLPath(p); err != nil {
		return fmt.Errorf("%s %q cannot be referenced: %w", TemplatePathAnnotation, p, err)
	}
	if configMap.Namespace == TemplateLibraryNamespace {
		if canonical := templateNamePath(p); canonical != p {
			return fmt.Errorf("%s %q in namespace %s must be spelled as %q", TemplatePathAnnotation, p, TemplateLibraryNamespace, canonical)
		}
	}
	if _, ok := configMap.Labels[controllers.TemplateConfigMapLabel]; ok {
		return fmt.Errorf("template library ConfigMap must not have the %s label", controllers.TemplateConfigMapLabel)
	}

	list := &corev1.ConfigMapList{}
	if err := v.Reader.List(ctx, list, client.InNamespace(configMap.Namespace), client.MatchingLabels{TemplateLibraryLabel: "true"}); err != nil {
		return fmt.Errorf("failed to list template library ConfigMaps: %w", err)
	}
	for i := range list.Items {
		other := &list.Items[i]
		if other.Name != configMap.Name && other.Annotations[TemplatePathAnnotation] == p {
			return fmt.Errorf("ConfigMap %q in namespace %q already has %s %q", other.Name, other.Namespace, TemplatePathAnnotation, p)
		}
	}

	if ext := path.Ext(p); ext == ".yaml" || ext == ".yml" {
		if err := limayaml.Unmarshal([]byte(configMap.Data[v1alpha1.TemplateConfigMapKey]), &limatype.LimaYAML{}, p); err != nil {
			return fmt.Errorf("failed to parse %s: %w", p, err)
		}
	}
	return nil
}

// templateNamePath returns the path that Lima's templatestore.Read looks up for
// a `template:` name. It copies templatestore's name rule, which Lima doesn't
// export. The rule cleans the name, drops a leading '/', and appends ".yaml" when
// the name lacks an extension or its extension starts with a digit, as in
// "ubuntu-24.04".
func templateNamePath(name string) string {
	name = strings.TrimPrefix(path.Clean(name), "/")
	ext := path.Ext(name)
	if len(ext) < 2 || unicode.IsDigit(rune(ext[1])) {
		name += ".yaml"
	}
	return name
}
