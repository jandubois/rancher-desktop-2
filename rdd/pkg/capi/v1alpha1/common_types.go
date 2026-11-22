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
