package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ImpVMMigrationSpec defines a migration request.
type ImpVMMigrationSpec struct {
	// SourceVMName is the name of the ImpVM to migrate.
	SourceVMName string `json:"sourceVMName"`

	// SourceVMNamespace is the namespace of the source ImpVM.
	SourceVMNamespace string `json:"sourceVMNamespace"`

	// TargetNode optionally pins the destination node.
	// If empty, the controller picks the best-fit CPU-compatible node.
	// +optional
	TargetNode string `json:"targetNode,omitempty"`
}

// ImpVMMigrationStatus reflects the observed migration state.
type ImpVMMigrationStatus struct {
	// Phase is the current migration phase (Pending, Running, Succeeded, Failed).
	// +optional
	Phase string `json:"phase,omitempty"`

	// Message is a human-readable description of the current state.
	// +optional
	Message string `json:"message,omitempty"`

	// SelectedNode is the node chosen by the scheduler (when TargetNode was empty).
	// +optional
	SelectedNode string `json:"selectedNode,omitempty"`

	// CompletedAt is the time migration completed or failed.
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=impmig,categories=imp
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.sourceVMName`
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.targetNode`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ImpVMMigration moves a running ImpVM to another node via Firecracker snapshot/restore.
// The destination node must have a compatible CPU model.
type ImpVMMigration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImpVMMigrationSpec   `json:"spec,omitempty"`
	Status ImpVMMigrationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImpVMMigrationList contains a list of ImpVMMigration.
type ImpVMMigrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImpVMMigration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImpVMMigration{}, &ImpVMMigrationList{})
}
