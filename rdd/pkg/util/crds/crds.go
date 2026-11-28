// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

// Package crds installs CustomResourceDefinitions into an API server.
package crds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// Outcome says what Apply did to the API server.
type Outcome int

const (
	// Unchanged means the stored CRD already had the spec and labels that
	// Apply was given.
	Unchanged Outcome = iota
	// Created means the CRD did not exist and Apply created it.
	Created
	// Updated means Apply rewrote the stored CRD's spec or labels.
	Updated
)

// Decode returns the CRDs in a multi-document YAML string.
func Decode(data string) ([]apiextensionsv1.CustomResourceDefinition, error) {
	decoder := yaml.NewYAMLToJSONDecoder(bytes.NewBufferString(data))
	var result []apiextensionsv1.CustomResourceDefinition
	for {
		var crd apiextensionsv1.CustomResourceDefinition
		if err := decoder.Decode(&crd); err != nil {
			if errors.Is(err, io.EOF) {
				return result, nil
			}
			return nil, err
		}
		if crd.Name == "" {
			// A comment block before the first document decodes to an empty CRD.
			continue
		}
		result = append(result, crd)
	}
}

// Apply creates crd on the API server. If the API server already has it,
// Apply updates the stored copy when the stored spec differs from crd's, or
// when one of crd's labels is missing from the stored copy or has another
// value there. The update keeps the labels that crd does not set.
func Apply(ctx context.Context, c apiextensionsv1client.CustomResourceDefinitionInterface, crd *apiextensionsv1.CustomResourceDefinition) (Outcome, error) {
	existing, err := c.Get(ctx, crd.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, err := c.Create(ctx, crd, metav1.CreateOptions{}); err != nil {
			return Unchanged, fmt.Errorf("failed to create CRD %s: %w", crd.Name, err)
		}
		return Created, nil
	}
	if err != nil {
		return Unchanged, fmt.Errorf("failed to get CRD %s: %w", crd.Name, err)
	}
	// The API server fills in defaults that controller-gen leaves out of the
	// YAML, such as spec.conversion. Without those defaults, crd's spec never
	// matches the stored one, so fill them in before comparing.
	defaulted := crd.DeepCopy()
	apiextensionsv1.SetObjectDefaults_CustomResourceDefinition(defaulted)
	// Cluster API reads a label on a provider's CRD to learn which of the
	// CRD's API versions implements a Cluster API contract. So a CRD whose
	// labels alone changed still needs an update.
	if apiequality.Semantic.DeepEqual(existing.Spec, defaulted.Spec) && hasLabels(existing, crd.Labels) {
		return Unchanged, nil
	}
	existing.Spec = defaulted.Spec
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	for k, v := range crd.Labels {
		existing.Labels[k] = v
	}
	if _, err := c.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return Unchanged, fmt.Errorf("failed to update CRD %s: %w", crd.Name, err)
	}
	return Updated, nil
}

// hasLabels reports whether crd has every label in labels with the same value.
func hasLabels(crd *apiextensionsv1.CustomResourceDefinition, labels map[string]string) bool {
	for k, v := range labels {
		if got, ok := crd.Labels[k]; !ok || got != v {
			return false
		}
	}
	return true
}

// WaitEstablished waits until the API server serves the named CRD.
func WaitEstablished(ctx context.Context, c apiextensionsv1client.CustomResourceDefinitionInterface, name string, timeout time.Duration) error {
	err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, timeout, true, func(ctx context.Context) (bool, error) {
		crd, err := c.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, condition := range crd.Status.Conditions {
			if condition.Type == apiextensionsv1.Established && condition.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("CRD %s not established: %w", name, err)
	}
	return nil
}
