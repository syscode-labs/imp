package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ImpWarmPoolSpec defines a pool of pre-booted VMs ready for instant assignment.
type ImpWarmPoolSpec struct {
	// SnapshotRef names the ImpVMSnapshot parent to boot pool members from.
	// The pool controller uses status.baseSnapshot on the referenced object
	// to resolve the elected execution artifact. The pool stays idle until
	// a base snapshot has been elected via `kubectl imp elect`.
	SnapshotRef string `json:"snapshotRef"`

	// Size is the number of pre-booted VMs to maintain in the pool.
	// +kubebuilder:default=2
	Size int32 `json:"size,omitempty"`

	// TemplateName is the ImpVMTemplate used to create pool members.
	TemplateName string `json:"templateName"`
}

// ImpWarmPoolStatus reflects the observed pool state.
type ImpWarmPoolStatus struct {
	// ReadyCount is the number of pool members currently in Running phase.
	// +optional
	ReadyCount int32 `json:"readyCount,omitempty"`

	// Conditions follow the standard k8s condition convention.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=impwp,categories=imp
// +kubebuilder:printcolumn:name="Snapshot",type=string,JSONPath=`.spec.snapshotRef`
// +kubebuilder:printcolumn:name="Size",type=integer,JSONPath=`.spec.size`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ImpWarmPool maintains a pool of pre-booted VMs ready for instant assignment.
// Pool members are created from spec.templateName using the snapshot in spec.snapshotRef.
type ImpWarmPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImpWarmPoolSpec   `json:"spec,omitempty"`
	Status ImpWarmPoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImpWarmPoolList contains a list of ImpWarmPool.
type ImpWarmPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImpWarmPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImpWarmPool{}, &ImpWarmPoolList{})
}
