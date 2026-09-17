// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ContainerNamespaceKind is the Kind string for ContainerNamespace resources.
const ContainerNamespaceKind = "ContainerNamespace"

// ContainerNamespaceStatus defines the observed state of a ContainerNamespace.
type ContainerNamespaceStatus struct {
	// Name is the name of the container namespace.
	//
	// +optional
	Name string `json:"name,omitempty"`
	// Labels are the labels associated with the container namespace.
	//
	// +optional
	Labels map[string]string `json:"labels,omitzero"`
}

// +kubebuilder:object:root=true
// +kubebuilder:ac:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:selectablefield:JSONPath=.status.name
// +kubebuilder:resource:shortName=cns,categories="all"

// ContainerNamespace defines a container engine namespace; note that this is distinct
// from Kubernetes namespaces.
type ContainerNamespace struct {
	metav1.TypeMeta `json:",inline"`

	// Metadata is a standard object metadata
	//
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// Status defines the observed state of the container namespace.
	//
	// +optional
	Status ContainerNamespaceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true

// ContainerNamespaceList contains a list of [ContainerNamespace].
type ContainerNamespaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ContainerNamespace `json:"items"`
}

func init() {
	registerTypes(
		&ContainerNamespace{}, &ContainerNamespaceList{},
	)
}
