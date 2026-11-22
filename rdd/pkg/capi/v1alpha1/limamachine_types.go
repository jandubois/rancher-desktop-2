// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MachineFinalizer keeps a LimaMachine until rdd has deleted its node VM.
// Cluster API waits for the infrastructure machine to disappear before it
// deletes the Machine, so the finalizer also holds the Machine back. The
// name contains a "/" because the API server warns about a custom resource
// finalizer without one.
const MachineFinalizer = "capi.rancherdesktop.io/limamachine"

// LimaMachineSpec defines the desired state of a LimaMachine.
type LimaMachineSpec struct {
	// providerID must match the provider ID on the Node object of this machine.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	ProviderID string `json:"providerID,omitempty"`
	// cpus is the number of vCPUs to allocate to the machine's VM.
	// When unset (0), the VM gets 2 vCPUs.
	// +optional
	// +kubebuilder:validation:Minimum=0
	CPUs int `json:"cpus,omitempty"`
	// memory is the amount of RAM to allocate to the machine's VM, as a
	// Kubernetes resource quantity (e.g. "4Gi"). When unset, the VM gets 3Gi.
	// +optional
	// +kubebuilder:validation:XValidation:rule="quantity(string(self)).isGreaterThan(quantity('0'))",message="memory must be positive"
	Memory *resource.Quantity `json:"memory,omitempty"`
	// disk is the size of the machine's VM disk, as a Kubernetes resource
	// quantity (e.g. "100Gi"). When unset, the VM gets Lima's default disk
	// size.
	// +optional
	// +kubebuilder:validation:XValidation:rule="quantity(string(self)).isGreaterThan(quantity('0'))",message="disk must be positive"
	Disk *resource.Quantity `json:"disk,omitempty"`
}

// LimaMachineInitializationStatus provides observations of the LimaMachine
// initialization process.
// +kubebuilder:validation:MinProperties=1
type LimaMachineInitializationStatus struct {
	// provisioned is true when the machine's infrastructure is fully provisioned.
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// LimaMachineStatus defines the observed state of a LimaMachine.
type LimaMachineStatus struct {
	// initialization provides observations of the LimaMachine initialization process.
	// +optional
	Initialization LimaMachineInitializationStatus `json:"initialization,omitempty,omitzero"`

	// addresses contains the addresses of the machine's VM.
	// +optional
	Addresses MachineAddresses `json:"addresses,omitempty"`

	// conditions represent the observations of the LimaMachine's current state.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=32
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=limamachines,scope=Namespaced,categories=cluster-api
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"

// LimaMachine is the Cluster API infrastructure machine backed by a Lima VM.
type LimaMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LimaMachineSpec   `json:"spec,omitempty"`
	Status LimaMachineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LimaMachineList contains a list of LimaMachines.
type LimaMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LimaMachine `json:"items"`
}

func init() {
	registerTypes(&LimaMachine{}, &LimaMachineList{})
}
