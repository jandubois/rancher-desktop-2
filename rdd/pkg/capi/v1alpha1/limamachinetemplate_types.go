// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LimaMachineTemplateSpec defines the LimaMachines created from the template.
type LimaMachineTemplateSpec struct {
	// template describes the LimaMachines created from this template.
	Template LimaMachineTemplateResource `json:"template"`
}

// LimaMachineTemplateResource describes the data needed to create a LimaMachine.
type LimaMachineTemplateResource struct {
	// spec is the specification of the LimaMachines created from this template.
	Spec LimaMachineSpec `json:"spec"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=limamachinetemplates,scope=Namespaced,categories=cluster-api
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"

// LimaMachineTemplate is the template that Cluster API creates LimaMachines
// from, for example for the machines of a MachineDeployment.
type LimaMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec LimaMachineTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// LimaMachineTemplateList contains a list of LimaMachineTemplates.
type LimaMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LimaMachineTemplate `json:"items"`
}

func init() {
	registerTypes(&LimaMachineTemplate{}, &LimaMachineTemplateList{})
}
