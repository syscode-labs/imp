package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ImpNetworkSpec defines a virtual network shared by one or more VMs.
type ImpNetworkSpec struct {
	// Subnet is the CIDR block for this network (e.g. "192.168.100.0/24").
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}/[0-9]{1,2}$`
	Subnet string `json:"subnet"`

	// Gateway is the default gateway IP for VMs on this network.
	// Defaults to the first usable host address in Subnet if unset.
	// +optional
	Gateway string `json:"gateway,omitempty"`

	// NAT configures masquerade/SNAT for outbound VM traffic.
	// +optional
	NAT NATSpec `json:"nat,omitempty"`

	// DNS lists the nameservers injected into VMs on this network.
	// +optional
	DNS []string `json:"dns,omitempty"`

	// Cilium holds Cilium-specific integration settings.
	// Only relevant when the cluster CNI is Cilium.
	// +optional
	Cilium *CiliumNetworkSpec `json:"cilium,omitempty"`

	// IPAM configures the IP address management backend for this network.
	// Defaults to "internal" (Imp's built-in allocator).
	// +optional
	IPAM *IPAMSpec `json:"ipam,omitempty"`

	// Groups defines named VM groups that share subnets within this network.
	// VMs not in any group receive an isolated /30 CIDR.
	// +optional
	Groups []NetworkGroupSpec `json:"groups,omitempty"`
}

// NATSpec configures outbound NAT for a network.
type NATSpec struct {
	// Enabled turns on MASQUERADE/SNAT for outbound traffic.
	Enabled bool `json:"enabled"`

	// EgressInterface is the host network interface used for NAT.
	// The node agent auto-detects the default route interface if unset.
	// +optional
	EgressInterface string `json:"egressInterface,omitempty"`
}

// CiliumNetworkSpec configures Cilium integration for this network.
type CiliumNetworkSpec struct {
	// ExcludeFromIPAM tells Cilium not to manage IP allocation for this subnet.
	// +optional
	ExcludeFromIPAM bool `json:"excludeFromIPAM,omitempty"`

	// MasqueradeViaCilium delegates outbound masquerade to Cilium's ipMasqAgent.
	// Requires Cilium ipMasqAgent to be configured with this subnet.
	// +optional
	MasqueradeViaCilium bool `json:"masqueradeViaCilium,omitempty"`
}

// IPAMSpec configures the IP address management backend for an ImpNetwork.
type IPAMSpec struct {
	// Provider selects the IPAM backend.
	// "internal" uses Imp's built-in allocator.
	// "cilium" delegates to a CiliumPodIPPool.
	// +kubebuilder:default=internal
	// +kubebuilder:validation:Enum=internal;cilium
	Provider string `json:"provider,omitempty"`

	// Cilium configures Cilium IPAM. Required when Provider is "cilium".
	// +optional
	Cilium *CiliumIPAMSpec `json:"cilium,omitempty"`
}

// CiliumIPAMSpec configures Cilium pool-based IP allocation.
type CiliumIPAMSpec struct {
	// PoolRef is the name of the CiliumPodIPPool resource to allocate from.
	PoolRef string `json:"poolRef"`
}

// NetworkGroupSpec defines a named group of VMs sharing a subnet within an ImpNetwork.
type NetworkGroupSpec struct {
	// Name identifies this group. ImpVMs reference this name via spec.networkGroup.
	Name string `json:"name"`

	// Connectivity controls L2/L3 adjacency between group members.
	// "subnet" places all members on the same subnet (default).
	// "policy-only" uses group identity for policy without L2 adjacency.
	// +kubebuilder:default=subnet
	// +kubebuilder:validation:Enum=subnet;policy-only
	Connectivity string `json:"connectivity,omitempty"`

	// ExpectedSize is a hint for CIDR sizing.
	// The controller rounds up to the next power-of-2 subnet.
	// Isolated VMs (no group) always receive a /30.
	// Default: 14 → /28.
	// +kubebuilder:default=14
	ExpectedSize int32 `json:"expectedSize,omitempty"`
}

// ImpNetworkStatus reflects the observed state of an ImpNetwork.
type ImpNetworkStatus struct {
	// AllocatedIPs tracks IP addresses currently assigned to ImpVMs on this network.
	// +optional
	AllocatedIPs []string `json:"allocatedIPs,omitempty"`

	// Conditions follow the standard k8s condition convention.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=impnet,categories=imp
// +kubebuilder:printcolumn:name="Subnet",type=string,JSONPath=`.spec.subnet`
// +kubebuilder:printcolumn:name="NAT",type=boolean,JSONPath=`.spec.nat.enabled`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ImpNetwork is the Schema for the impnetworks API.
type ImpNetwork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImpNetworkSpec   `json:"spec,omitempty"`
	Status ImpNetworkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImpNetworkList contains a list of ImpNetwork.
type ImpNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImpNetwork `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImpNetwork{}, &ImpNetworkList{})
}
