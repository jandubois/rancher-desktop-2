// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LimaClusterSpec defines the desired state of a LimaCluster.
type LimaClusterSpec struct {
	// controlPlaneEndpoint represents the endpoint used to communicate with the control plane.
	// +optional
	// +kubebuilder:default={host: "localhost", port: 6443}
	ControlPlaneEndpoint APIEndpoint `json:"controlPlaneEndpoint,omitempty,omitzero"`
}

// LimaClusterInitializationStatus provides observations of the LimaCluster
// initialization process.
// +kubebuilder:validation:MinProperties=1
type LimaClusterInitializationStatus struct {
	// provisioned is true when the cluster infrastructure is fully provisioned.
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// LimaClusterStatus defines the observed state of a LimaCluster.
type LimaClusterStatus struct {
	// initialization provides observations of the LimaCluster initialization process.
	// +optional
	Initialization LimaClusterInitializationStatus `json:"initialization,omitempty,omitzero"`

	// conditions represent the observations of the LimaCluster's current state.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=32
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=limaclusters,scope=Namespaced,categories=cluster-api
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"

// LimaCluster is the Cluster API infrastructure cluster for Lima machines.
type LimaCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec is the desired state of the LimaCluster. It defaults to empty, so a
	// LimaCluster created without a spec still gets the default
	// controlPlaneEndpoint.
	// +kubebuilder:default={}
	Spec   LimaClusterSpec   `json:"spec,omitempty"`
	Status LimaClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LimaClusterList contains a list of LimaClusters.
type LimaClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LimaCluster `json:"items"`
}

func init() {
	registerTypes(&LimaCluster{}, &LimaClusterList{})
}
