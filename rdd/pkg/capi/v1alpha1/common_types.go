// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: SUSE LLC
// SPDX-FileCopyrightText: The Rancher Desktop Authors

package v1alpha1

// APIEndpoint represents a reachable Kubernetes API endpoint. It has the JSON
// shape of Cluster API's v1beta2 APIEndpoint.
// +kubebuilder:validation:MinProperties=1
type APIEndpoint struct {
	// host is the hostname on which the API server is serving.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	Host string `json:"host,omitempty"`

	// port is the port on which the API server is serving.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`
}

// MachineAddressType describes a valid MachineAddress type.
// +kubebuilder:validation:Enum=Hostname;ExternalIP;InternalIP;ExternalDNS;InternalDNS
type MachineAddressType string

// The MachineAddressType values that Cluster API defines.
const (
	MachineHostName    MachineAddressType = "Hostname"
	MachineExternalIP  MachineAddressType = "ExternalIP"
	MachineInternalIP  MachineAddressType = "InternalIP"
	MachineExternalDNS MachineAddressType = "ExternalDNS"
	MachineInternalDNS MachineAddressType = "InternalDNS"
)

// MachineAddress contains information for the node's address. It has the JSON
// shape of Cluster API's v1beta2 MachineAddress.
type MachineAddress struct {
	// type is the machine address type, one of Hostname, ExternalIP, InternalIP, ExternalDNS or InternalDNS.
	// +required
	Type MachineAddressType `json:"type,omitempty"`

	// address is the machine address.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Address string `json:"address,omitempty"`
}

// +kubebuilder:validation:MaxItems=128
// +listType=atomic

// MachineAddresses is a slice of MachineAddress items reported by the provider.
type MachineAddresses []MachineAddress

const (
	// ReadyCondition is the condition type that tells Cluster API whether an
	// infrastructure object is ready. Cluster API v1beta2 mirrors it into the
	// InfrastructureReady condition of the Cluster or Machine that references
	// the object.
	ReadyCondition = "Ready"

	// ReadyReason is the reason for a true ReadyCondition. Cluster API uses
	// the same reason for its own true Ready conditions.
	ReadyReason = "Ready"
)

// Reasons for a false ReadyCondition on a LimaMachine.
const (
	// WaitingForMachineOwnerReason means the LimaMachine doesn't have an
	// owner reference to a Cluster API Machine, or that Machine doesn't exist
	// yet.
	WaitingForMachineOwnerReason = "WaitingForMachineOwner"

	// UnsupportedDistributionReason means the owner Machine's version is not
	// a K3s version. The provider supports only K3s downstream clusters.
	UnsupportedDistributionReason = "UnsupportedDistribution"

	// InvalidNameReason means the LimaMachine's name doesn't work as the
	// name of a Lima instance, has a character that Lima changes in the VM's
	// hostname, or makes the hostname or one of the VM's socket paths too
	// long.
	InvalidNameReason = "InvalidName"

	// WaitingForBootstrapDataReason means the owner Machine doesn't name its
	// bootstrap data Secret yet, or that Secret doesn't exist yet. Cluster
	// API uses the same reason.
	WaitingForBootstrapDataReason = "WaitingForBootstrapData"

	// UnsupportedBootstrapDataReason means the bootstrap data is not the
	// cloud-config that Rancher generates.
	UnsupportedBootstrapDataReason = "UnsupportedBootstrapData"

	// NameConflictReason means the LimaVM or the input ConfigMap that the
	// LimaMachine needs already exists in rdd's control plane and belongs to
	// something else, or a LimaVM in another namespace has the LimaMachine's
	// name.
	NameConflictReason = "NameConflict"

	// ProvisioningReason means rdd is creating the LimaMachine's node VM or
	// has created it, and the VM hasn't reported a failure.
	ProvisioningReason = "Provisioning"

	// BootstrapFailedReason means the node VM runs, but the install script
	// from the bootstrap data has not succeeded.
	BootstrapFailedReason = "BootstrapFailed"
)
