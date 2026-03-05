package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ClusterImpNodeProfileSpec defines per-node capacity overrides.
// The resource name must match the Kubernetes node name.
// If no profile exists for a node, ClusterImpConfig.spec.capacity.defaultFraction applies.
type ClusterImpNodeProfileSpec struct {
	// CapacityFraction overrides the cluster-default fraction for this node.
	// Range: 0.0–1.0.
	// +optional
	// +kubebuilder:validation:Pattern=`^(0(\.[0-9]+)?|1(\.0+)?)$`
	CapacityFraction string `json:"capacityFraction,omitempty"`

	// MaxImpVMs is a hard cap on the number of ImpVMs allowed on this node,
	// regardless of remaining compute headroom.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxImpVMs int32 `json:"maxImpVMs,omitempty"`

	// VCPUCapacity is the total number of vCPUs available for VMs on this node.
	// When non-zero, takes precedence over fraction-based scheduling.
	// +optional
	// +kubebuilder:validation:Minimum=0
	VCPUCapacity int32 `json:"vcpuCapacity,omitempty"`

	// MemoryMiB is the total memory in MiB available for VMs on this node.
	// When non-zero, takes precedence over fraction-based scheduling.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MemoryMiB int64 `json:"memoryMiB,omitempty"`

	// CPUModel is the CPU model string detected by the node agent at startup
	// (e.g. "Intel(R) Core(TM) i5-8500T CPU @ 2.10GHz").
	// Used by the migration scheduler to filter CPU-compatible destination nodes.
	// Set automatically by the node agent; do not edit manually.
	// +optional
	CPUModel string `json:"cpuModel,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=impnp,categories=imp
// +kubebuilder:printcolumn:name="Fraction",type=string,JSONPath=`.spec.capacityFraction`
// +kubebuilder:printcolumn:name="MaxVMs",type=integer,JSONPath=`.spec.maxImpVMs`

// ClusterImpNodeProfile is the Schema for the clusterimpnodeprofiles API.
// Name must match the target Kubernetes node name.
type ClusterImpNodeProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ClusterImpNodeProfileSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterImpNodeProfileList contains a list of ClusterImpNodeProfile.
type ClusterImpNodeProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterImpNodeProfile `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterImpNodeProfile{}, &ClusterImpNodeProfileList{})
}
